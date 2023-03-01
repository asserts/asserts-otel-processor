package assertsprocessor

import (
	"context"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/tilinna/clock"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"math"
	"regexp"
	"sync"
)

type traceQueues struct {
	latencyQueue *TraceQueue
	errorQueue   *TraceQueue
}

func (tQ *traceQueues) errorTraceCount() int {
	return len(tQ.errorQueue.priorityQueue)
}

func (tQ *traceQueues) latencyTraceCount() int {
	return len(tQ.latencyQueue.priorityQueue)
}

type sampler struct {
	logger           *zap.Logger
	config           *Config
	thresholdHelper  *thresholdHelper
	topTraces        *cmap.ConcurrentMap[string, cmap.ConcurrentMap[string, *traceQueues]]
	traceFlushTicker *clock.Ticker
	nextConsumer     consumer.Traces
	stop             chan bool
	requestRegexps   *map[string]regexp.Regexp
	mutex            sync.Mutex
}

func (s *sampler) sampleTrace(namespace string, serviceName string, ctx context.Context,
	trace ptrace.Traces, rootSpan ptrace.Span) {
	traceHasError := hasError(ctx, trace)
	latencyIsHigh := s.latencyIsHigh(namespace, serviceName, rootSpan)
	if latencyIsHigh || traceHasError {
		entityKey := buildEntityKey(s.config, namespace, serviceName)
		// Get the latency queue for the entity and request
		pq := s.getTraceQueues(entityKey, rootSpan)
		if traceHasError {
			pq.errorQueue.push(&Item{
				trace:   &trace,
				ctx:     &ctx,
				latency: computeLatency(rootSpan),
			})
		} else {
			pq.latencyQueue.push(&Item{
				trace:   &trace,
				ctx:     &ctx,
				latency: computeLatency(rootSpan),
			})
		}
	}
}

func (s *sampler) getTraceQueues(entityKey EntityKeyDto, rootSpan ptrace.Span) *traceQueues {
	// Although topTraces is a thread-safe implementation, what we need here
	// is a compareAndSet which is available in sync.Map. We can try switching later
	_map := s.getEntityMap(entityKey)
	request := getExp(s.requestRegexps, rootSpan)
	pq, _ := _map.Get(request)
	if pq == nil {
		// For each request, we capture a max of 5 traces per minute
		pq = &traceQueues{
			latencyQueue: NewTraceQueue(int(math.Min(5, float64(s.config.MaxTracesPerMinutePerContainer)))),
			errorQueue:   NewTraceQueue(int(math.Min(5, float64(s.config.MaxTracesPerMinutePerContainer)))),
		}
		_map.Set(request, pq)
	}
	return pq
}

func (s *sampler) getEntityMap(entityKey EntityKeyDto) cmap.ConcurrentMap[string, *traceQueues] {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	_map, found := s.topTraces.Get(entityKey.AsString())
	if !found {
		_map = cmap.New[*traceQueues]()
		s.topTraces.Set(entityKey.AsString(), _map)
	}
	return _map
}

func (s *sampler) latencyIsHigh(namespace string, serviceName string, rootSpan ptrace.Span) bool {
	if rootSpan.ParentSpanID().IsEmpty() {
		spanDuration := computeLatency(rootSpan)

		var entityKey = buildEntityKey(s.config, namespace, serviceName)

		s.logger.Info("Sampling check based on Root Span Duration",
			zap.String("Trace Id", rootSpan.TraceID().String()),
			zap.String("Entity Key", entityKey.AsString()),
			zap.Float64("Duration", spanDuration))

		threshold := s.thresholdHelper.getThreshold(namespace, serviceName, rootSpan.Name())
		return spanDuration > threshold
	}
	return false
}

func (s *sampler) stopFlushing() {
	func() { s.stop <- true }()
}

func (s *sampler) flushTraces() {
	for {
		select {
		case <-s.stop:
			s.logger.Info("Trace flush background routine stopped")
			return
		case <-s.traceFlushTicker.C:
			s.logger.Info("Gathering Traces for flush")
			var previousTraces = s.topTraces
			c := cmap.New[cmap.ConcurrentMap[string, *traceQueues]]()
			s.topTraces = &c
			for _, cMap := range (*previousTraces).Items() {
				for _, q := range cMap.Items() {
					// Flush all the errors
					for _, tp := range q.errorQueue.priorityQueue {
						_ = s.nextConsumer.ConsumeTraces(*tp.ctx, *tp.trace)
					}
				}

				finalQ := NewTraceQueue(s.config.MaxTracesPerMinute)
				for _, q := range cMap.Items() {
					// For latency, enforce an overall rate limit across contexts
					var length = len(q.latencyQueue.priorityQueue)
					for i := 0; i < length; i++ {
						finalQ.pushUnsafe((q.latencyQueue.priorityQueue)[i])
					}
				}

				// Drain the latency queue
				s.logger.Info("Flushing High Latency Traces", zap.Int("Num Traces", len(finalQ.priorityQueue)))
				for _, tp := range finalQ.priorityQueue {
					_ = s.nextConsumer.ConsumeTraces(*tp.ctx, *tp.trace)
				}
			}
		}
	}
}
