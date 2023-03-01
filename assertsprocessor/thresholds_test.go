package assertsprocessor

import (
	"context"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/stretchr/testify/assert"
	"github.com/tilinna/clock"
	"go.uber.org/zap"
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
		thresholds: cmap.New[cmap.ConcurrentMap[string, ThresholdDto]](),
		entityKeys: cmap.New[EntityKeyDto](),
	}

	dto := EntityKeyDto{
		Type: "Service", Name: "api-server", Scope: map[string]string{
			"env": "dev", "site": "us-west-2", "namespace": "platform",
		},
	}
	assert.Equal(t, 0.5, m.getThreshold("platform", "api-server", "123"))
	load, ok := m.entityKeys.Get(dto.AsString())
	assert.True(t, ok)
	assert.Equal(t, dto, load)
}

func TestGetThresholdFound(t *testing.T) {
	logger, _ := zap.NewProduction()
	var m = thresholdHelper{
		logger: logger,
		config: &Config{
			Env:                     "dev",
			Site:                    "us-west-2",
			AssertsServer:           "http://localhost:8030",
			DefaultLatencyThreshold: 0.5,
		},
		thresholds: cmap.New[cmap.ConcurrentMap[string, ThresholdDto]](),
		entityKeys: cmap.New[EntityKeyDto](),
	}

	dto := EntityKeyDto{
		Type: "Service", Name: "api-server", Scope: map[string]string{
			"env": "dev", "site": "us-west-2", "namespace": "platform",
		},
	}
	m.entityKeys.Set(dto.AsString(), dto)

	byRequest := cmap.New[ThresholdDto]()
	m.thresholds.Set(dto.AsString(), byRequest)

	byRequest.Set("/v1/latency-thresholds", ThresholdDto{
		ResourceURIPattern: "/v1/latency-thresholds",
		LatencyUpperBound:  1,
	})

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
		thresholds: cmap.New[cmap.ConcurrentMap[string, ThresholdDto]](),
		entityKeys: cmap.New[EntityKeyDto](),
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
		thresholds:          cmap.New[cmap.ConcurrentMap[string, ThresholdDto]](),
		entityKeys:          cmap.New[EntityKeyDto](),
		stop:                make(chan bool),
		thresholdSyncTicker: clock.FromContext(ctx).NewTicker(time.Second * 5),
	}
	entityKey := EntityKeyDto{
		Type: "Service", Name: "api-server", Scope: map[string]string{
			"env": "dev", "site": "us-west-2",
		},
	}
	m.entityKeys.Set(entityKey.AsString(), entityKey)
	go func() { m.updateThresholds() }()
	time.Sleep(10 * time.Second)
	m.stopUpdates()
	time.Sleep(2 * time.Second)
}
