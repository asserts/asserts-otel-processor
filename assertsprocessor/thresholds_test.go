package assertsprocessor

import (
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"testing"
)

func TestAsString(t *testing.T) {
	dto := EntityKeyDto{
		EntityType: "Service", Name: "api-server", Scope: map[string]string{
			"asserts_env": "dev", "asserts_site": "us-west-2", "namespace": "ride-service",
		},
	}
	assert.Equal(t, "{, asserts_env=dev, asserts_site=us-west-2, namespace=ride-service}/Service/api-server", dto.AsString())
}

func TestGetThresholdDefaultThreshold(t *testing.T) {
	logger, _ := zap.NewProduction()
	m := thresholdHelper{
		logger: logger,
		config: Config{
			Env:                     "dev",
			Site:                    "us-west-2",
			AssertsServer:           "http://localhost:8030",
			DefaultLatencyThreshold: 0.5,
		},
		thresholds: cmap.New[cmap.ConcurrentMap[string, ThresholdDto]](),
		entityKeys: cmap.New[EntityKeyDto](),
	}

	dto := EntityKeyDto{
		EntityType: "Service", Name: "api-server", Scope: map[string]string{
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
		config: Config{
			Env:                     "dev",
			Site:                    "us-west-2",
			AssertsServer:           "http://localhost:8030",
			DefaultLatencyThreshold: 0.5,
		},
		thresholds: cmap.New[cmap.ConcurrentMap[string, ThresholdDto]](),
		entityKeys: cmap.New[EntityKeyDto](),
	}

	dto := EntityKeyDto{
		EntityType: "Service", Name: "api-server", Scope: map[string]string{
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
