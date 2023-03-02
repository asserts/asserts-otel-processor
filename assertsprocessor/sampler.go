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
	sampleNow := currentTime-state.lastSampleTime > int64(time.Duration(everyNMinutes)*time.Minute)
	if sampleNow {
		state.rwMutex.RUnlock()
		state.rwMutex.Lock()
		if currentTime-state.lastSampleTime > int64(time.Duration(everyNMinutes)*time.Minute) {
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

type traceSummary struct {
	hasError        bool
	isSlow          bool
	slowestRootSpan *ptrace.Span
	requestKey      RequestKey
	latency         float64
}

func (s *sampler) sampleTrace(ctx context.Context,
	trace ptrace.Traces, spanSets []*resourceSpanGroup) {
	summary := s.getSummary(spanSets)
	item := Item{
		trace:   &trace,
		ctx:     &ctx,
		latency: summary.latency,
	}
	if summary.hasError || summary.isSlow {
		// Get the trace queue for the entity and request
		pq := s.getTraceQueues(summary.requestKey)
		if summary.hasError {
			s.logger.Info("Capturing error trace",
				zap.String("traceId", summary.slowestRootSpan.TraceID().String()),
				zap.Float64("latency", computeLatency(*summary.slowestRootSpan)))
			pq.errorQueue.push(&item)
		} else {
			s.logger.Info("Capturing slow trace",
				zap.String("traceId", summary.slowestRootSpan.TraceID().String()),
				zap.Float64("latency", computeLatency(*summary.slowestRootSpan)))
			pq.slowQueue.push(&item)
		}
	} else {
		// Capture healthy samples based on configured sampling rate
		state, _ := s.healthySamplingState.LoadOrStore(summary.requestKey.AsString(), &periodicSamplingState{
			lastSampleTime: 0,
			rwMutex:        &sync.RWMutex{},
		})
		samplingState := state.(*periodicSamplingState)
		if samplingState.sample(s.config.NormalSamplingFrequencyMinutes) {
			s.logger.Info("Capturing normal trace",
				zap.String("traceId", summary.slowestRootSpan.TraceID().String()),
				zap.Float64("latency", summary.latency))
			pq := s.getTraceQueues(summary.requestKey)

			// Push to the latency queue to prioritize the healthy sample too
			pq.slowQueue.push(&item)
		}
	}
}

func (s *sampler) getSummary(spanSets []*resourceSpanGroup) *traceSummary {
	summary := traceSummary{}
	summary.hasError = false
	summary.isSlow = false
	maxLatency := float64(0)
	for _, spanSet := range spanSets {
		summary.hasError = summary.hasError || spanSet.hasError()
		entityKey := buildEntityKey(s.config, spanSet.namespace, spanSet.service)
		for _, rootSpan := range spanSet.rootSpans {
			request := getRequest(s.requestRegexps, *rootSpan)
			summary.isSlow = summary.isSlow || s.isSlow(spanSet.namespace, spanSet.service, *rootSpan)
			max := math.Max(maxLatency, computeLatency(*rootSpan))
			if max > maxLatency {
				maxLatency = max
				summary.slowestRootSpan = rootSpan
				summary.requestKey = RequestKey{
					entityKey: entityKey,
					request:   request,
				}
				summary.latency = maxLatency
			}
		}
	}
	return &summary
}

func (s *sampler) getTraceQueues(key RequestKey) *traceQueues {
	var pq, _ = s.topTracesMap.LoadOrStore(key.AsString(), &traceQueues{
		slowQueue:  NewTraceQueue(int(math.Min(5, float64(s.config.MaxTracesPerMinutePerContainer)))),
		errorQueue: NewTraceQueue(int(math.Min(5, float64(s.config.MaxTracesPerMinutePerContainer)))),
	})
	return pq.(*traceQueues)
}

func (s *sampler) isSlow(namespace string, serviceName string, rootSpan ptrace.Span) bool {
	spanDuration := computeLatency(rootSpan)
	threshold := s.thresholdHelper.getThreshold(namespace, serviceName, rootSpan.Name())
	return spanDuration > threshold
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
				if len(q.errorQueue.priorityQueue) > 0 {
					s.logger.Info("Flushing Error Traces for",
						zap.String("Request", requestKey),
						zap.Int("Count", len(q.errorQueue.priorityQueue)))
					for _, item := range q.errorQueue.priorityQueue {
						_ = s.nextConsumer.ConsumeTraces(*item.ctx, *item.trace)
					}
				}

				if len(q.slowQueue.priorityQueue) > 0 {
					serviceQueue := serviceQueues[entityKey]
					if serviceQueue == nil {
						serviceQueue = NewTraceQueue(s.config.MaxTracesPerMinute)
						serviceQueues[entityKey] = serviceQueue
					}
					for _, item := range q.slowQueue.priorityQueue {
						serviceQueue.push(item)
					}
				}
				return true
			})

			// Drain the latency queue
			for entityKey, q := range serviceQueues {
				s.logger.Info("Flushing Slow Traces",
					zap.String("Service", entityKey),
					zap.Int("Count", len(q.priorityQueue)))
				for _, tp := range q.priorityQueue {
					_ = s.nextConsumer.ConsumeTraces(*tp.ctx, *tp.trace)
				}
			}
		}
	}
}
