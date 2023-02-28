package assertsprocessor

import (
	"context"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"sync"
)

type OrderedTrace struct {
	trace   *ptrace.Traces
	ctx     *context.Context
	latency float64
	index   int
}

// TracePQ implements heap.Interface and holds Items. Inspired by
// https://cs.opensource.google/go/go/+/master:src/container/heap/
type TracePQ struct {
	traces  []*OrderedTrace
	rwLock  sync.RWMutex
	maxSize int
}

func NewTracePQ(maxSize int) TracePQ {
	return TracePQ{
		traces:  []*OrderedTrace{},
		maxSize: maxSize,
	}
}

func (pq *TracePQ) Len() int {
	pq.rwLock.RLock()
	defer pq.rwLock.RUnlock()
	return len(pq.traces)
}

func (pq *TracePQ) Less(i, j int) bool {
	pq.rwLock.RLock()
	defer pq.rwLock.RUnlock()
	// We want Pop to give us the lowest, not the highest latency, so we use less than here.
	return pq.traces[i].latency < pq.traces[j].latency
}

func (pq *TracePQ) Swap(i, j int) {
	pq.rwLock.Lock()
	defer pq.rwLock.Unlock()
	pq.traces[i], pq.traces[j] = pq.traces[j], pq.traces[i]
	pq.traces[i].index = i
	pq.traces[j].index = j
}

func (pq *TracePQ) Push(x OrderedTrace) {
	pq.rwLock.Lock()
	defer pq.rwLock.Unlock()
	pq.pushUnsafe(x)
	if len(pq.traces) > pq.maxSize {
		pq.popUnsafe()
	}
}

func (pq *TracePQ) pushUnsafe(x OrderedTrace) {
	n := len((*pq).traces)
	x.index = n
	(*pq).traces = append((*pq).traces, &x)
}

func (pq *TracePQ) Pop() OrderedTrace {
	pq.rwLock.Lock()
	defer pq.rwLock.Unlock()
	return pq.popUnsafe()
}

func (pq *TracePQ) popUnsafe() OrderedTrace {
	old := (*pq).traces
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // avoid memory leak
	item.index = -1 // for safety
	(*pq).traces = old[0 : n-1]
	return *item
}

// update modifies the priority and value of an Item in the queue.
func (pq *TracePQ) update(item *OrderedTrace, value *ptrace.Traces, latency float64) {
	// Left empty as it's not needed
}
