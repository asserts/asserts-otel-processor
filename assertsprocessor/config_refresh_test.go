package assertsprocessor

import (
	"errors"
	"github.com/stretchr/testify/assert"
	"net/http"
	"testing"
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
	cr := configRefresh{logger: logger}

	mockClient := &mockRestClient{
		expectedData: []byte(`{
			"capture_metrics": true,
			"request_context_regex": {"default":[{"attr_name":"attribute1","regex":"(Foo).+","replacement":"$1"}]},
			"attributes_as_metric_labels": ["rpc.system", "rpc.service"],
			"sampling_latency_threshold_seconds": 0.51,
			"unknown": "foo"
		}`),
		expectedErr: nil,
	}
	config, err := cr.fetchConfig(mockClient)

	assert.Equal(t, http.MethodGet, mockClient.expectedMethod)
	assert.Equal(t, configApi, mockClient.expectedApi)
	assert.Nil(t, mockClient.expectedPayload)

	assert.NotNil(t, config)
	assert.True(t, config.CaptureMetrics)
	assert.NotNil(t, config.RequestContextExps)
	assert.Equal(t, 1, len(config.RequestContextExps))
	assert.Equal(t, 1, len(config.RequestContextExps["default"]))
	assert.Equal(t, "attribute1", config.RequestContextExps["default"][0].AttrName)
	assert.Equal(t, "(Foo).+", config.RequestContextExps["default"][0].Regex)
	assert.Equal(t, "$1", config.RequestContextExps["default"][0].Replacement)
	assert.NotNil(t, config.CaptureAttributesInMetric)
	assert.Equal(t, 2, len(config.CaptureAttributesInMetric))
	assert.Equal(t, "rpc.system", config.CaptureAttributesInMetric[0])
	assert.Equal(t, "rpc.service", config.CaptureAttributesInMetric[1])
	assert.Equal(t, 0.51, config.DefaultLatencyThreshold)
	assert.Nil(t, err)
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
