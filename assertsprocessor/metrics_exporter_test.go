package assertsprocessor

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"testing"
)

func TestStopExporter(t *testing.T) {
	logger, _ := zap.NewProduction()
	exp := &metricsExporter{
		logger: logger,
		config: &testConfig,
	}

	exp.start(prometheus.NewRegistry())
	assert.Nil(t, exp.stop())
}
