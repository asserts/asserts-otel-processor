package assertsprocessor

import (
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"testing"
)

func TestShouldSampleTrue(t *testing.T) {
	logger, _ := zap.NewProduction()

	config := Config{
		Env:                     "dev",
		Site:                    "us-west-2",
		AssertsServer:           "http://localhost:8030",
		DefaultLatencyThreshold: 0.5,
	}

	entityKeys := cmap.New[EntityKeyDto]()
	c := cmap.New[cmap.ConcurrentMap[string, ThresholdDto]]()
	var th = thresholdHelper{
		logger:     logger,
		config:     config,
		entityKeys: entityKeys,
		thresholds: c,
	}

	var s = sampler{
		logger:          logger,
		config:          config,
		thresholdHelper: th,
	}

	testSpan := ptrace.NewSpan()
	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(1e9 + 6e8)

	assert.True(t, s.shouldCaptureTrace("platform", "api-server", "trace-id", testSpan))
}

func TestShouldSampleFalse(t *testing.T) {
	logger, _ := zap.NewProduction()

	config := Config{
		Env:                     "dev",
		Site:                    "us-west-2",
		AssertsServer:           "http://localhost:8030",
		DefaultLatencyThreshold: 0.5,
	}

	entityKeys := cmap.New[EntityKeyDto]()
	c := cmap.New[cmap.ConcurrentMap[string, ThresholdDto]]()
	var th = thresholdHelper{
		logger:     logger,
		config:     config,
		entityKeys: entityKeys,
		thresholds: c,
	}

	var s = sampler{
		logger:          logger,
		config:          config,
		thresholdHelper: th,
	}

	testSpan := ptrace.NewSpan()
	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(1e9 + 4e8)

	assert.False(t, s.shouldCaptureTrace("platform", "api-server", "trace-id", testSpan))
}
