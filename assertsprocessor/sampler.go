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

type sampler struct {
	logger              *zap.Logger
	config              *Config
	thresholdHelper     thresholdHelper
	topTraces           *cmap.ConcurrentMap[string, cmap.ConcurrentMap[string, *TracePQ]]
	traceFlushTicker    *clock.Ticker
	nextConsumer        consumer.Traces
	stop                chan bool
	maxTracesPerService int
	requestRegexps      *map[string]regexp.Regexp
	mutex               sync.Mutex
}

func (s *sampler) sampleTrace(namespace string, serviceName string, ctx context.Context, trace ptrace.Traces, rootSpan ptrace.Span) {
	if s.latencyIsHigh(namespace, serviceName, rootSpan) || traceHasError(ctx, trace) {
		entityKey := EntityKeyDto{
			EntityType: "Service", Name: serviceName, Scope: map[string]string{
				"env": s.config.Env, "site": s.config.Site, "namespace": namespace,
			},
		}
		// Get the priority queue for the entity and request
		pq := s.getPriorityQueue(entityKey, rootSpan)
		(*pq).Push(OrderedTrace{
			trace:   &trace,
			ctx:     &ctx,
			latency: computeLatency(rootSpan),
		})
	}
}

func (s *sampler) getPriorityQueue(entityKey EntityKeyDto, rootSpan ptrace.Span) *TracePQ {
	var pq *TracePQ

	// Although topTraces is a thread-safe implementation, what we need here
	// is a compareAndSet which is available in sync.Map. We can try switching later
	s.mutex.Lock()
	defer s.mutex.Unlock()
	_map, found := s.topTraces.Get(entityKey.AsString())
	if !found {
		_map = cmap.New[*TracePQ]()
		s.topTraces.Set(entityKey.AsString(), _map)
	}
	request := getExp(s.requestRegexps, rootSpan)
	if request != "" {
		pq, _ = _map.Get(request)
		if pq == nil {
			queue := NewTracePQ(int(math.Min(5, float64(s.maxTracesPerService))))
			pq = &queue
		}
	}
	return pq
}

func (s *sampler) latencyIsHigh(namespace string, serviceName string, rootSpan ptrace.Span) bool {
	if rootSpan.ParentSpanID().IsEmpty() {
		spanDuration := computeLatency(rootSpan)

		var entityKey = EntityKeyDto{
			EntityType: "Service",
			Name:       serviceName,
			Scope: map[string]string{
				"asserts_env": s.config.Env, "asserts_site": s.config.Site, "namespace": namespace,
			},
		}

		s.logger.Info("Sampling check based on Root Span Duration",
			zap.String("Trace Id", rootSpan.TraceID().String()),
			zap.String("Entity Key", entityKey.AsString()),
			zap.Float64("Duration", spanDuration))

		threshold := s.thresholdHelper.getThreshold(namespace, serviceName, rootSpan.Name())
		return spanDuration > threshold
	}
	return false
}

func (s *sampler) flushTraces() {
	for {
		select {
		case <-s.stop:
			return
		case <-s.traceFlushTicker.C:
			s.logger.Info("Gathering Traces for flush")
			var previousTraces = s.topTraces
			c := cmap.New[cmap.ConcurrentMap[string, *TracePQ]]()
			s.topTraces = &c
			for _, cMap := range (*previousTraces).Items() {
				queues := make([]*TracePQ, len(cMap.Keys()))
				for i, key := range cMap.Keys() {
					queues[i], _ = cMap.Get(key)
				}

				// Round-robin through all the contexts and fill up the queue
				finalQ := NewTracePQ(s.config.MaxTracesPerMinute)
				for finalQ.Len() < s.config.MaxTracesPerMinute {
					for _, q := range queues {
						if q.Len() > 0 && finalQ.Len() < s.config.MaxTracesPerMinute {
							finalQ.pushUnsafe(q.popUnsafe())
						}
					}
				}

				// Drain the queue
				s.logger.Info("Flushing Traces", zap.Int("Num Traces", finalQ.Len()))
				for _, tp := range finalQ.traces {
					_ = s.nextConsumer.ConsumeTraces(*tp.ctx, *tp.trace)
				}
			}
		}
	}
}
