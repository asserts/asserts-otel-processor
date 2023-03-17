package assertsprocessor

import (
	"context"
	"math"
	"regexp"
	"sync"

	"github.com/tilinna/clock"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

const (
	AssertsRequestContextAttribute = "asserts.request.context"
)

type traceSampler struct {
	slowQueue  *TraceQueue
	errorQueue *TraceQueue
}

func (tS *traceSampler) errorTraceCount() int {
	return len(tS.errorQueue.priorityQueue)
}

func (tS *traceSampler) slowTraceCount() int {
	return len(tS.slowQueue.priorityQueue)
}

type sampler struct {
	logger             *zap.Logger
	config             *Config
	thresholdHelper    *thresholdHelper
	topTracesByService *sync.Map
	traceFlushTicker   *clock.Ticker
	nextConsumer       consumer.Traces
	stop               chan bool
	requestRegexps     *map[string]*regexp.Regexp
}

type traceSummary struct {
	hasError        bool
	isSlow          bool
	slowestRootSpan *ptrace.Span
	requestKey      *RequestKey
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
	if summary == nil {
		return
	}
	item := Item{
		trace:   &trace,
		ctx:     &ctx,
		latency: summary.latency,
	}
	entityKey := summary.requestKey.entityKey.AsString()
	if summary.hasError || summary.isSlow {
		// Get the trace queue for the entity and request
		perService, _ := s.topTracesByService.LoadOrStore(entityKey, NewServiceQueues(s.config))
		requestState := perService.(*serviceQueues).getRequestState(summary.requestKey.request)

		// If there are too many requests, we may not get a queue due to constraints
		if requestState != nil {
			if summary.hasError {
				// For all the spans which have error, add the request context
				for _, span := range spanSet.rootSpans {
					if spanHasError(span) {
						span.Attributes().PutStr(AssertsRequestContextAttribute, getRequest(s.requestRegexps, span))
					}
				}

				for _, span := range spanSet.exitSpans {
					if spanHasError(span) {
						span.Attributes().PutStr(AssertsRequestContextAttribute, getRequest(s.requestRegexps, span))
					}
				}

				s.logger.Debug("Capturing error trace",
					zap.String("traceId", traceId),
					zap.Float64("latency", summary.latency))
				requestState.errorQueue.push(&item)
			} else {
				summary.slowestRootSpan.Attributes().PutStr(AssertsRequestContextAttribute,
					getRequest(s.requestRegexps, summary.slowestRootSpan))
				s.logger.Debug("Capturing slow trace",
					zap.String("traceId", traceId),
					zap.Float64("latency", summary.latency))
				requestState.slowQueue.push(&item)
			}
		}
	} else if len(spanSet.rootSpans) > 0 && summary.requestKey.AsString() != "" {
		// Capture healthy samples based on configured sampling rate
		entry, _ := s.topTracesByService.LoadOrStore(entityKey, NewServiceQueues(s.config))
		perService := entry.(*serviceQueues)
		requestState := perService.getRequestState(summary.requestKey.request)
		samplingState, _ := perService.periodicSamplingStates.LoadOrStore(entityKey, &periodicSamplingState{
			lastSampleTime: 0,
			rwMutex:        &sync.RWMutex{},
		})
		if requestState != nil && samplingState.(*periodicSamplingState).sample(s.config.NormalSamplingFrequencyMinutes) {
			s.logger.Debug("Capturing normal trace",
				zap.String("traceId", traceId),
				zap.Float64("latency", summary.latency))

			// Capture request context as attribute and push to the latency queue to prioritize the healthy sample too
			summary.slowestRootSpan.Attributes().PutStr(AssertsRequestContextAttribute,
				getRequest(s.requestRegexps, summary.slowestRootSpan))
			requestState.slowQueue.push(&item)
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
			summary.requestKey = &RequestKey{
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

func (s *sampler) isSlow(namespace string, serviceName string, rootSpan *ptrace.Span, request string) bool {
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
				s.topTracesByService.Range(func(key any, value any) bool {
					var entityKey = key.(string)
					var sq = value.(*serviceQueues)

					sq.clearRequestStates().Range(func(key1 any, value1 any) bool {
						var requestKey = key1.(string)
						var _sampler = value1.(*traceSampler)

						// Flush all the errors
						if len(_sampler.errorQueue.priorityQueue) > 0 {
							s.logger.Debug("Flushing Error Traces for",
								zap.String("Service", entityKey),
								zap.String("Request", requestKey),
								zap.Int("Count", len(_sampler.errorQueue.priorityQueue)))
							for _, item := range _sampler.errorQueue.priorityQueue {
								_ = (*s).nextConsumer.ConsumeTraces(*item.ctx, *item.trace)
							}
						}

						// Flush all the slow traces
						if len(_sampler.slowQueue.priorityQueue) > 0 {
							s.logger.Debug("Flushing Slow Traces for",
								zap.String("Service", entityKey),
								zap.String("Request", requestKey),
								zap.Int("Count", len(_sampler.slowQueue.priorityQueue)))
							for _, item := range _sampler.slowQueue.priorityQueue {
								_ = (*s).nextConsumer.ConsumeTraces(*item.ctx, *item.trace)
							}
						}
						return true
					})
					return true
				})
			}
		}
	}()
}
