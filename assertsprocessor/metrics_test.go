package assertsprocessor

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"testing"
)

func TestRegisterMetrics(t *testing.T) {
	logger, _ := zap.NewProduction()
	reg := &metrics{
		logger: logger,
		config: &Config{
			LatencyHistogramBuckets: []float64{1, 2.5, 5, 10},
		},
		prometheusRegistry: prometheus.NewRegistry(),
	}
	attributes := []string{"rpc.system", "rpc.service", "rpc.method",
		"aws.table.name", "aws.queue.url", "host.name"}

	assert.Nil(t, reg.registerMetrics(attributes))
	assert.NotNil(t, reg.prometheusRegistry)
	assert.NotNil(t, reg.latencyHistogram)
	assert.NotNil(t, reg.sampledTraceCount)
	assert.NotNil(t, reg.totalTraceCount)
}

func TestUnregisterMetrics(t *testing.T) {
	logger, _ := zap.NewProduction()
	reg := &metrics{
		logger:             logger,
		config:             &Config{},
		prometheusRegistry: prometheus.NewRegistry(),
	}
	attributes := []string{"rpc.system", "rpc.service", "rpc.method",
		"aws.table.name", "aws.queue.url", "host.name"}

	_ = reg.registerMetrics(attributes)
	reg.unregisterMetrics()
}
