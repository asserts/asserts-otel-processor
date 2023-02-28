package assertsprocessor

import (
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

type sampler struct {
	logger          *zap.Logger
	config          Config
	thresholdHelper thresholdHelper
}

func (p *sampler) shouldCaptureTrace(namespace string, serviceName string, traceId string, rootSpan ptrace.Span) bool {
	spanDuration := computeLatency(rootSpan)

	var entityKey = EntityKeyDto{
		EntityType: "Service",
		Name:       serviceName,
		Scope: map[string]string{
			"asserts_env": p.config.Env, "asserts_site": p.config.Site, "namespace": namespace,
		},
	}

	p.logger.Info("Sampling check based on Root Span Duration",
		zap.String("Trace Id", traceId),
		zap.String("Entity Key", entityKey.AsString()),
		zap.Float64("Duration", spanDuration))

	threshold := p.thresholdHelper.getThreshold(namespace, serviceName, rootSpan.Name())
	return spanDuration > threshold
}
