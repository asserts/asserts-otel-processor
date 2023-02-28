package assertsprocessor

import (
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"testing"
)

func TestComputeLatency(t *testing.T) {
	testSpan := ptrace.NewSpan()
	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(1e9 + 4e8)
	assert.Equal(t, 0.4, computeLatency(testSpan))
}
