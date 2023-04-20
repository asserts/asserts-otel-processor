package assertsprocessor

import (
	"context"
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
		thresholds: &sync.Map{},
		entityKeys: &sync.Map{},
		rwMutex:    &sync.RWMutex{},
	}

	dto := EntityKeyDto{
		Type: "Service", Name: "api-server", Scope: map[string]string{
			"env": "dev", "site": "us-west-2", "namespace": "platform",
		},
	}
	assert.Equal(t, 0.5, th.getThreshold("platform", "api-server", "123"))
	th.entityKeys.Range(func(key any, value any) bool {
		assert.Equal(t, dto.AsString(), key.(string))
		assert.Equal(t, dto, value.(EntityKeyDto))
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
		thresholds: &sync.Map{},
		entityKeys: &sync.Map{},
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
		thresholds: &sync.Map{},
		entityKeys: &sync.Map{},
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
		thresholds: &sync.Map{},
		entityKeys: &sync.Map{},
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
	var th = thresholdHelper{
		logger:              logger,
		config:              config,
		thresholds:          &sync.Map{},
		entityKeys:          &sync.Map{},
		stop:                make(chan bool),
		thresholdSyncTicker: clock.FromContext(ctx).NewTicker(time.Millisecond),
		rc: &assertsClient{
			config: config,
			logger: logger,
		},
	}
	entityKey := EntityKeyDto{
		Type: "Service", Name: "api-server", Scope: map[string]string{
			"env": "dev", "site": "us-west-2",
		},
	}
	th.entityKeys.Store(entityKey.AsString(), entityKey)
	go func() { th.startUpdates() }()
	time.Sleep(2 * time.Millisecond)
	th.stopUpdates()
	time.Sleep(1 * time.Millisecond)
}

func TestThresholdsIsUpdated(t *testing.T) {
	prevConfig := &Config{
		DefaultLatencyThreshold: 0.5,
	}
	currentConfig := &Config{
		DefaultLatencyThreshold: 0.51,
	}

	logger, _ := zap.NewProduction()
	var th = thresholdHelper{
		logger:  logger,
		rwMutex: &sync.RWMutex{},
	}

	assert.False(t, th.isUpdated(prevConfig, prevConfig))
	assert.True(t, th.isUpdated(prevConfig, currentConfig))
}

func TestThresholdsOnUpdate(t *testing.T) {
	prevConfig := &Config{
		DefaultLatencyThreshold: 0.5,
	}
	currentConfig := &Config{
		DefaultLatencyThreshold: 0.51,
	}

	logger, _ := zap.NewProduction()
	var th = thresholdHelper{
		logger:  logger,
		config:  prevConfig,
		rwMutex: &sync.RWMutex{},
	}

	assert.Equal(t, .5, th.getDefaultThreshold())
	err := th.onUpdate(currentConfig)
	assert.Nil(t, err)
	assert.Equal(t, .51, th.getDefaultThreshold())
}
