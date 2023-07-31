package assertsprocessor

import (
	"context"
	"errors"
	"github.com/stretchr/testify/assert"
	"github.com/tilinna/clock"
	"net/http"
	"testing"
	"time"
)

type (
	mockConfigListener struct {
		expectedIsUpdated bool
		expectedOnUpdate  bool
		expectedErr       error
	}
)

func (mcl *mockConfigListener) isUpdated(currConfig *Config, newConfig *Config) bool {
	return mcl.expectedIsUpdated
}

func (mcl *mockConfigListener) onUpdate(newConfig *Config) error {
	mcl.expectedOnUpdate = true
	return mcl.expectedErr
}

func TestFetchConfig(t *testing.T) {
	mockClient := &mockRestClient{
		expectedData: []byte(`{
      "ignore_client_errors": true,
      "capture_metrics": true,
      "custom_attributes": {
        "asserts.request.context": {
          "asserts#api-server": [
            {
              "source_attributes": [
                "attr1",
                "attr2"
              ],
              "regex": "(.+?);(.+)",
              "replacement": "$1:$2"
            }
          ],
          "default": [
            {
              "source_attributes": [
                "attr1"
              ],
              "regex": "(.+)",
              "replacement": "$1"
            }
          ]
        }
      },
      "span_attributes": [{
        "attr_name": "asserts.request.context",
        "attr_configs": [
          {
            "namespace": "asserts",
            "service": "api-server",
            "rules": [
              {
                "source_attributes": [
                  "attr1",
                  "attr2"
                ],
                "regex": "(.+?);(.+)",
                "replacement": "$1:$2"
              }
            ]
          },
          {
            "rules": [
              {
                "source_attributes": [
                  "attr1"
                ],
                "regex": "(.+)",
                "replacement": "$1"
              }
            ]
          }
        ]
      }],
      "attributes_as_metric_labels": [
        "rpc.system",
        "rpc.service"
      ],
      "sampling_latency_threshold_seconds": 0.51,
      "unknown": "foo"
    }`),
		expectedErr: nil,
	}

	ctx := context.Background()
	cr := configRefresh{
		logger:           logger,
		config:           &config,
		configSyncTicker: clock.FromContext(ctx).NewTicker(10 * time.Millisecond),
		restClient:       mockClient,
	}

	go func() { cr.startUpdates() }()
	time.Sleep(20 * time.Millisecond)
	cr.stopUpdates()
	time.Sleep(10 * time.Millisecond)

	assert.Equal(t, http.MethodGet, mockClient.expectedMethod)
	assert.Equal(t, configApi, mockClient.expectedApi)
	assert.Nil(t, mockClient.expectedPayload)

	assert.NotNil(t, cr.config)
	assert.True(t, cr.config.IgnoreClientErrors)
	assert.True(t, cr.config.CaptureMetrics)
	assert.NotNil(t, cr.config.CustomAttributeConfigs)
	assert.Equal(t, 1, len(cr.config.CustomAttributeConfigs))
	assert.Equal(t, 2, len(cr.config.CustomAttributeConfigs["asserts.request.context"]))
	assert.Equal(t, 1, len(cr.config.CustomAttributeConfigs["asserts.request.context"]["default"]))
	assert.Equal(t, 1, len(cr.config.CustomAttributeConfigs["asserts.request.context"]["asserts#api-server"]))
	assert.Equal(t, 1, len(cr.config.SpanAttributes))
	assert.Equal(t, "asserts.request.context", cr.config.SpanAttributes[0].AttributeName)
	assert.Equal(t, 2, len(cr.config.SpanAttributes[0].AttributeConfigs))
	assert.Equal(t, "asserts", cr.config.SpanAttributes[0].AttributeConfigs[0].Namespace)
	assert.Equal(t, "api-server", cr.config.SpanAttributes[0].AttributeConfigs[0].Service)
	assert.Equal(t, 1, len(cr.config.SpanAttributes[0].AttributeConfigs[0].Rules))
	assert.Equal(t, "", cr.config.SpanAttributes[0].AttributeConfigs[1].Namespace)
	assert.Equal(t, "", cr.config.SpanAttributes[0].AttributeConfigs[1].Service)
	assert.Equal(t, 1, len(cr.config.SpanAttributes[0].AttributeConfigs[1].Rules))
	assert.NotNil(t, cr.config.CaptureAttributesInMetric)
	assert.Equal(t, 2, len(cr.config.CaptureAttributesInMetric))
	assert.Equal(t, "rpc.system", cr.config.CaptureAttributesInMetric[0])
	assert.Equal(t, "rpc.service", cr.config.CaptureAttributesInMetric[1])
	assert.Equal(t, 0.51, cr.config.DefaultLatencyThreshold)
}

