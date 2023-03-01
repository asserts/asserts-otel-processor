package assertsprocessor

import (
	"context"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"testing"
)

// This example creates a PriorityQueue with some items, adds and manipulates an item,
// and then removes the items in latency order.
func TestPriorityQueue(t *testing.T) {
	queueWrapper := NewTraceQueue(3)

	ctx1 := context.Background()
	trace1 := ptrace.NewTraces()
	queueWrapper.push(&Item{
		trace: &trace1, ctx: &ctx1, latency: 0.3,
	})
	assert.Equal(t, 1, len(queueWrapper.priorityQueue))
	assert.Equal(t, &trace1, queueWrapper.priorityQueue[0].trace)
	assert.Equal(t, 0.3, queueWrapper.priorityQueue[0].latency)

	ctx2 := context.Background()
	trace2 := ptrace.NewTraces()
	queueWrapper.push(&Item{
		trace: &trace2, ctx: &ctx2, latency: 0.2,
	})
	assert.Equal(t, 2, len(queueWrapper.priorityQueue))
	assert.Equal(t, &trace2, queueWrapper.priorityQueue[0].trace)
	assert.Equal(t, 0.2, queueWrapper.priorityQueue[0].latency)

	ctx3 := context.Background()
	trace3 := ptrace.NewTraces()
	queueWrapper.push(&Item{
		trace: &trace3, ctx: &ctx3, latency: 0.25,
	})
	assert.Equal(t, 3, len(queueWrapper.priorityQueue))
	assert.Equal(t, &trace2, queueWrapper.priorityQueue[0].trace)
	assert.Equal(t, 0.2, queueWrapper.priorityQueue[0].latency)

	ctx4 := context.Background()
	trace4 := ptrace.NewTraces()
	queueWrapper.push(&Item{
		trace: &trace4, ctx: &ctx4, latency: 0.4,
	})
	assert.Equal(t, 3, len(queueWrapper.priorityQueue))
	assert.Equal(t, &trace3, queueWrapper.priorityQueue[0].trace)
	assert.Equal(t, 0.25, queueWrapper.priorityQueue[0].latency)

	// This should be rejected
	ctx5 := context.Background()
	trace5 := ptrace.NewTraces()
	queueWrapper.push(&Item{
		trace: &trace5, ctx: &ctx5, latency: 0.2,
	})
	assert.Equal(t, 3, len(queueWrapper.priorityQueue))
	assert.Equal(t, &trace3, queueWrapper.priorityQueue[0].trace)
	assert.Equal(t, 0.25, queueWrapper.priorityQueue[0].latency)

	var item = queueWrapper.pop()
	assert.Equal(t, 2, len(queueWrapper.priorityQueue))
	assert.Equal(t, trace3, *item.trace)
	assert.Equal(t, 0.25, item.latency)
	assert.Equal(t, &trace1, queueWrapper.priorityQueue[0].trace)
	assert.Equal(t, 0.3, queueWrapper.priorityQueue[0].latency)
}
