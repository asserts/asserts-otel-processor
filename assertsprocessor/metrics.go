package assertsprocessor

import (
	"github.com/jellydator/ttlcache/v3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/puzpuzpuz/xsync/v2"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	envLabel            = "asserts_env"
	siteLabel           = "asserts_site"
	namespaceLabel      = "namespace"
	serviceLabel        = "service"
	requestContextLabel = "asserts_request_context"
)

type metricHelper struct {
	logger                   *zap.Logger
	config                   *Config
	prometheusRegistry       *prometheus.Registry
	spanMatcher              *spanMatcher
	histograms               *xsync.MapOf[string, *prometheus.HistogramVec]
	requestContextsByService *xsync.MapOf[string, *ttlcache.Cache[string, string]] // limit cardinality of request contexts for which metrics are captured
}

func newMetricHelper(logger *zap.Logger, config *Config, spanMatcher *spanMatcher) *metricHelper {
	return &metricHelper{
		logger:                   logger,
		config:                   config,
		prometheusRegistry:       prometheus.NewRegistry(),
		spanMatcher:              spanMatcher,
		histograms:               xsync.NewMapOf[*prometheus.HistogramVec](),
		requestContextsByService: xsync.NewMapOf[*ttlcache.Cache[string, string]](),
	}
}

func (p *metricHelper) captureMetrics(namespace string, service string, span *ptrace.Span,
	resourceSpan *ptrace.ResourceSpans) {

	serviceKey := namespace + "#" + service
	requestContext := p.spanMatcher.getRequest(span)

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
			p.logger.Debug("Adding request context to cache",
				zap.String("service", serviceKey),
				zap.String("request context", requestContext),
			)
		}
		labels := p.buildLabels(namespace, service, requestContext, span, resourceSpan)
		latencySeconds := computeLatency(span)
		histogram := p.getOrCreateHistogram(labels)
		histogram.With(labels).Observe(latencySeconds)
	} else {
		p.logger.Warn("Too many request contexts. Metrics won't be captured for",
			zap.String("service", serviceKey),
			zap.String("request context", requestContext),
		)
	}
}

func (p *metricHelper) buildLabels(namespace string, service string, requestContext string, span *ptrace.Span,
	resourceSpan *ptrace.ResourceSpans) prometheus.Labels {

	labels := prometheus.Labels{
		envLabel:            p.config.Env,
		siteLabel:           p.config.Site,
		namespaceLabel:      namespace,
		serviceLabel:        service,
		requestContextLabel: requestContext,
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
		}
	}
	labels["span_kind"] = span.Kind().String()
	p.logger.Debug("Captured Metric labels",
		zap.String("traceId", span.TraceID().String()),
		zap.String("spanId", span.SpanID().String()),
		zap.String("capturedSpanAttributes", strings.Join(capturedSpanAttributes, ", ")),
		zap.String("capturedResourceAttributes", strings.Join(capturedResourceAttributes, ", ")),
	)
	return labels
}

func (p *metricHelper) getOrCreateHistogram(labels prometheus.Labels) *prometheus.HistogramVec {
	labelNames := make([]string, 0, len(labels))
	for k := range labels {
		labelNames = append(labelNames, k)
	}
	sort.Strings(labelNames)
	histogramKey := strings.Join(labelNames, ",")
	histogram, _ := p.histograms.LoadOrCompute(histogramKey, func() *prometheus.HistogramVec {
		histogram := prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "otel",
			Subsystem: "span",
			Name:      "latency_seconds",
		}, labelNames)
		err := p.prometheusRegistry.Register(histogram)
		if err != nil {
			p.logger.Error("Failed to register histogram", zap.Error(err))
		}
		return histogram
	})
	return histogram
}

func (p *metricHelper) startExporter() {
	s := &http.Server{
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

	// Expose the registered metrics via HTTP.
	http.Handle("/metrics", promhttp.HandlerFor(
		p.prometheusRegistry,
		promhttp.HandlerOpts{},
	))

	p.logger.Info("Starting Prometheus Exporter Listening", zap.Uint64("port", p.config.PrometheusExporterPort))
	p.logger.Fatal("Error starting Prometheus Server", zap.Error(s.ListenAndServe()))
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
