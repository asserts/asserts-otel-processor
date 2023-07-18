package assertsprocessor

import (
	"context"
	"github.com/puzpuzpuz/xsync/v2"
	"github.com/stretchr/testify/assert"
	"github.com/tilinna/clock"
	"go.uber.org/zap"
	"sync"
	"testing"
	"time"
)

func TestGetThresholdDefaultThreshold(t *testing.T) {
	logger, _ := zap.NewProduction()
	th := thresholdHelper{
		logger: logger,
		config: &Config{
			Env:                     "dev",
			Site:                    "us-west-2",
			AssertsServer:           &map[string]string{"endpoint": "http://localhost:8030"},
			DefaultLatencyThreshold: 0.5,
		},
		thresholds: xsync.NewMapOf[map[string]*ThresholdDto](),
		entityKeys: xsync.NewMapOf[EntityKeyDto](),
		rwMutex:    &sync.RWMutex{},
	}

	dto := EntityKeyDto{
		Type: "Service", Name: "api-server", Scope: map[string]string{
			"env": "dev", "site": "us-west-2", "namespace": "platform",
		},
	}
	assert.Equal(t, 0.5, th.getThreshold("platform", "api-server", "123"))
	th.entityKeys.Range(func(key string, entityKey EntityKeyDto) bool {
		assert.Equal(t, dto.AsString(), key)
		assert.Equal(t, dto, entityKey)
		return true
	})
}

func TestGetRequestThresholdFound(t *testing.T) {
	logger, _ := zap.NewProduction()
	var th = thresholdHelper{
		logger: logger,
		config: &Config{
			Env:                     "dev",
			Site:                    "us-west-2",
			AssertsServer:           &map[string]string{"endpoint": "http://localhost:8030"},
			DefaultLatencyThreshold: 0.5,
		},
		thresholds: xsync.NewMapOf[map[string]*ThresholdDto](),
		entityKeys: xsync.NewMapOf[EntityKeyDto](),
		rwMutex:    &sync.RWMutex{},
	}

	dto := EntityKeyDto{
		Type: "Service", Name: "api-server", Scope: map[string]string{
			"env": "dev", "site": "us-west-2", "namespace": "platform",
		},
	}
	th.entityKeys.Store(dto.AsString(), dto)

	byRequest := map[string]*ThresholdDto{}
	th.thresholds.Store(dto.AsString(), byRequest)

	byRequest["/v1/latency-thresholds"] = &ThresholdDto{
		RequestContext:    "/v1/latency-thresholds",
		LatencyUpperBound: 1,
	}

	byRequest[""] = &ThresholdDto{
		RequestContext:    "",
		LatencyUpperBound: 2,
	}

	assert.Equal(t, float64(1), th.getThreshold("platform", "api-server", "/v1/latency-thresholds"))
}

func TestGetServiceDefaultThresholdFound(t *testing.T) {
	logger, _ := zap.NewProduction()
	var th = thresholdHelper{
		logger: logger,
		config: &Config{
			Env:                     "dev",
			Site:                    "us-west-2",
			AssertsServer:           &map[string]string{"endpoint": "http://localhost:8030"},
			DefaultLatencyThreshold: 0.5,
		},
		thresholds: xsync.NewMapOf[map[string]*ThresholdDto](),
		entityKeys: xsync.NewMapOf[EntityKeyDto](),
		rwMutex:    &sync.RWMutex{},
	}

	dto := EntityKeyDto{
		Type: "Service", Name: "api-server", Scope: map[string]string{
			"env": "dev", "site": "us-west-2", "namespace": "platform",
		},
	}
	th.entityKeys.Store(dto.AsString(), dto)

	byRequest := map[string]*ThresholdDto{}
	th.thresholds.Store(dto.AsString(), byRequest)

	byRequest[""] = &ThresholdDto{
		RequestContext:    "",
		LatencyUpperBound: 1,
	}

	assert.Equal(t, float64(1), th.getThreshold("platform", "api-server", "/v1/latency-thresholds"))
}

func TestStopUpdates(t *testing.T) {
	logger, _ := zap.NewProduction()
	var th = thresholdHelper{
		logger: logger,
		config: &Config{
			Env:                     "dev",
			Site:                    "us-west-2",
			AssertsServer:           &map[string]string{"endpoint": "http://localhost:8030"},
			DefaultLatencyThreshold: 0.5,
		},
		thresholds: xsync.NewMapOf[map[string]*ThresholdDto](),
		entityKeys: xsync.NewMapOf[EntityKeyDto](),
		stop:       make(chan bool),
	}
	th.stopUpdates()
	assert.True(t, <-th.stop)
}

