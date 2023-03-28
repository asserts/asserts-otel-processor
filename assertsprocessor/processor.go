package assertsprocessor

import (
	"context"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

// The methods from a Span that we care for to enable easy mocking

type assertsProcessorImpl struct {
	logger        *zap.Logger
	config        *Config
	nextConsumer  consumer.Traces
	metricBuilder *metricHelper
	sampler       *sampler
}

// Capabilities implements the consumer.Traces interface.
func (p *assertsProcessorImpl) Capabilities() consumer.Capabilities {
	p.logger.Info("consumer.Capabilities callback")
	return consumer.Capabilities{MutatesData: true}
}

// Start implements the component.Component interface.
func (p *assertsProcessorImpl) Start(ctx context.Context, host component.Host) error {
	p.logger.Info("consumer.Start callback")
	p.sampler.startProcessing()
	return nil
}

// Shutdown implements the component.Component interface
func (p *assertsProcessorImpl) Shutdown(context.Context) error {
	p.logger.Info("consumer.Shutdown")
	p.sampler.stopProcessing()
	return nil
}

// ConsumeTraces implements the consumer.Traces interface.
// Samples the traceStruct if the latency threshold exceeds for the root spans.
// Also generates span metrics for the spans of interest
func (p *assertsProcessorImpl) ConsumeTraces(ctx context.Context, traces ptrace.Traces) error {
	return spanIterator(ctx, traces, p.processSpans)
}

func (p *assertsProcessorImpl) processSpans(ctx context.Context, traces *resourceTraces) error {
	p.sampler.sampleTraces(ctx, traces)

	if p.config.CaptureMetrics {
		for _, aTrace := range *traces.traceById {
			if aTrace.rootSpan != nil {
				p.metricBuilder.captureMetrics(traces.namespace, traces.service, aTrace.rootSpan)
			}

			for _, entrySpan := range aTrace.entrySpans {
				p.metricBuilder.captureMetrics(traces.namespace, traces.service, entrySpan)
			}

			for _, exitSpan := range aTrace.exitSpans {
				p.metricBuilder.captureMetrics(traces.namespace, traces.service, exitSpan)
			}
		}
	}

	return nil
}
