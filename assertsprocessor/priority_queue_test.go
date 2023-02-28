package assertsprocessor

import (
	"context"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"testing"
)

func TestNewPriorityQueue(t *testing.T) {
	pq := NewTracePQ(3)
	assert.Equal(t, 3, pq.maxSize)
	assert.NotNil(t, pq.traces)
}

func TestPushWithOverflow(t *testing.T) {
	pq := NewTracePQ(3)
	ctx := context.Background()

	trace1 := ptrace.NewTraces()
	pq.Push(OrderedTrace{
		trace:   &trace1,
		ctx:     &ctx,
		latency: 1,
	})
	assert.Equal(t, 1, pq.Len())
	assert.Equal(t, &trace1, pq.traces[0].trace)

	trace2 := ptrace.NewTraces()
	pq.Push(OrderedTrace{
		trace:   &trace2,
		ctx:     &ctx,
		latency: 2,
	})
	assert.Equal(t, 2, pq.Len())
	assert.Equal(t, &trace1, pq.traces[0].trace)
	assert.Equal(t, &trace2, pq.traces[1].trace)

	trace3 := ptrace.NewTraces()
	pq.Push(OrderedTrace{
		trace:   &trace3,
		ctx:     &ctx,
		latency: 3,
	})
	assert.Equal(t, 3, pq.Len())
	assert.Equal(t, &trace1, pq.traces[0].trace)
	assert.Equal(t, &trace2, pq.traces[1].trace)
	assert.Equal(t, &trace3, pq.traces[2].trace)

	trace4 := ptrace.NewTraces()
	pq.Push(OrderedTrace{
		trace:   &trace4,
		ctx:     &ctx,
		latency: 4,
	})
	assert.Equal(t, 3, pq.Len())
	assert.Equal(t, &trace2, pq.traces[0].trace)
	assert.Equal(t, &trace3, pq.traces[1].trace)
	assert.Equal(t, &trace4, pq.traces[2].trace)
}
