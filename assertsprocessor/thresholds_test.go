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
	m := thresholdHelper{
		logger: logger,
		config: &Config{
			Env:                     "dev",
			Site:                    "us-west-2",
			AssertsServer:           "http://localhost:8030",
			DefaultLatencyThreshold: 0.5,
		},
		thresholds: &sync.Map{},
		entityKeys: &sync.Map{},
	}

	dto := EntityKeyDto{
		Type: "Service", Name: "api-server", Scope: map[string]string{
			"env": "dev", "site": "us-west-2", "namespace": "platform",
		},
	}
	assert.Equal(t, 0.5, m.getThreshold("platform", "api-server", "123"))
	m.entityKeys.Range(func(key any, value any) bool {
		assert.Equal(t, dto.AsString(), key.(string))
		assert.Equal(t, dto, value.(EntityKeyDto))
		return true
	})
}

func TestGetRequestThresholdFound(t *testing.T) {
	logger, _ := zap.NewProduction()
	var m = thresholdHelper{
		logger: logger,
		config: &Config{
			Env:                     "dev",
			Site:                    "us-west-2",
			AssertsServer:           "http://localhost:8030",
			DefaultLatencyThreshold: 0.5,
		},
		thresholds: &sync.Map{},
		entityKeys: &sync.Map{},
	}

	dto := EntityKeyDto{
		Type: "Service", Name: "api-server", Scope: map[string]string{
			"env": "dev", "site": "us-west-2", "namespace": "platform",
		},
	}
	m.entityKeys.Store(dto.AsString(), dto)

	byRequest := map[string]*ThresholdDto{}
	m.thresholds.Store(dto.AsString(), byRequest)

	byRequest["/v1/latency-thresholds"] = &ThresholdDto{
		ResourceURIPattern: "/v1/latency-thresholds",
		LatencyUpperBound:  1,
	}

	byRequest[""] = &ThresholdDto{
		ResourceURIPattern: "",
		LatencyUpperBound:  2,
	}

	assert.Equal(t, float64(1), m.getThreshold("platform", "api-server", "/v1/latency-thresholds"))
}

func TestGetServiceDefaultThresholdFound(t *testing.T) {
	logger, _ := zap.NewProduction()
	var m = thresholdHelper{
		logger: logger,
		config: &Config{
			Env:                     "dev",
			Site:                    "us-west-2",
			AssertsServer:           "http://localhost:8030",
			DefaultLatencyThreshold: 0.5,
		},
		thresholds: &sync.Map{},
		entityKeys: &sync.Map{},
	}

	dto := EntityKeyDto{
		Type: "Service", Name: "api-server", Scope: map[string]string{
			"env": "dev", "site": "us-west-2", "namespace": "platform",
		},
	}
	m.entityKeys.Store(dto.AsString(), dto)

	byRequest := map[string]*ThresholdDto{}
	m.thresholds.Store(dto.AsString(), byRequest)

	byRequest[""] = &ThresholdDto{
		ResourceURIPattern: "",
		LatencyUpperBound:  1,
	}

	assert.Equal(t, float64(1), m.getThreshold("platform", "api-server", "/v1/latency-thresholds"))
}

func TestStopUpdates(t *testing.T) {
	logger, _ := zap.NewProduction()
	var m = thresholdHelper{
		logger: logger,
		config: &Config{
			Env:                     "dev",
			Site:                    "us-west-2",
			AssertsServer:           "http://localhost:8030",
			DefaultLatencyThreshold: 0.5,
		},
		thresholds: &sync.Map{},
		entityKeys: &sync.Map{},
		stop:       make(chan bool),
	}
	m.stopUpdates()
	assert.True(t, <-m.stop)
}

func TestUpdateThresholds(t *testing.T) {
	logger, _ := zap.NewProduction()
	ctx := context.Background()
	var m = thresholdHelper{
		logger: logger,
		config: &Config{
			Env:                     "dev",
			Site:                    "us-west-2",
			AssertsServer:           "http://localhost:8030",
			DefaultLatencyThreshold: 0.5,
		},
		thresholds:          &sync.Map{},
		entityKeys:          &sync.Map{},
		stop:                make(chan bool),
		thresholdSyncTicker: clock.FromContext(ctx).NewTicker(time.Second),
	}
	entityKey := EntityKeyDto{
		Type: "Service", Name: "api-server", Scope: map[string]string{
			"env": "dev", "site": "us-west-2",
		},
	}
	m.entityKeys.Store(entityKey.AsString(), entityKey)
	go func() { m.updateThresholds() }()
	time.Sleep(2 * time.Second)
	m.stopUpdates()
	time.Sleep(1 * time.Second)
}
