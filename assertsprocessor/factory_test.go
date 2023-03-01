package assertsprocessor

import (
	"context"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/processor"
	"testing"
)

type dummyConsumer struct {
	items []*Item
	consumer.Traces
}

func (dC dummyConsumer) ConsumeTraces(ctx context.Context, trace ptrace.Traces) error {
	dC.items = append(dC.items, &Item{
		ctx:   &ctx,
		trace: &trace,
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
	assert.Equal(t, 100, pConfig.MaxTracesPerMinute)
	assert.Equal(t, 0.5, pConfig.DefaultLatencyThreshold)
}

func TestCreateProcessor(t *testing.T) {
	factory := NewFactory()
	var defaultConfig = factory.CreateDefaultConfig()
	var pConfig = defaultConfig.(*Config)
	assert.Equal(t, "dev", pConfig.Env)
	assert.Equal(t, "us-west-2", pConfig.Site)
	assert.Equal(t, 100, pConfig.MaxTracesPerMinute)
	assert.Equal(t, 0.5, pConfig.DefaultLatencyThreshold)

	ctx := context.Background()
	var createSettings = processor.CreateSettings{
		ID: component.NewIDWithName(component.DataTypeTraces, ""),
	}
	createSettings.Logger = logger
	var nextConsumer consumer.Traces = dummyConsumer{}
	var _processorRef, err = factory.CreateTracesProcessor(ctx, createSettings, config, nextConsumer)

	assert.Nil(t, err)
	assert.NotNil(t, _processorRef)

	var _assertsProcessor = _processorRef.(*assertsProcessorImpl)
	assert.Equal(t, config, *_assertsProcessor.config)
	assert.NotNil(t, logger, _assertsProcessor.logger)
	assert.NotNil(t, nextConsumer, _assertsProcessor.nextConsumer)
	assert.NotNil(t, _assertsProcessor.thresholdsHelper)
	assert.NotNil(t, _assertsProcessor.metricBuilder)
	assert.NotNil(t, _assertsProcessor.sampler)

	// Metric Builder
	assert.Equal(t, config, *_assertsProcessor.metricBuilder.config)
	assert.Equal(t, logger, _assertsProcessor.metricBuilder.logger)
	assert.NotNil(t, _assertsProcessor.metricBuilder.latencyHistogram)
	assert.NotNil(t, _assertsProcessor.metricBuilder.prometheusRegistry)

	// Threshold Helper
	assert.Equal(t, config, *_assertsProcessor.thresholdsHelper.config)
	assert.Equal(t, logger, _assertsProcessor.thresholdsHelper.logger)
	assert.NotNil(t, _assertsProcessor.thresholdsHelper.entityKeys)
	assert.NotNil(t, _assertsProcessor.thresholdsHelper.thresholds)
	assert.NotNil(t, _assertsProcessor.thresholdsHelper.stop)
	assert.NotNil(t, _assertsProcessor.thresholdsHelper.thresholdSyncTicker)

	// Sampler
	assert.Equal(t, config, *_assertsProcessor.sampler.config)
	assert.Equal(t, logger, _assertsProcessor.sampler.logger)
	assert.NotNil(t, _assertsProcessor.sampler.stop)
	assert.Equal(t, nextConsumer, _assertsProcessor.sampler.nextConsumer)
	assert.Equal(t, _assertsProcessor.thresholdsHelper, _assertsProcessor.sampler.thresholdHelper)
	assert.NotNil(t, _assertsProcessor.sampler.topTraces)
	assert.NotNil(t, _assertsProcessor.sampler.traceFlushTicker)
}
