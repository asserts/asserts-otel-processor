package assertsprocessor

import (
	"context"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/processor"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type dummyConsumer struct {
	items []*Item
	consumer.Traces
}

func (dC dummyConsumer) ConsumeTraces(ctx context.Context, trace ptrace.Traces) error {
	dC.items = append(dC.items, &Item{
		ctx: &ctx,
	})
	return nil
}

func TestNewFactory(t *testing.T) {
	factory := NewFactory()
	assert.NotNil(t, factory)
}

func TestDefaultConfig(t *testing.T) {
	factory := NewFactory()
	var defaultConfig = factory.CreateDefaultConfig()
	var pConfig = defaultConfig.(*Config)
	assert.Equal(t, "dev", pConfig.Env)
	assert.Equal(t, "us-west-2", pConfig.Site)
	assert.Equal(t, 100, pConfig.LimitPerService)
	assert.Equal(t, float64(3), pConfig.DefaultLatencyThreshold)
}

func TestCreateProcessor(t *testing.T) {
	factory := NewFactory()
	ctx := context.Background()
	var createSettings = processor.CreateSettings{
		ID: component.NewIDWithName(component.DataTypeTraces, ""),
	}
	createSettings.Logger = logger
	var nextConsumer consumer.Traces = dummyConsumer{}
	var _processorRef, err = factory.CreateTracesProcessor(ctx, createSettings, &config, nextConsumer)

	assert.Nil(t, err)
	assert.NotNil(t, _processorRef)

	var _assertsProcessor = _processorRef.(*assertsProcessorImpl)
	assert.Equal(t, config, *_assertsProcessor.config)
	assert.NotNil(t, logger, _assertsProcessor.logger)
	assert.NotNil(t, nextConsumer, _assertsProcessor.nextConsumer)
	assert.NotNil(t, _assertsProcessor.metricBuilder)
	assert.NotNil(t, _assertsProcessor.sampler)

	// Metric Builder
	assert.Equal(t, config, *_assertsProcessor.metricBuilder.config)
	assert.Equal(t, logger, _assertsProcessor.metricBuilder.logger)
	assert.NotNil(t, _assertsProcessor.metricBuilder.prometheusRegistry)
	assert.NotNil(t, _assertsProcessor.metricBuilder.spanMatcher)
	assert.NotNil(t, _assertsProcessor.metricBuilder.latencyHistogram)
	assert.NotNil(t, _assertsProcessor.metricBuilder.sampledTraceCount)
	assert.NotNil(t, _assertsProcessor.metricBuilder.requestContextsByService)

	// Sampler
	assert.Equal(t, config, *_assertsProcessor.sampler.config)
	assert.Equal(t, logger, _assertsProcessor.sampler.logger)
	assert.NotNil(t, _assertsProcessor.sampler.stop)
	assert.Equal(t, nextConsumer, _assertsProcessor.sampler.nextConsumer)
	assert.NotNil(t, _assertsProcessor.sampler.topTracesByService)
	assert.NotNil(t, _assertsProcessor.sampler.traceFlushTicker)
	assert.NotNil(t, _assertsProcessor.sampler.spanMatcher)
	assert.NotNil(t, _assertsProcessor.sampler.metricHelper)
	assert.Equal(t, _assertsProcessor.metricBuilder, _assertsProcessor.sampler.metricHelper)

	// Threshold Helper
	assert.Equal(t, config, *_assertsProcessor.sampler.thresholdHelper.config)
	assert.Equal(t, logger, _assertsProcessor.sampler.thresholdHelper.logger)
	assert.NotNil(t, _assertsProcessor.sampler.thresholdHelper.entityKeys)
	assert.NotNil(t, _assertsProcessor.sampler.thresholdHelper.thresholds)
	assert.NotNil(t, _assertsProcessor.sampler.thresholdHelper.stop)
	assert.NotNil(t, _assertsProcessor.sampler.thresholdHelper.thresholdSyncTicker)
}
