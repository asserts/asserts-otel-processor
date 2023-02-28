package assertsprocessor

import (
	"context"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
	"go.uber.org/zap"
)

// The methods from a Span that we care for to enable easy mocking

type assertsProcessorImpl struct {
	logger           *zap.Logger
	config           Config
	nextConsumer     consumer.Traces
	thresholdsHelper thresholdHelper
	metricBuilder    metricHelper
	sampler          sampler
}

// Capabilities implements the consumer interface.
func (p *assertsProcessorImpl) Capabilities() consumer.Capabilities {
	p.logger.Info("consumer.Capabilities callback")
	return consumer.Capabilities{MutatesData: false}
}

// Start implements the consumer interface.
func (p *assertsProcessorImpl) Start(ctx context.Context, host component.Host) error {
	p.logger.Info("consumer.Start callback")
	err := p.metricBuilder.compileSpanFilterRegexps()
	if err == nil {
		if p.config.AssertsServer != "" {
			go p.thresholdsHelper.updateThresholds()
		} else {
			p.logger.Info("Asserts Server not specified. No dynamic thresholds")
		}
	}
	return err
}

func (p *assertsProcessorImpl) Shutdown(context.Context) error {
	p.logger.Info("consumer.Shutdown")
	p.thresholdsHelper.stopUpdates()
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
					sampleTrace = p.sampler.shouldCaptureTrace(namespace, serviceName, traceId, span)
				}
				if p.metricBuilder.shouldCaptureMetrics(span) {
					p.metricBuilder.captureMetrics(namespace, serviceName, span)
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
