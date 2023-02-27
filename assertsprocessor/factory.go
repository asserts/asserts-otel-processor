package assertsprocessor

import (
	"context"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/tilinna/clock"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
	"go.uber.org/zap"
	"regexp"
	"sync"
	"time"
)

const (
	// The value of "type" key in configuration.
	typeStr = "assertsprocessor"
	// The stability level of the processor.
	stability = component.StabilityLevelDevelopment
)

// NewFactory creates a factory for the assertsotelprocessor processor.
func NewFactory() processor.Factory {
	return processor.NewFactory(
		typeStr,
		createDefaultConfig,
		processor.WithTraces(createTracesProcessor, stability),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		DefaultLatencyThreshold: 0.5,
	}
}

func createTracesProcessor(ctx context.Context, params processor.CreateSettings, cfg component.Config, nextConsumer consumer.Traces) (processor.Traces, error) {
	return newProcessor(params.Logger, ctx, cfg, nextConsumer)
}

func newProcessor(logger *zap.Logger, ctx context.Context, config component.Config, nextConsumer consumer.Traces) (*assertsProcessorImpl, error) {
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
		thresholdSyncTicker: clock.FromContext(ctx).NewTicker(time.Minute),
		latencyBounds:       sync.Map{},
	}

	// Start the prometheus server on port 9465
	p.prometheusRegistry = prometheus.NewRegistry()
	go startExporter(p.prometheusRegistry)

	return p, nil
}
