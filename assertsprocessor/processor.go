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
	"strings"
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
	p.logger.Info("consumer.Capabilities callback")
	return consumer.Capabilities{MutatesData: false}
}

// Start implements the consumer interface.
func (p *assertsProcessorImpl) Start(ctx context.Context, host component.Host) error {
	p.logger.Info("consumer.Start callback")
	return p.buildCompiledRegexps()
}

func (p *assertsProcessorImpl) Shutdown(context.Context) error {
	p.logger.Info("consumer.Shutdown")
	return nil
}

// ConsumeTraces implements the consumer.Traces interface.
// Samples the trace if the latency threshold exceeds for any of the spans of interest.
// Also generates span metrics for the spans of interest
func (p *assertsProcessorImpl) ConsumeTraces(ctx context.Context, traces ptrace.Traces) error {
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
				if span.ParentSpanID().IsEmpty() {
					sampleTrace = p.shouldCaptureTrace(traceId, span)
				}
				if p.spanOfInterest(span) {
					p.captureMetrics(namespace, serviceName, span)
				}
			}
		}
	}
	if sampleTrace {
		p.logger.Info("consumer.ConsumeTraces Sampling Trace",
			zap.String("traceId", traceId))
		return p.nextConsumer.ConsumeTraces(ctx, traces)
	}
	return nil
}

func (p *assertsProcessorImpl) buildCompiledRegexps() error {
	p.logger.Info("consumer.Start compiling regexps")
	for attName, matchExpString := range *p.config.AttributeExps {
		compile, err := regexp.Compile(matchExpString)
		if err != nil {
			return err
		}
		(*p.attributeValueRegExps)[attName] = *compile
	}
	p.logger.Info("consumer.Start compiled regexps successfully")
	return nil
}

// Returns true if the span meets the span selection criteria
func (p *assertsProcessorImpl) matches(span ptrace.Span) bool {
	if len(*p.attributeValueRegExps) > 0 {
		for attName, matchExp := range *p.attributeValueRegExps {
			value, found := span.Attributes().Get(attName)
			if !found || !matchExp.MatchString(value.AsString()) {
				return false
			} else {

			}
		}
		return true
	}
	return false
}

// Returns true if a span is a root span or if it matches the span selection criteria
func (p *assertsProcessorImpl) spanOfInterest(span ptrace.Span) bool {
	return span.ParentSpanID().IsEmpty() || p.matches(span)
}

func (p *assertsProcessorImpl) captureMetrics(namespace string, service string, span ptrace.Span) {
	p.logger.Info("consumer.ConsumeTraces Capturing span duration metric for",
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
			labels[applyPromConventions(labelName)] = value.AsString()
		} else {
			labels[applyPromConventions(labelName)] = ""
		}
	}

	// Recording latency metric

	p.latencyHistogram.With(labels).Observe(float64(span.EndTimestamp()-span.StartTimestamp()) / 1e9)
}

func applyPromConventions(text string) string {
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

func (p *assertsProcessorImpl) shouldCaptureTrace(traceId string, rootSpan ptrace.Span) bool {
	spanDuration := float64(rootSpan.EndTimestamp() - rootSpan.StartTimestamp())
	p.logger.Info("Root Span Duration ", zap.String("Trace Id", traceId), zap.Duration("Duration", time.Duration(spanDuration)))
	return spanDuration > p.config.DefaultLatencyThreshold*1e9
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
		for _, name := range (*pConfig).CaptureAttributesInMetric {
			allowedLabels = append(allowedLabels, applyPromConventions(name))
		}
	}

	histogramVec := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "otel",
		Subsystem: "span",
		Name:      "latency_seconds",
	}, allowedLabels)

	p := &assertsProcessorImpl{
		logger:                logger,
		config:                *pConfig,
		nextConsumer:          nextConsumer,
		attributeValueRegExps: &map[string]regexp.Regexp{},
		latencyHistogram:      histogramVec,
	}

	// Start the prometheus server on port 9465
	p.prometheusRegistry = prometheus.NewRegistry()
	p.prometheusRegistry.Register(histogramVec)

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
