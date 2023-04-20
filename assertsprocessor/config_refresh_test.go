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

func (mcl *mockConfigListener) isUpdated(prevConfig *Config, currentConfig *Config) bool {
	return mcl.expectedIsUpdated
}

func (mcl *mockConfigListener) onUpdate(currentConfig *Config) error {
	mcl.expectedOnUpdate = true
	return mcl.expectedErr
}

func TestFetchConfig(t *testing.T) {
	cr := configRefresh{logger: logger}

	mockClient := &mockRestClient{
		expectedData: []byte(`{
			"CaptureMetrics": true,
			"RequestContextExps": {"default":[{"AttrName":"attribute1","Regex":"(Foo).+","Replacement":"$1"}]},
			"CaptureAttributesInMetric": ["rpc.system", "rpc.service"],
			"DefaultLatencyThreshold": 0.51,
			"Unknown": "foo"
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
	prevConfig := &Config{
		CaptureMetrics: false,
	}
	currentConfig := &Config{
		CaptureMetrics: true,
	}
	listener := &mockConfigListener{
		expectedIsUpdated: true,
		expectedErr:       nil,
	}

	cr := configRefresh{
		logger:          logger,
		config:          prevConfig,
		configListeners: []configListener{listener},
	}

	assert.False(t, cr.config.CaptureMetrics)
	cr.updateConfig(currentConfig)
	assert.True(t, listener.expectedOnUpdate)
	assert.True(t, cr.config.CaptureMetrics)
}

func TestUpdateConfigNoChange(t *testing.T) {
	prevConfig := &Config{
		CaptureMetrics: true,
	}
	currentConfig := &Config{
		CaptureMetrics: true,
	}
	listener := &mockConfigListener{
		expectedIsUpdated: false,
		expectedErr:       nil,
	}

	cr := configRefresh{
		logger:          logger,
		config:          prevConfig,
		configListeners: []configListener{listener},
	}

	assert.True(t, cr.config.CaptureMetrics)
	cr.updateConfig(currentConfig)
	assert.False(t, listener.expectedOnUpdate)
	assert.True(t, cr.config.CaptureMetrics)
}

func TestUpdateConfigError(t *testing.T) {
	prevConfig := &Config{
		CaptureMetrics: false,
	}
	currentConfig := &Config{
		CaptureMetrics: false,
	}
	listener := &mockConfigListener{
		expectedIsUpdated: true,
		expectedErr:       errors.New("update failed"),
	}

	cr := configRefresh{
		logger:          logger,
		config:          prevConfig,
		configListeners: []configListener{listener},
	}

	assert.False(t, cr.config.CaptureMetrics)
	cr.updateConfig(currentConfig)
	assert.True(t, listener.expectedOnUpdate)
	assert.False(t, cr.config.CaptureMetrics)
}

func TestFetchAndUpdateConfig(t *testing.T) {
	mockClient := &mockRestClient{
		expectedData: []byte(`{
			"DefaultLatencyThreshold": 0.51
		}`),
		expectedErr: nil,
	}
	prevConfig := &Config{
		DefaultLatencyThreshold: 0.5,
	}
	listener := &mockConfigListener{
		expectedIsUpdated: true,
		expectedErr:       nil,
	}

	cr := configRefresh{
		logger:          logger,
		config:          prevConfig,
		configListeners: []configListener{listener},
	}

	assert.Equal(t, 0.5, cr.config.DefaultLatencyThreshold)
	cr.fetchAndUpdateConfig(mockClient)
	assert.True(t, listener.expectedOnUpdate)
	assert.Equal(t, 0.51, cr.config.DefaultLatencyThreshold)
}
