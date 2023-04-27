package assertsprocessor

import (
	"context"
	"errors"
	"github.com/jellydator/ttlcache/v3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/puzpuzpuz/xsync/v2"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	envLabel             = "asserts_env"
	siteLabel            = "asserts_site"
	namespaceLabel       = "namespace"
	serviceLabel         = "service"
	requestTypeLabel     = "asserts_request_type"
	errorTypeLabel       = "asserts_error_type"
	requestContextLabel  = "asserts_request_context"
	spanKind             = "span_kind"
	traceSampleTypeLabel = "sample_type"
)

type metricHelper struct {
	logger             *zap.Logger
	config             *Config
	httpServer         *http.Server
	prometheusRegistry *prometheus.Registry
	latencyHistogram   *prometheus.HistogramVec
	totalTraceCount    *prometheus.CounterVec
	sampledTraceCount  *prometheus.CounterVec
	// limit cardinality of request contexts for which metrics are captured
	requestContextsByService *xsync.MapOf[string, *ttlcache.Cache[string, string]]
	// guard access to config.CaptureAttributesInMetric and latencyHistogram
	rwMutex *sync.RWMutex
}

func newMetricHelper(logger *zap.Logger, config *Config) *metricHelper {
	return &metricHelper{
		logger:                   logger,
		config:                   config,
		prometheusRegistry:       prometheus.NewRegistry(),
		requestContextsByService: xsync.NewMapOf[*ttlcache.Cache[string, string]](),
		rwMutex:                  &sync.RWMutex{},
	}
}

func (p *metricHelper) recordLatency(labels prometheus.Labels, latencySeconds float64) {
	p.rwMutex.RLock()
	defer p.rwMutex.RUnlock()
	p.latencyHistogram.With(labels).Observe(latencySeconds)
}

func (p *metricHelper) registerMetrics() error {
	var traceCountLabels = []string{envLabel, siteLabel, namespaceLabel, serviceLabel}
	var sampledTraceCountLabels = []string{envLabel, siteLabel, namespaceLabel, traceSampleTypeLabel, serviceLabel}

	p.logger.Info("Total Trace Counter with ", zap.String("labels", strings.Join(traceCountLabels, ", ")))
	p.logger.Info("Sampled Trace Counter with ", zap.String("labels", strings.Join(sampledTraceCountLabels, ", ")))

	// Start the prometheus server on port 9465
	p.prometheusRegistry = prometheus.NewRegistry()

	// Create Counter for total trace count
	p.totalTraceCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "asserts",
		Subsystem: "trace",
		Name:      "count_total",
	}, traceCountLabels)
	err := p.prometheusRegistry.Register(p.totalTraceCount)
	if err != nil {
		p.logger.Fatal("Error registering Total Trace Counter Vector", zap.Error(err))
		return err
	}

	// Create Counter for sampled trace count
	p.sampledTraceCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "asserts",
		Subsystem: "trace",
		Name:      "sampled_count_total",
	}, sampledTraceCountLabels)
	err = p.prometheusRegistry.Register(p.sampledTraceCount)
	if err != nil {
		p.logger.Fatal("Error registering Sampled Trace Counter Vector", zap.Error(err))
		return err
	}

	return p.registerLatencyHistogram(p.config.CaptureAttributesInMetric)
}

func (p *metricHelper) registerLatencyHistogram(captureAttributesInMetric []string) error {
	var spanMetricLabels = []string{envLabel, siteLabel, namespaceLabel, serviceLabel, spanKind}

	if captureAttributesInMetric != nil {
		for _, label := range captureAttributesInMetric {
			spanMetricLabels = append(spanMetricLabels, p.applyPromConventions(label))
		}
	}
	sort.Strings(spanMetricLabels)
	p.logger.Info("Registering Latency Histogram with ", zap.String("labels", strings.Join(spanMetricLabels, ", ")))

	p.latencyHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "otel",
		Subsystem: "span",
		Name:      "latency_seconds",
	}, spanMetricLabels)
	err := p.prometheusRegistry.Register(p.latencyHistogram)
	if err != nil {
		p.logger.Fatal("Error registering Latency Histogram Metric Vector", zap.Error(err))
		return err
	}

	return nil
}

