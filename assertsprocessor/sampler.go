package assertsprocessor

import (
	"context"
	"github.com/tilinna/clock"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"
)

type traceQueues struct {
	slowQueue  *TraceQueue
	errorQueue *TraceQueue
}

func (tQ *traceQueues) errorTraceCount() int {
	return len(tQ.errorQueue.priorityQueue)
}

func (tQ *traceQueues) slowTraceCount() int {
	return len(tQ.slowQueue.priorityQueue)
}

type periodicSamplingState struct {
	lastSampleTime int64
	rwMutex        *sync.RWMutex
}

func (state *periodicSamplingState) sample(everyNMinutes int) bool {
	state.rwMutex.RLock()
	currentTime := time.Now().Unix()
	sampleNow := currentTime-state.lastSampleTime > int64(everyNMinutes*3600)
	if sampleNow {
		state.rwMutex.RUnlock()
		state.rwMutex.Lock()
		if currentTime-state.lastSampleTime > int64(everyNMinutes*3600) {
			state.lastSampleTime = currentTime
		}
		state.rwMutex.Unlock()
		return true
	}
	return false
}

type sampler struct {
	logger               *zap.Logger
	config               *Config
	thresholdHelper      *thresholdHelper
	topTracesMap         *sync.Map
	healthySamplingState *sync.Map
	traceFlushTicker     *clock.Ticker
	nextConsumer         consumer.Traces
	stop                 chan bool
	requestRegexps       *map[string]regexp.Regexp
}

func (s *sampler) sampleTrace(namespace string, serviceName string, ctx context.Context,
	trace ptrace.Traces, rootSpan ptrace.Span) {
	entityKey := buildEntityKey(s.config, namespace, serviceName)
	request := getRequest(s.requestRegexps, rootSpan)
	key := RequestKey{
		entityKey: entityKey,
		request:   request,
	}
	traceHasError := hasError(ctx, trace)
	latencyIsHigh := s.latencyIsHigh(namespace, serviceName, rootSpan)
	latency := computeLatency(rootSpan)
	if traceHasError || latencyIsHigh {
		// Get the latency queue for the entity and request
		pq := s.getTraceQueues(key)
		if traceHasError {
			s.logger.Info("Capturing error trace",
				zap.String("traceId", rootSpan.TraceID().String()),
				zap.Float64("latency", latency))
			pq.errorQueue.push(&Item{
				trace:   &trace,
				ctx:     &ctx,
				latency: latency,
			})
		} else {
			s.logger.Info("Capturing slow trace",
				zap.String("traceId", rootSpan.TraceID().String()),
				zap.Float64("latency", latency))
			pq.slowQueue.push(&Item{
				trace:   &trace,
				ctx:     &ctx,
				latency: latency,
			})
		}
	} else {
		// Capture healthy samples based on configured sampling rate
		state, _ := s.healthySamplingState.LoadOrStore(key.AsString(), &periodicSamplingState{
			lastSampleTime: 0,
			rwMutex:        &sync.RWMutex{},
		})
		samplingState := state.(*periodicSamplingState)
		if samplingState.sample(s.config.NormalSamplingFrequencyMinutes) {
			s.logger.Info("Capturing normal trace",
				zap.String("traceId", rootSpan.TraceID().String()),
				zap.Float64("latency", latency))
			pq := s.getTraceQueues(key)

			// Push to the latency queue to prioritize the healthy sample too
			pq.slowQueue.push(&Item{
				trace:   &trace,
				ctx:     &ctx,
				latency: latency,
			})
		}
	}
}

func (s *sampler) getTraceQueues(key RequestKey) *traceQueues {
	var pq, _ = s.topTracesMap.LoadOrStore(key.AsString(), &traceQueues{
		slowQueue:  NewTraceQueue(int(math.Min(5, float64(s.config.MaxTracesPerMinutePerContainer)))),
		errorQueue: NewTraceQueue(int(math.Min(5, float64(s.config.MaxTracesPerMinutePerContainer)))),
	})
	return pq.(*traceQueues)
}

func (s *sampler) latencyIsHigh(namespace string, serviceName string, rootSpan ptrace.Span) bool {
	if rootSpan.ParentSpanID().IsEmpty() {
		spanDuration := computeLatency(rootSpan)
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
			var previousTraces = s.topTracesMap
			s.topTracesMap = &sync.Map{}
			var serviceQueues = map[string]*TraceQueue{}
			previousTraces.Range(func(key any, value any) bool {
				var requestKey = key.(string)
				var scopeString, part1, _ = strings.Cut(requestKey, "#")
				var typeName, part2, _ = strings.Cut(part1, "#")
				var name, _, _ = strings.Cut(part2, "#")
				var entityKey = scopeString + "#" + typeName + "#" + name

				var q = value.(*traceQueues)

				// Flush all the errors
				s.logger.Info("Flushing Error Traces for",
					zap.String("Request", requestKey),
					zap.Int("Num Traces", len(q.errorQueue.priorityQueue)))
				for _, item := range q.errorQueue.priorityQueue {
					_ = s.nextConsumer.ConsumeTraces(*item.ctx, *item.trace)
				}

				serviceQueue := serviceQueues[entityKey]
				if serviceQueue == nil {
					serviceQueue = NewTraceQueue(s.config.MaxTracesPerMinute)
					serviceQueues[entityKey] = serviceQueue
				}
				for _, item := range q.slowQueue.priorityQueue {
					serviceQueue.push(item)
				}

				return true
			})

			// Drain the latency queue
			for entityKey, q := range serviceQueues {
				s.logger.Info("Flushing Slow Traces",
					zap.String("Service", entityKey),
					zap.Int("Num Traces", len(q.priorityQueue)))
				for _, tp := range q.priorityQueue {
					_ = s.nextConsumer.ConsumeTraces(*tp.ctx, *tp.trace)
				}
			}
		}
	}
}
