package assertsprocessor

import (
	"container/heap"
	"context"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"sync"
)

// An Item is something we manage in a latency queue.
type Item struct {
	trace   *ptrace.Traces // The value of the item; arbitrary.
	ctx     *context.Context
	latency float64 // The latency of the item in the queue.
	// The index is needed by update and is maintained by the heap.Interface methods.
	index int // The index of the item in the heap.
}

// A PriorityQueue implements heap.Interface and holds Items.
type PriorityQueue []*Item

type TraceQueue struct {
	priorityQueue PriorityQueue
	maxSize       int
	mutex         sync.Mutex
}

func NewTraceQueue(maxSize int) *TraceQueue {
	traceQueue := TraceQueue{
		priorityQueue: make(PriorityQueue, 0),
		maxSize:       maxSize,
	}
	return &traceQueue
}

func (tq *TraceQueue) push(item *Item) {
	tq.mutex.Lock()
	defer tq.mutex.Unlock()
	tq.pushUnsafe(item)
}

func (tq *TraceQueue) pushUnsafe(item *Item) {
	// If limit reached, compare new item with
	// existing item to see if it qualifies to be in the heap
	if len(tq.priorityQueue) == tq.maxSize {
		// Need to pop to compare
		pop := heap.Pop(&tq.priorityQueue)
		if pop.(*Item).latency > item.latency {
			// If new item is lower priority, put the popped item back
			// and return
			heap.Push(&tq.priorityQueue, pop)
			return
		}
	}
	heap.Push(&tq.priorityQueue, item)
}

func (tq *TraceQueue) pop() *Item {
	tq.mutex.Lock()
	defer tq.mutex.Unlock()
	pop := heap.Pop(&tq.priorityQueue)

	return pop.(*Item)
}

func (pq PriorityQueue) Len() int { return len(pq) }

func (pq PriorityQueue) Less(i, j int) bool {
	// We want Pop to give us the highest, not lowest, latency, so we use less than here.
	return pq[i].latency < pq[j].latency
}

func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *PriorityQueue) Push(x any) {
	n := len(*pq)
	item := x.(*Item)
	item.index = n
	*pq = append(*pq, item)
}

func (pq *PriorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // avoid memory leak
	item.index = -1 // for safety
	*pq = old[0 : n-1]
	return item
}

// update modifies the latency and value of an Item in the queue.
func (pq *PriorityQueue) update(item *Item, trace *ptrace.Traces, priority int) {
	// Empty implementation to comply with interface requirements
	// We don't need this. The priority type being `int` is another challenge.
}