func (p *metricHelper) captureMetrics(namespace string, service string, span *ptrace.Span,
	resourceSpan *ptrace.ResourceSpans) {
	serviceKey := getServiceKey(namespace, service)
	attrValue, _ := span.Attributes().Get(AssertsRequestContextAttribute)
	requestContext := attrValue.AsString()

	cache, _ := p.requestContextsByService.LoadOrCompute(serviceKey, func() *ttlcache.Cache[string, string] {
		cache := ttlcache.New[string, string](
			ttlcache.WithTTL[string, string](time.Minute*time.Duration(p.config.RequestContextCacheTTL)),
			ttlcache.WithCapacity[string, string](uint64(p.config.LimitPerService)),
		)
		p.logger.Debug("Created a cache of known request contexts for service - " + serviceKey)

		go cache.Start() // starts automatic expired item deletion
		return cache
	})

	if val := cache.Get(requestContext); cache.Len() < p.config.LimitPerService || val != nil {
		if val == nil {
			cache.Set(requestContext, requestContext, ttlcache.DefaultTTL)
			p.logger.Info("Adding request context to cache",
				zap.String("service", serviceKey),
				zap.String("request context", requestContext),
			)
		}
		labels := p.buildLabels(namespace, service, span, resourceSpan)
		latencySeconds := computeLatency(span)
		p.recordLatency(labels, latencySeconds)
	} else {
		p.logger.Warn("Too many request contexts. Metrics won't be captured for",
			zap.String("service", serviceKey),
			zap.String("request context", requestContext),
		)
	}
}

func (p *metricHelper) buildLabels(namespace string, service string, span *ptrace.Span,
	resourceSpan *ptrace.ResourceSpans) prometheus.Labels {

	p.rwMutex.RLock()
	defer p.rwMutex.RUnlock()

	labels := prometheus.Labels{
		envLabel:       p.config.Env,
		siteLabel:      p.config.Site,
		namespaceLabel: namespace,
		serviceLabel:   service,
	}

	capturedResourceAttributes := make([]string, 0)
	capturedSpanAttributes := make([]string, 0)
	for _, labelName := range p.config.CaptureAttributesInMetric {
		value, present := span.Attributes().Get(labelName)
		if !present {
			value, present = resourceSpan.Resource().Attributes().Get(labelName)
			if present {
				capturedResourceAttributes = append(capturedResourceAttributes, labelName)
			}
		} else {
			capturedSpanAttributes = append(capturedSpanAttributes, labelName)
		}
		if present {
			labels[p.applyPromConventions(labelName)] = value.AsString()
		} else {
			labels[p.applyPromConventions(labelName)] = ""
		}
	}
	labels[spanKind] = span.Kind().String()
	p.logger.Debug("Captured Metric labels",
		zap.String("traceId", span.TraceID().String()),
		zap.String("spanId", span.SpanID().String()),
		zap.String("capturedSpanAttributes", strings.Join(capturedSpanAttributes, ", ")),
		zap.String("capturedResourceAttributes", strings.Join(capturedResourceAttributes, ", ")),
	)
	return labels
}

