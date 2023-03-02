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
	logger           *zap.Logger
	config           *Config
	nextConsumer     consumer.Traces
	thresholdsHelper *thresholdHelper
	metricBuilder    *metricHelper
	sampler          *sampler
}

// Capabilities implements the consumer.Traces interface.
func (p *assertsProcessorImpl) Capabilities() consumer.Capabilities {
	p.logger.Info("consumer.Capabilities callback")
	return consumer.Capabilities{MutatesData: false}
}

// Start implements the component.Component interface.
func (p *assertsProcessorImpl) Start(ctx context.Context, host component.Host) error {
	p.logger.Info("consumer.Start callback")
	err := p.metricBuilder.compileSpanFilterRegexps()
	if err == nil {
		if p.config.AssertsServer != nil && (*p.config.AssertsServer)["endpoint"] != "" {
			go p.thresholdsHelper.updateThresholds()
		} else {
			p.logger.Info("Asserts Server not specified. No dynamic thresholds")
		}
		go p.sampler.flushTraces()
	}
	return err
}

// Shutdown implements the component.Component interface
func (p *assertsProcessorImpl) Shutdown(context.Context) error {
	p.logger.Info("consumer.Shutdown")
	p.thresholdsHelper.stopUpdates()
	p.sampler.stopFlushing()
	return nil
}

// ConsumeTraces implements the consumer.Traces interface.
// Samples the trace if the latency threshold exceeds for the root spans.
// Also generates span metrics for the spans of interest
func (p *assertsProcessorImpl) ConsumeTraces(ctx context.Context, traces ptrace.Traces) error {
	return spanIterator(p.logger, ctx, traces, p.processSpans)
}

func (p *assertsProcessorImpl) processSpans(ctx context.Context,
	traces ptrace.Traces, traceId string, spanSet *resourceSpanGroup) error {
	p.sampler.sampleTrace(ctx, traces, traceId, spanSet)

	for _, _span := range spanSet.rootSpans {
		p.metricBuilder.captureMetrics(spanSet.namespace, spanSet.service, _span)
	}

	for _, _span := range spanSet.nestedSpans {
		p.metricBuilder.captureMetrics(spanSet.namespace, spanSet.service, _span)
	}
	return nil
}
