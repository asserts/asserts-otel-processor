package assertsprocessor

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type metricHelper struct {
	logger                *zap.Logger
	config                *Config
	prometheusRegistry    *prometheus.Registry
	latencyHistogram      *prometheus.HistogramVec
	attributeValueRegExps *map[string]*regexp.Regexp
}

func (p *metricHelper) compileSpanFilterRegexps() error {
	p.logger.Info("compiling regexps")
	for attName, matchExpString := range *p.config.AttributeExps {
		compile, err := regexp.Compile(matchExpString)
		if err != nil {
			return err
		}
		(*p.attributeValueRegExps)[attName] = compile
	}
	p.logger.Debug("compiled regexps successfully")
	return nil
}

// Returns true if a span matches the span selection criteria
func (p *metricHelper) shouldCaptureMetrics(span ptrace.Span) bool {
	if len(*p.attributeValueRegExps) > 0 {
		spanAttributes := span.Attributes()
		for attName, matchExp := range *p.attributeValueRegExps {
			value, found := spanAttributes.Get(attName)
			if !found {
				return false
			}
			valueMatches := matchExp.String() == value.AsString() || matchExp.MatchString(value.AsString())
			if !valueMatches {
				return false
			}
		}
		return true
	} else {
		return false
	}
}

func (p *metricHelper) captureMetrics(namespace string, service string, span ptrace.Span) {
	if p.shouldCaptureMetrics(span) {
		labels := p.buildLabels(namespace, service, span)
		latencySeconds := computeLatency(span)
		p.recordLatency(labels, latencySeconds)
	}
}

func (p *metricHelper) buildLabels(namespace string, service string, span ptrace.Span) prometheus.Labels {
	labels := prometheus.Labels{
		"asserts_env":  p.config.Env,
		"asserts_site": p.config.Site,
		"namespace":    namespace,
		"service":      service,
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
	var allowedLabels = []string{"asserts_env", "asserts_site", "namespace", "service"}
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
