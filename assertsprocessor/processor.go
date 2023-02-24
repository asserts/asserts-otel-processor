package assertsprocessor

import (
	"context"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tilinna/clock"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
	"go.uber.org/zap"
	"log"
	"net/http"
	"regexp"
	"time"
)

type assertsProcessorImpl struct {
	logger                *zap.Logger
	config                Config
	attributeValueRegExps *map[string]regexp.Regexp
	nextConsumer          consumer.Traces
	metricsFlushTicker    *clock.Ticker
	latencyBounds         *map[string]map[string]LatencyBound
	latencyHistogram      *prometheus.HistogramVec
	prometheusRegistry    *prometheus.Registry
}

type LatencyBound struct {
	Lower float64
	Upper float64
}

// Capabilities implements the consumer interface.
func (p *assertsProcessorImpl) Capabilities() consumer.Capabilities {
	p.logger.Debug("consumer.Capabilities callback")
	return consumer.Capabilities{MutatesData: false}
}

// Start implements the consumer interface.
func (p *assertsProcessorImpl) Start(ctx context.Context, host component.Host) error {
	p.logger.Debug("consumer.Start callback")
	return p.buildCompiledRegexps()
}

func (p *assertsProcessorImpl) Shutdown(context.Context) error {
	p.logger.Debug("consumer.Shutdown")
	return nil
}

// ConsumeTraces implements the consumer.Traces interface.
// Samples the trace if the latency threshold exceeds for any of the spans of interest.
// Also generates span metrics for the spans of interest
func (p *assertsProcessorImpl) ConsumeTraces(ctx context.Context, traces ptrace.Traces) error {
	p.logger.Debug("consumer.ConsumeTraces")
	sampleTrace := false
	var traceId string
	for i := 0; i < traces.ResourceSpans().Len(); i++ {
		resourceSpans := traces.ResourceSpans().At(i)
		resourceAttributes := resourceSpans.Resource().Attributes()

		namespaceAttr, found := resourceAttributes.Get(conventions.AttributeServiceNamespace)
		if !found {
			continue
		}

		serviceAttr, found := resourceAttributes.Get(conventions.AttributeServiceName)
		if !found {
			continue
		}

		serviceName := serviceAttr.Str()
		namespace := namespaceAttr.Str()
		ilsSlice := resourceSpans.ScopeSpans()
		for j := 0; j < ilsSlice.Len(); j++ {
			ils := ilsSlice.At(j)
			spans := ils.Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				if traceId == "" {
					traceId = span.TraceID().String()
				}
				if p.spanOfInterest(span) {
					p.captureMetrics(namespace, serviceName, span)
					sampleTrace = sampleTrace || p.shouldCaptureTrace(span)
				}
			}
		}
	}
	if sampleTrace {
		p.logger.Debug("consumer.ConsumeTraces Sampling Trace",
			zap.String("traceId", traceId))
		return p.nextConsumer.ConsumeTraces(ctx, traces)
	}
	return nil
}

func (p *assertsProcessorImpl) buildCompiledRegexps() error {
	p.logger.Debug("consumer.Start compiling regexps")
	for attName, matchExpString := range *p.config.AttributeExps {
		compile, err := regexp.Compile(matchExpString)
		if err != nil {
			return err
		}
		(*p.attributeValueRegExps)[attName] = *compile
	}
	p.logger.Debug("consumer.Start compiled regexps successfully")
	return nil
}

// Returns true if the span meets the span selection criteria
func (p *assertsProcessorImpl) matches(span ptrace.Span) bool {
	if len(*p.attributeValueRegExps) > 0 {
		for attName, matchExp := range *p.attributeValueRegExps {
			value, found := span.Attributes().Get(attName)
			if !found || !matchExp.MatchString(value.AsString()) {
				return false
			}
		}
		return true
	}
	return false
}

// Returns true if a span is a root span or if it matches the span selection criteria
func (p *assertsProcessorImpl) spanOfInterest(span ptrace.Span) bool {
	return span.ParentSpanID() == [8]byte{} || p.matches(span)
}

func (p *assertsProcessorImpl) captureMetrics(namespace string, service string, span ptrace.Span) {
	p.logger.Debug("consumer.ConsumeTraces Capturing span duration metric for",
		zap.String("spanKind", span.Kind().String()),
		zap.String("spanId", span.SpanID().String()),
	)

	labels := prometheus.Labels{
		"asserts_env":  p.config.Env,
		"asserts_site": p.config.Site,
		"namespace":    namespace,
		"service":      service,
	}

	for _, labelName := range p.config.CaptureAttributesInMetric {
		value, present := span.Attributes().Get(labelName)
		if present {
			labels[labelName] = value.AsString()
		}
	}

	p.latencyHistogram.With(labels).Observe(float64(span.EndTimestamp()-span.StartTimestamp()) / 1e9)
}

func (p *assertsProcessorImpl) shouldCaptureTrace(span ptrace.Span) bool {
	return (span.EndTimestamp() - span.StartTimestamp()) > 5e8
}

func newProcessor(logger *zap.Logger, config component.Config, nextConsumer consumer.Traces) (*assertsProcessorImpl, error) {
	logger.Info("Creating assertsotelprocessor")
	pConfig := config.(*Config)

	var allowedLabels []string
	allowedLabels = append(allowedLabels, "asserts_env")
	allowedLabels = append(allowedLabels, "asserts_site")
	allowedLabels = append(allowedLabels, "namespace")
	allowedLabels = append(allowedLabels, "service")
	if pConfig.CaptureAttributesInMetric != nil {
		allowedLabels = append((*pConfig).CaptureAttributesInMetric)
	}

	p := &assertsProcessorImpl{
		logger:                logger,
		config:                *pConfig,
		nextConsumer:          nextConsumer,
		attributeValueRegExps: &map[string]regexp.Regexp{},
		latencyHistogram: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "otel",
			Subsystem: "span",
			Name:      "latency_seconds",
		}, allowedLabels),
	}

	// Start the prometheus server on port 9465
	p.prometheusRegistry = prometheus.NewRegistry()
	go startExporter(p.prometheusRegistry)

	return p, nil
}

func startExporter(reg *prometheus.Registry) {
	s := &http.Server{
		Addr:           ":9465",
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	// Add Go module build info.
	reg.MustRegister(collectors.NewBuildInfoCollector())
	reg.MustRegister(collectors.NewGoCollector(
		collectors.WithGoCollectorRuntimeMetrics(collectors.GoRuntimeMetricsRule{Matcher: regexp.MustCompile("/.*")}),
	))

	// Expose the registered metrics via HTTP.
	http.Handle("/metrics", promhttp.HandlerFor(
		reg,
		promhttp.HandlerOpts{},
	))

	log.Println("Starting Prometheus Exporter Listening on port 9465")
	log.Fatal(s.ListenAndServe())
}