func TestFetchConfigUnmarshalError(t *testing.T) {
	cr := configRefresh{logger: logger}

	_, err := cr.fetchConfig(&mockRestClient{
		expectedData: []byte(`invalid json`),
		expectedErr:  nil,
	})

	assert.NotNil(t, err)
}

func TestUpdateConfig(t *testing.T) {
	currConfig := &Config{
		CaptureMetrics: false,
	}
	newConfig := &Config{
		CaptureMetrics: true,
	}
	listener := &mockConfigListener{
		expectedIsUpdated: true,
		expectedErr:       nil,
	}

	cr := configRefresh{
		logger:          logger,
		config:          currConfig,
		configListeners: []configListener{listener},
	}

	assert.False(t, cr.config.CaptureMetrics)
	cr.updateConfig(newConfig)
	assert.True(t, listener.expectedOnUpdate)
	assert.True(t, cr.config.CaptureMetrics)
}

func TestUpdateConfigNoChange(t *testing.T) {
	currConfig := &Config{
		CaptureMetrics: true,
	}
	newConfig := &Config{
		CaptureMetrics: true,
	}
	listener := &mockConfigListener{
		expectedIsUpdated: false,
		expectedErr:       nil,
	}

	cr := configRefresh{
		logger:          logger,
		config:          currConfig,
		configListeners: []configListener{listener},
	}

	assert.True(t, cr.config.CaptureMetrics)
	cr.updateConfig(newConfig)
	assert.False(t, listener.expectedOnUpdate)
	assert.True(t, cr.config.CaptureMetrics)
}

func TestUpdateConfigError(t *testing.T) {
	currConfig := &Config{
		CaptureMetrics: false,
	}
	newConfig := &Config{
		CaptureMetrics: false,
	}
	listener := &mockConfigListener{
		expectedIsUpdated: true,
		expectedErr:       errors.New("update failed"),
	}

	cr := configRefresh{
		logger:          logger,
		config:          currConfig,
		configListeners: []configListener{listener},
	}

	assert.False(t, cr.config.CaptureMetrics)
	cr.updateConfig(newConfig)
	assert.True(t, listener.expectedOnUpdate)
	assert.False(t, cr.config.CaptureMetrics)
}

func TestFetchAndUpdateConfig(t *testing.T) {
	mockClient := &mockRestClient{
		expectedData: []byte(`{
			"sampling_latency_threshold_seconds": 0.51
		}`),
		expectedErr: nil,
	}
	currConfig := &Config{
		DefaultLatencyThreshold: 0.5,
	}
	listener := &mockConfigListener{
		expectedIsUpdated: true,
		expectedErr:       nil,
	}

	cr := configRefresh{
		logger:          logger,
		config:          currConfig,
		configListeners: []configListener{listener},
	}

	assert.Equal(t, 0.5, cr.config.DefaultLatencyThreshold)
	cr.fetchAndUpdateConfig(mockClient)
	assert.True(t, listener.expectedOnUpdate)
	assert.Equal(t, 0.51, cr.config.DefaultLatencyThreshold)
}
