package assertsprocessor

import (
	"context"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/tilinna/clock"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
	"go.uber.org/zap"
	"regexp"
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
		Env:                     "dev",
		Site:                    "us-west-2",
		DefaultLatencyThreshold: 0.5,
	}
}

func createTracesProcessor(ctx context.Context, params processor.CreateSettings, cfg component.Config, nextConsumer consumer.Traces) (processor.Traces, error) {
	return newProcessor(params.Logger, ctx, cfg, nextConsumer)
}

func newProcessor(logger *zap.Logger, ctx context.Context, config component.Config, nextConsumer consumer.Traces) (*assertsProcessorImpl, error) {
	logger.Info("Creating assertsotelprocessor")
	pConfig := config.(*Config)

	thresholdsHelper := thresholdHelper{
		thresholdSyncTicker: clock.FromContext(ctx).NewTicker(time.Minute),
		thresholds:          cmap.New[cmap.ConcurrentMap[string, ThresholdDto]](),
		entityKeys:          cmap.New[EntityKeyDto](),
	}

	metricsHelper := metricHelper{
		config:                *pConfig,
		attributeValueRegExps: &map[string]regexp.Regexp{},
	}

	p := &assertsProcessorImpl{
		logger:           logger,
		config:           *pConfig,
		nextConsumer:     nextConsumer,
		metricBuilder:    metricsHelper,
		thresholdsHelper: thresholdsHelper,
	}

	metricsHelper.buildHistogram()
	go metricsHelper.startExporter()

	return p, nil
}
