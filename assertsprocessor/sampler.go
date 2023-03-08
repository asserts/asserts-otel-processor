package assertsprocessor

import (
	"context"
	"github.com/tilinna/clock"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"math"
	"regexp"
	"sync"
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

type sampler struct {
	logger               *zap.Logger
	config               *Config
	thresholdHelper      *thresholdHelper
	topTracesMap         *sync.Map
	healthySamplingState *sync.Map
	traceFlushTicker     *clock.Ticker
	nextConsumer         consumer.Traces
	stop                 chan bool
	requestRegexps       *map[string]*regexp.Regexp
}

type traceSummary struct {
	hasError        bool
	isSlow          bool
	slowestRootSpan ptrace.Span
	requestKey      RequestKey
	latency         float64
}

func (s *sampler) startProcessing() {
	s.thresholdHelper.startUpdates()
	s.startTraceFlusher()
}

func (s *sampler) stopProcessing() {
	s.thresholdHelper.stopUpdates()
	s.stopTraceFlusher()
}

func (s *sampler) sampleTrace(ctx context.Context,
	trace ptrace.Traces, traceId string, spanSet *resourceSpanGroup) {
	summary := s.getSummary(traceId, spanSet)
	item := Item{
		trace:   &trace,
		ctx:     &ctx,
		latency: summary.latency,
	}
	if summary.hasError || summary.isSlow {
		// Get the trace queue for the entity and request
		pq := s.getTraceQueues(summary.requestKey)
		if summary.hasError {
			s.logger.Debug("Capturing error trace",
				zap.String("traceId", summary.slowestRootSpan.TraceID().String()),
				zap.Float64("latency", summary.latency))
			pq.errorQueue.push(&item)
		} else {
			s.logger.Debug("Capturing slow trace",
				zap.String("traceId", summary.slowestRootSpan.TraceID().String()),
				zap.Float64("latency", summary.latency))
			pq.slowQueue.push(&item)
		}
	} else if len(spanSet.rootSpans) > 0 && summary.requestKey.AsString() != "" {
		// Capture healthy samples based on configured sampling rate
		state, _ := s.healthySamplingState.LoadOrStore(summary.requestKey.AsString(), &periodicSamplingState{
			lastSampleTime: 0,
			rwMutex:        &sync.RWMutex{},
		})
		samplingState := state.(*periodicSamplingState)
		if samplingState.sample(s.config.NormalSamplingFrequencyMinutes) {
			s.logger.Debug("Capturing normal trace",
				zap.String("traceId", summary.slowestRootSpan.TraceID().String()),
				zap.Float64("latency", summary.latency))
			pq := s.getTraceQueues(summary.requestKey)

			// Push to the latency queue to prioritize the healthy sample too
			pq.slowQueue.push(&item)
		}
	}
}

func (s *sampler) getSummary(traceId string, spanSet *resourceSpanGroup) *traceSummary {
	summary := traceSummary{}
	maxLatency := float64(0)
	summary.hasError = summary.hasError || spanSet.hasError()
	entityKey := buildEntityKey(s.config, spanSet.namespace, spanSet.service)
	for _, rootSpan := range spanSet.rootSpans {
		request := getRequest(s.requestRegexps, rootSpan)
		summary.isSlow = summary.isSlow || s.isSlow(spanSet.namespace, spanSet.service, rootSpan, request)
		max := math.Max(maxLatency, computeLatency(rootSpan))
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
	s.logger.Debug("Trace summary",
		zap.String("traceId", traceId),
		zap.String("request", summary.requestKey.AsString()),
		zap.Bool("slow", summary.isSlow),
		zap.Bool("error", summary.hasError),
		zap.Float64("latency", summary.latency))
	return &summary
}

func (s *sampler) getTraceQueues(key RequestKey) *traceQueues {
	var pq, _ = s.topTracesMap.LoadOrStore(key.AsString(), &traceQueues{
		slowQueue:  NewTraceQueue(int(math.Min(5, float64(s.config.LimitPerRequestPerService)))),
		errorQueue: NewTraceQueue(int(math.Min(5, float64(s.config.LimitPerRequestPerService)))),
	})
	return pq.(*traceQueues)
}

func (s *sampler) isSlow(namespace string, serviceName string, rootSpan ptrace.Span, request string) bool {
	spanDuration := computeLatency(rootSpan)
	threshold := s.thresholdHelper.getThreshold(namespace, serviceName, request)
	s.logger.Debug("Slow check ",
		zap.String("traceId", rootSpan.TraceID().String()),
		zap.Float64("latency", spanDuration),
		zap.Float64("threshold", threshold))
	return spanDuration > threshold
}

func (s *sampler) stopTraceFlusher() {
	go func() { s.stop <- true }()
}

func (s *sampler) startTraceFlusher() {
	go func() {
		for {
			select {
			case <-s.stop:
				s.logger.Info("Trace flush background routine stopped")
				return
			case <-s.traceFlushTicker.C:
				var previousTraces = s.topTracesMap
				s.topTracesMap = &sync.Map{}
				previousTraces.Range(func(key any, value any) bool {
					var requestKey = key.(string)
					var q = value.(*traceQueues)

					// Flush all the errors
					if len(q.errorQueue.priorityQueue) > 0 {
						s.logger.Debug("Flushing Error Traces for",
							zap.String("Request", requestKey),
							zap.Int("Count", len(q.errorQueue.priorityQueue)))
						for _, item := range q.errorQueue.priorityQueue {
							_ = (*s).nextConsumer.ConsumeTraces(*item.ctx, *item.trace)
						}
					}

					// Flush all the slow traces
					if len(q.slowQueue.priorityQueue) > 0 {
						s.logger.Debug("Flushing Slow Traces for",
							zap.String("Request", requestKey),
							zap.Int("Count", len(q.slowQueue.priorityQueue)))
						for _, item := range q.slowQueue.priorityQueue {
							_ = (*s).nextConsumer.ConsumeTraces(*item.ctx, *item.trace)
						}
					}
					return true
				})
			}
		}
	}()
}
