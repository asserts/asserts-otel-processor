package assertsprocessor

import (
	"context"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/processor"
	"go.uber.org/zap"
	"net/http"
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

func TestCreateProcessorDefaultConfig(t *testing.T) {
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
	assert.Equal(t, nextConsumer, _assertsProcessor.nextConsumer)
	assert.NotNil(t, _assertsProcessor.spanEnricher)
	assert.NotNil(t, _assertsProcessor.metricBuilder)
	assert.NotNil(t, _assertsProcessor.sampler)
	assert.NotNil(t, _assertsProcessor.configRefresh)
	assert.NotNil(t, _assertsProcessor.rwMutex)

	// Metric Builder
	assert.Equal(t, config, *_assertsProcessor.metricBuilder.config)
	assert.Equal(t, logger, _assertsProcessor.metricBuilder.logger)
	assert.NotNil(t, _assertsProcessor.metricBuilder.prometheusRegistry)
	assert.NotNil(t, _assertsProcessor.metricBuilder.latencyHistogram)
	assert.NotNil(t, _assertsProcessor.metricBuilder.sampledTraceCount)
	assert.NotNil(t, _assertsProcessor.metricBuilder.totalTraceCount)
	assert.NotNil(t, _assertsProcessor.metricBuilder.requestContextsByService)
	assert.NotNil(t, _assertsProcessor.metricBuilder.rwMutex)

	// Sampler
	assert.Equal(t, config, *_assertsProcessor.sampler.config)
	assert.Equal(t, logger, _assertsProcessor.sampler.logger)
	assert.NotNil(t, _assertsProcessor.sampler.stop)
	assert.Equal(t, nextConsumer, _assertsProcessor.sampler.nextConsumer)
	assert.NotNil(t, _assertsProcessor.sampler.topTracesByService)
	assert.NotNil(t, _assertsProcessor.sampler.traceFlushTicker)
	assert.NotNil(t, _assertsProcessor.sampler.metricHelper)
	assert.Equal(t, _assertsProcessor.metricBuilder, _assertsProcessor.sampler.metricHelper)

	// Threshold Helper
	assert.Equal(t, config, *_assertsProcessor.sampler.thresholdHelper.config)
	assert.Equal(t, logger, _assertsProcessor.sampler.thresholdHelper.logger)
	assert.NotNil(t, _assertsProcessor.sampler.thresholdHelper.entityKeys)
	assert.NotNil(t, _assertsProcessor.sampler.thresholdHelper.thresholds)
	assert.NotNil(t, _assertsProcessor.sampler.thresholdHelper.stop)
	assert.NotNil(t, _assertsProcessor.sampler.thresholdHelper.thresholdSyncTicker)
	assert.NotNil(t, _assertsProcessor.sampler.thresholdHelper.rc)
	assert.NotNil(t, _assertsProcessor.sampler.thresholdHelper.rwMutex)

	// Config Refresh
	assert.Equal(t, config, *_assertsProcessor.configRefresh.config)
	assert.Equal(t, logger, _assertsProcessor.configRefresh.logger)
	assert.NotNil(t, _assertsProcessor.configRefresh.configSyncTicker)
	assert.NotNil(t, _assertsProcessor.configRefresh.stop)
	assert.NotNil(t, _assertsProcessor.configRefresh.restClient)
	assert.NotNil(t, _assertsProcessor.configRefresh.configListeners)
	assert.Equal(t, 4, len(_assertsProcessor.configRefresh.configListeners))

	_ = _assertsProcessor.metricBuilder.stopExporter()
}

func TestCreateProcessorMergeFetchedConfig(t *testing.T) {
	factory := NewFactory()
	ctx := context.Background()
	var createSettings = processor.CreateSettings{
		ID: component.NewIDWithName(component.DataTypeTraces, ""),
	}
	createSettings.Logger = logger
	var nextConsumer consumer.Traces = dummyConsumer{}

	mockClient := &mockRestClient{
		expectedData: []byte(`{
      "capture_metrics": true,
      "custom_attributes": {
        "asserts.request.context": {
          "asserts#api-server": [
            {
              "source_attributes": [
                "attr1",
                "attr2"
              ],
              "regexp": "(.+?);(.+)",
              "replacement": "$1:$2"
            }
          ],
          "default": [
            {
              "source_attributes": [
                "attr1"
              ],
              "regexp": "+",
              "replacement": "$1"
            }
          ]
        }
      },
      "attributes_as_metric_labels": [
        "rpc.system",
        "rpc.service"
      ],
      "sampling_latency_threshold_seconds": 0.51,
      "unknown": "foo"
    }`),
		expectedErr: nil,
	}
	restClientFactory = func(logger *zap.Logger, pConfig *Config) restClient {
		return mockClient
	}

	assert.False(t, config.CaptureMetrics)
	assert.Nil(t, config.CustomAttributeConfigs)
	assert.Nil(t, config.CaptureAttributesInMetric)
	assert.Equal(t, 0.5, config.DefaultLatencyThreshold)

	var _processorRef, err = factory.CreateTracesProcessor(ctx, createSettings, &config, nextConsumer)

	assert.NotNil(t, err)
	assert.Nil(t, _processorRef)

	var _assertsProcessor = _processorRef.(*assertsProcessorImpl)
	// Compilation of a bad regex will cause processor creation to fail in this test
	assert.Nil(t, _assertsProcessor)

	assert.Equal(t, http.MethodGet, mockClient.expectedMethod)
	assert.Equal(t, configApi, mockClient.expectedApi)
	assert.Nil(t, mockClient.expectedPayload)

	assert.True(t, config.CaptureMetrics)
	assert.NotNil(t, config)
	assert.True(t, config.CaptureMetrics)
	assert.NotNil(t, config.CustomAttributeConfigs)
	assert.Equal(t, 1, len(config.CustomAttributeConfigs))
	assert.Equal(t, 2, len(config.CustomAttributeConfigs["asserts.request.context"]))
	assert.Equal(t, 1, len(config.CustomAttributeConfigs["asserts.request.context"]["default"]))
	assert.Equal(t, 1, len(config.CustomAttributeConfigs["asserts.request.context"]["asserts#api-server"]))
	assert.NotNil(t, config.CaptureAttributesInMetric)
	assert.Equal(t, 5, len(config.CaptureAttributesInMetric))
	assert.Equal(t, "rpc.system", config.CaptureAttributesInMetric[0])
	assert.Equal(t, "rpc.service", config.CaptureAttributesInMetric[1])
	assert.Equal(t, 0.51, config.DefaultLatencyThreshold)
}
