package assertsprocessor

import (
	"context"
	"github.com/orcaman/concurrent-map/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/tilinna/clock"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
	"go.uber.org/zap"
	"regexp"
)

// The methods from a Span that we care for to enable easy mocking

type assertsProcessorImpl struct {
	logger                *zap.Logger
	config                Config
	attributeValueRegExps *map[string]regexp.Regexp
	nextConsumer          consumer.Traces
	latencyBounds         cmap.ConcurrentMap[string, cmap.ConcurrentMap[string, LatencyBound]]
	latencyHistogram      *prometheus.HistogramVec
	prometheusRegistry    *prometheus.Registry
	thresholdSyncTicker   *clock.Ticker
	entityKeys            cmap.ConcurrentMap[string, EntityKeyDto]
	done                  chan bool
}

// Capabilities implements the consumer interface.
func (p *assertsProcessorImpl) Capabilities() consumer.Capabilities {
	p.logger.Info("consumer.Capabilities callback")
	return consumer.Capabilities{MutatesData: false}
}

// Start implements the consumer interface.
func (p *assertsProcessorImpl) Start(ctx context.Context, host component.Host) error {
	p.logger.Info("consumer.Start callback")
	err := p.buildCompiledRegexps()
	if err == nil {
		if p.config.AssertsServer != "" {
			go p.fetchThresholds()
		} else {
			p.logger.Info("Asserts Server not specified. No dynamic thresholds")
		}
	}
	return err
}

func (p *assertsProcessorImpl) Shutdown(context.Context) error {
	p.logger.Info("consumer.Shutdown")
	p.done <- true
	return nil
}

// ConsumeTraces implements the consumer.Traces interface.
// Samples the trace if the latency threshold exceeds for the root spans.
// Also generates span metrics for the spans of interest
func (p *assertsProcessorImpl) ConsumeTraces(ctx context.Context, traces ptrace.Traces) error {
	sampleTrace := false
	var traceId string
	for i := 0; i < traces.ResourceSpans().Len(); i++ {
		resourceSpans := traces.ResourceSpans().At(i)
		resourceAttributes := resourceSpans.Resource().Attributes()

		var namespace string
		namespaceAttr, found := resourceAttributes.Get(conventions.AttributeServiceNamespace)
		if found {
			namespace = namespaceAttr.Str()
		}

		serviceAttr, found := resourceAttributes.Get(conventions.AttributeServiceName)
		if !found {
			continue
		}
		serviceName := serviceAttr.Str()
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
					sampleTrace = p.shouldCaptureTrace(namespace, serviceName, traceId, span)
				}
				if p.shouldCaptureMetrics(span) {
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
	p.logger.Debug("consumer.Start compiled regexps successfully")
	return nil
}

// Returns true if a span matches the span selection criteria
func (p *assertsProcessorImpl) shouldCaptureMetrics(span ptrace.Span) bool {
	if len(*p.attributeValueRegExps) > 0 {
		spanAttributes := span.Attributes()
		for attName, matchExp := range *p.attributeValueRegExps {
			value, found := spanAttributes.Get(attName)
			if !found {
				return false
			}
			//p.logger.Info("Found Span Attribute",
			//	zap.String(attName, value.AsString()))

			valueMatches := matchExp.String() == value.AsString() || matchExp.MatchString(value.AsString())
			//p.logger.Info("Value Regexp Result",
			//	zap.String("regexp", matchExp.String()),
			//	zap.Bool("result", valueMatches))
			if !valueMatches {
				return false
			}
		}
		return true
	} else {
		return false
	}
}

func (p *assertsProcessorImpl) captureMetrics(namespace string, service string, span ptrace.Span) {
	labels := p.buildLabels(namespace, service, span)
	latencySeconds := p.computeLatency(span)
	p.recordLatency(labels, latencySeconds)
}

func (p *assertsProcessorImpl) computeLatency(span ptrace.Span) float64 {
	return float64(span.EndTimestamp()-span.StartTimestamp()) / 1e9
}

func (p *assertsProcessorImpl) buildLabels(namespace string, service string, span ptrace.Span) prometheus.Labels {
	p.logger.Info("consumer.ConsumeTraces Capturing span duration metric for",
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
	return labels
}

func (p *assertsProcessorImpl) recordLatency(labels prometheus.Labels, latencySeconds float64) {
	p.latencyHistogram.With(labels).Observe(latencySeconds)
}

func (p *assertsProcessorImpl) shouldCaptureTrace(namespace string, serviceName string, traceId string, rootSpan ptrace.Span) bool {
	spanDuration := p.computeLatency(rootSpan)

	var entityKey = EntityKeyDto{
		EntityType: "Service",
		Name:       serviceName,
		Scope: map[string]string{
			"asserts_env": p.config.Env, "asserts_site": p.config.Site, "namespace": namespace,
		},
	}

	p.logger.Info("Sampling based on Root Span Duration",
		zap.String("Trace Id", traceId),
		zap.String("Entity Key", entityKey.AsString()),
		zap.Float64("Duration", spanDuration))

	load, found := p.latencyBounds.Get(entityKey.AsString())
	if !found {
		p.logger.Info("Thresholds not found for service. Will use default",
			zap.String("Entity", entityKey.AsString()),
			zap.Float64("Default Duration", p.config.DefaultLatencyThreshold))
		// Use default threshold the first time. The entity thresholds will be updated
		// in the map every minute in async
		p.entityKeys.Set(entityKey.AsString(), entityKey)
		return spanDuration > p.config.DefaultLatencyThreshold
	} else {
		var latencyBound, ok = load.Get(rootSpan.Name())
		if !ok {
			p.logger.Info("Threshold not found for request. Will use default",
				zap.String("Entity", entityKey.AsString()),
				zap.String("request", rootSpan.Name()),
				zap.Float64("Default Duration", p.config.DefaultLatencyThreshold))
			return spanDuration > p.config.DefaultLatencyThreshold
		} else {
			p.logger.Info("Threshold found for request",
				zap.String("Entity", entityKey.AsString()),
				zap.String("request", rootSpan.Name()),
				zap.Float64("Threshold", latencyBound.Upper))
			return spanDuration > latencyBound.Upper
		}
	}
}