func TestUpdateThresholds(t *testing.T) {
	logger, _ := zap.NewProduction()
	ctx := context.Background()
	config := &Config{
		Env:  "dev",
		Site: "us-west-2",
		AssertsServer: &map[string]string{
			"endpoint": "http://localhost:8030",
			"user":     "user",
			"password": "password",
		},
		DefaultLatencyThreshold: 0.5,
	}
	mockClient := mockRestClient{
		expectedData: []byte(`[
			{
				"entityKey": {
					"type": "Service",
					"name": "api-server",
					"scope": {
						"env": "dev",
						"site": "us-west-2"
					}
				},
				"latencyThresholds": [
					{
						"requestType": "inbound",
						"requestContext": "/v4/rules",
						"upperThreshold": 0.25
					},
					{
						"requestType": "inbound",
						"requestContext": "/v1/assertions",
						"upperThreshold": 0.5
					}
				]
			},
			{
				"entityKey": {
					"type": "Service",
					"name": "model-builder",
					"scope": {
						"env": "dev",
						"site": "us-west-2"
					}
				},
				"latencyThresholds": [
					{
						"requestType": "method",
						"requestContext": "run",
						"upperThreshold": 1.5
					}
				]
			}
		]`),
		expectedErr: nil,
	}
	var th = thresholdHelper{
		logger:              logger,
		config:              config,
		thresholds:          xsync.NewMapOf[map[string]*ThresholdDto](),
		entityKeys:          xsync.NewMapOf[EntityKeyDto](),
		stop:                make(chan bool),
		thresholdSyncTicker: clock.FromContext(ctx).NewTicker(10 * time.Millisecond),
		rc:                  &mockClient,
	}
	entityKey1 := EntityKeyDto{
		Type: "Service", Name: "api-server", Scope: map[string]string{
			"env": "dev", "site": "us-west-2",
		},
	}
	entityKey2 := EntityKeyDto{
		Type: "Service", Name: "model-builder", Scope: map[string]string{
			"env": "dev", "site": "us-west-2",
		},
	}
	th.entityKeys.Store(entityKey1.AsString(), entityKey1)
	th.entityKeys.Store(entityKey2.AsString(), entityKey2)

	value, _ := th.thresholds.Load(entityKey1.AsString())
	assert.Nil(t, value)

	go func() { th.startUpdates() }()
	time.Sleep(20 * time.Millisecond)
	th.stopUpdates()
	time.Sleep(10 * time.Millisecond)

	assert.Equal(t, latencyThresholdsApi, mockClient.expectedApi)
	assert.Equal(t, "POST", mockClient.expectedMethod)

	thresholds, _ := th.thresholds.Load(entityKey1.AsString())
	assert.NotNil(t, thresholds)

	assert.Equal(t, 2, len(thresholds))
	assert.NotNil(t, thresholds["/v4/rules"])
	assert.Equal(t, "inbound", thresholds["/v4/rules"].RequestType)
	assert.Equal(t, "/v4/rules", thresholds["/v4/rules"].RequestContext)
	assert.Equal(t, 0.25, thresholds["/v4/rules"].LatencyUpperBound)
	assert.NotNil(t, thresholds["/v1/assertions"])
	assert.Equal(t, "inbound", thresholds["/v1/assertions"].RequestType)
	assert.Equal(t, "/v1/assertions", thresholds["/v1/assertions"].RequestContext)
	assert.Equal(t, 0.5, thresholds["/v1/assertions"].LatencyUpperBound)

	thresholds, _ = th.thresholds.Load(entityKey2.AsString())
	assert.NotNil(t, thresholds)

	assert.Equal(t, 1, len(thresholds))
	assert.NotNil(t, thresholds["run"])
	assert.Equal(t, "method", thresholds["run"].RequestType)
	assert.Equal(t, "run", thresholds["run"].RequestContext)
	assert.Equal(t, 1.5, thresholds["run"].LatencyUpperBound)
}

func TestUpdateThresholdsUnmarshalError(t *testing.T) {
	logger, _ := zap.NewProduction()
	ctx := context.Background()
	config := &Config{
		Env:  "dev",
		Site: "us-west-2",
		AssertsServer: &map[string]string{
			"endpoint": "http://localhost:8030",
			"user":     "user",
			"password": "password",
		},
		DefaultLatencyThreshold: 0.5,
	}
	var th = thresholdHelper{
		logger:              logger,
		config:              config,
		thresholds:          xsync.NewMapOf[map[string]*ThresholdDto](),
		entityKeys:          xsync.NewMapOf[EntityKeyDto](),
		stop:                make(chan bool),
		thresholdSyncTicker: clock.FromContext(ctx).NewTicker(1 * time.Millisecond),
		rc: &mockRestClient{
			expectedData: []byte(`invalid json`),
			expectedErr:  nil,
		},
	}
	entityKey := EntityKeyDto{
		Type: "Service", Name: "api-server", Scope: map[string]string{
			"env": "dev", "site": "us-west-2",
		},
	}
	th.entityKeys.Store(entityKey.AsString(), entityKey)

	value, _ := th.thresholds.Load(entityKey.AsString())
	assert.Nil(t, value)

	go func() { th.startUpdates() }()
	time.Sleep(2 * time.Millisecond)
	th.stopUpdates()

	thresholds, _ := th.thresholds.Load(entityKey.AsString())
	assert.Nil(t, thresholds)
}

func TestThresholdsIsUpdated(t *testing.T) {
	currConfig := &Config{
		DefaultLatencyThreshold: 0.5,
	}
	newConfig := &Config{
		DefaultLatencyThreshold: 0.51,
	}

	logger, _ := zap.NewProduction()
	var th = thresholdHelper{
		logger:  logger,
		rwMutex: &sync.RWMutex{},
	}

	assert.False(t, th.isUpdated(currConfig, currConfig))
	assert.True(t, th.isUpdated(currConfig, newConfig))
}

func TestThresholdsOnUpdate(t *testing.T) {
	currConfig := &Config{
		DefaultLatencyThreshold: 0.5,
	}
	newConfig := &Config{
		DefaultLatencyThreshold: 0.51,
	}

	logger, _ := zap.NewProduction()
	var th = thresholdHelper{
		logger:  logger,
		config:  currConfig,
		rwMutex: &sync.RWMutex{},
	}

	assert.Equal(t, .5, th.getDefaultThreshold())
	err := th.onUpdate(newConfig)
	assert.Nil(t, err)
	assert.Equal(t, .51, th.getDefaultThreshold())
}
