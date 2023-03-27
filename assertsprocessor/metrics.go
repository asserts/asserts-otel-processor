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
	latencyHistogram         *prometheus.HistogramVec
	spanMatcher              *spanMatcher
	requestContextsByService *xsync.MapOf[string, *ttlcache.Cache[string, string]]
}

func (p *metricHelper) captureMetrics(namespace string, service string, span *ptrace.Span) {
	serviceKey := service + "#" + namespace
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
		labels := p.buildLabels(namespace, service, requestContext, span)
		latencySeconds := computeLatency(span)
		p.recordLatency(labels, latencySeconds)
	} else {
		p.logger.Warn("Too many request contexts. Metrics won't be captured for",
			zap.String("service", serviceKey),
			zap.String("request context", requestContext),
		)
	}
}

func (p *metricHelper) buildLabels(namespace string, service string, requestContext string, span *ptrace.Span) prometheus.Labels {
	labels := prometheus.Labels{
		envLabel:            p.config.Env,
		siteLabel:           p.config.Site,
		namespaceLabel:      namespace,
		serviceLabel:        service,
		requestContextLabel: requestContext,
	}

	for _, labelName := range p.config.CaptureAttributesInMetric {
		value, present := span.Attributes().Get(labelName)
		if present {
			labels[p.applyPromConventions(labelName)] = value.AsString()
		} else {
			labels[p.applyPromConventions(labelName)] = ""
		}
	}
	return labels
}

func (p *metricHelper) recordLatency(labels prometheus.Labels, latencySeconds float64) {
	p.latencyHistogram.With(labels).Observe(latencySeconds)
}

func (p *metricHelper) buildHistogram() error {
	var allowedLabels = []string{envLabel, siteLabel, namespaceLabel, serviceLabel, requestContextLabel}
	if p.config.CaptureAttributesInMetric != nil {
		for _, label := range p.config.CaptureAttributesInMetric {
			allowedLabels = append(allowedLabels, p.applyPromConventions(label))
		}
	}

	// Start the prometheus server on port 9465
	p.prometheusRegistry = prometheus.NewRegistry()
	p.latencyHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "otel",
		Subsystem: "span",
		Name:      "latency_seconds",
	}, allowedLabels)
	err := p.prometheusRegistry.Register(p.latencyHistogram)
	if err != nil {
		p.logger.Fatal("Error starting Prometheus Server", zap.Error(err))
		return err
	}
	return nil
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