func (p *metricHelper) startExporter() {
	// Create a new ServeMux instead of using the DefaultServeMux. This allows registering
	// a handler function for the same URL pattern again on a different htp server instance
	sm := http.NewServeMux()
	// Expose the registered metrics via HTTP.
	sm.Handle("/metrics", promhttp.HandlerFor(
		p.prometheusRegistry,
		promhttp.HandlerOpts{},
	))

	p.httpServer = &http.Server{
		Handler:        sm,
		Addr:           ":" + strconv.FormatUint(p.config.PrometheusExporterPort, 10),
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	// Add Go module build info.
	p.prometheusRegistry.MustRegister(collectors.NewBuildInfoCollector())
	p.prometheusRegistry.MustRegister(collectors.NewGoCollector(
		collectors.WithGoCollectorRuntimeMetrics(collectors.GoRuntimeMetricsRule{Matcher: regexp.MustCompile("/.*")}),
	))

	p.logger.Info("Starting Prometheus Exporter Listening", zap.Uint64("port", p.config.PrometheusExporterPort))
	go func() {
		if err := p.httpServer.ListenAndServe(); err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				p.logger.Error("Prometheus Exporter is shutdown", zap.Error(err))
			} else if err != nil {
				p.logger.Fatal("Error starting Prometheus Exporter", zap.Error(err))
			}
		}
	}()
}

func (p *metricHelper) stopExporter() error {
	p.latencyHistogram.Reset()
	p.totalTraceCount.Reset()
	p.sampledTraceCount.Reset()

	p.prometheusRegistry.Unregister(p.latencyHistogram)
	p.prometheusRegistry.Unregister(p.totalTraceCount)
	p.prometheusRegistry.Unregister(p.sampledTraceCount)

	shutdownCtx := context.Background()
	return p.httpServer.Shutdown(shutdownCtx)
}

func (p *metricHelper) applyPromConventions(text string) string {
	replacer := strings.NewReplacer(
		" ", "_",
		",", "_",
		"\t", "_",
		"/", "_",
		"\\", "_",
		".", "_",
		"-", "_",
		":", "_",
		"=", "_",
		"“", "_",
		"@", "_",
		"<", "_",
		">", "_",
		"%", "_percent",
	)
	return strings.ToLower(replacer.Replace(text))
}

// configListener interface implementation
func (p *metricHelper) isUpdated(currConfig *Config, newConfig *Config) bool {
	p.rwMutex.RLock()
	defer p.rwMutex.RUnlock()

	updated := !reflect.DeepEqual(currConfig.CaptureAttributesInMetric, newConfig.CaptureAttributesInMetric)
	if updated {
		p.logger.Info("Change detected in config CaptureAttributesInMetric",
			zap.Any("Current", currConfig.CaptureAttributesInMetric),
			zap.Any("New", newConfig.CaptureAttributesInMetric),
		)
	} else {
		p.logger.Debug("No change detected in config CaptureAttributesInMetric")
	}
	return updated
}

func (p *metricHelper) onUpdate(newConfig *Config) error {
	p.rwMutex.Lock()
	defer p.rwMutex.Unlock()

	// This is a bit tricky! We cannot simply register the metric again with different labels
	// We have to throw away the existing prometheus registry, shutdown the prometheus exporter
	// and redo all of that work again
	err := p.stopExporter()
	if err == nil {
		currConfigCaptureAttributesInMetric := p.config.CaptureAttributesInMetric
		// use new config
		p.config.setCaptureAttributesInMetric(newConfig.CaptureAttributesInMetric)

		// create new prometheus registry and register metrics
		err = p.registerMetrics()
		if err == nil {
			p.logger.Info("Updated config CaptureAttributesInMetric",
				zap.Any("New", newConfig.CaptureAttributesInMetric),
			)
		} else {
			p.logger.Error("Ignoring config CaptureAttributesInMetric due to error registering new latency histogram",
				zap.Error(err),
			)
			// latency histogram registration failed, reverting to old config
			// create new prometheus registry and register metrics again
			p.config.setCaptureAttributesInMetric(currConfigCaptureAttributesInMetric)
			_ = p.registerMetrics()
		}

		p.startExporter()
	} else {
		err = errors.New("error stopping http server exporting prometheus metrics")
		p.logger.Error("Ignoring config CaptureAttributesInMetric", zap.Error(err))
	}
	return err
}
