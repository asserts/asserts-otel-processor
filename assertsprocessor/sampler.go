package assertsprocessor

import (
	"context"
	"github.com/jellydator/ttlcache/v3"
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
	spanMatcher        *spanMatcher
}

func (s *sampler) startProcessing() {
	s.thresholdHelper.startUpdates()
	s.startTraceFlusher()
}

func (s *sampler) stopProcessing() {
	s.thresholdHelper.stopUpdates()
	s.stopTraceFlusher()
}

func (s *sampler) sampleTraces(ctx context.Context, traces *resourceTraces) {
	for _, traceStruct := range *traces.traceById {
		if traceStruct.rootSpan == nil {
			continue
		}
		s.updateTrace(traces.namespace, traces.service, traceStruct)
		entityKeyString := traceStruct.requestKey.entityKey.AsString()

		// Get the traceStruct queue for the entity and request
		perService, _ := s.topTracesByService.LoadOrStore(entityKeyString, newServiceQueues(s.config))
		request := traceStruct.requestKey.request
		requestState := perService.(*serviceQueues).getRequestState(request)
		if requestState == nil {
			s.logger.Warn("Too many requests in Entity. Dropping",
				zap.String("Entity", entityKeyString),
				zap.String("Request", request))
			return
		}

		item := Item{
			trace:   traceStruct,
			ctx:     &ctx,
			latency: traceStruct.latency,
		}
		if traceStruct.hasError() {
			// For all the spans which have error, add the request context
			traceStruct.rootSpan.Attributes().PutStr(AssertsRequestContextAttribute, request)
			for _, span := range traceStruct.exitSpans {
				if spanHasError(span) {
					span.Attributes().PutStr(AssertsRequestContextAttribute, request)
				}
			}

			s.logger.Debug("Capturing error trace",
				zap.String("traceId", traceStruct.rootSpan.TraceID().String()),
				zap.String("service", entityKeyString),
				zap.String("request", traceStruct.requestKey.request),
				zap.Float64("latency", traceStruct.latency))
			requestState.errorQueue.push(&item)
		} else if traceStruct.isSlow {
			traceStruct.rootSpan.Attributes().PutStr(AssertsRequestContextAttribute, request)

			s.logger.Debug("Capturing slow trace",
				zap.String("traceId", traceStruct.rootSpan.TraceID().String()),
				zap.String("service", entityKeyString),
				zap.String("request", traceStruct.requestKey.request),
				zap.Float64("latencyThreshold", traceStruct.latencyThreshold),
				zap.Float64("latency", traceStruct.latency))
			requestState.slowQueue.push(&item)
		} else {
			s.captureNormalSample(&item)
		}
	}
}

func (s *sampler) captureNormalSample(item *Item) {
	// Capture healthy samples based on configured sampling rate
	entityKeyString := item.trace.requestKey.entityKey.AsString()
	request := item.trace.requestKey.request
	entry, _ := s.topTracesByService.LoadOrStore(entityKeyString, newServiceQueues(s.config))
	perService := entry.(*serviceQueues)
	requestState := perService.getRequestState(request)
	samplingStates := perService.periodicSamplingStates
	samplingStateKV := samplingStates.Get(request)

	if samplingStates.Len() < s.config.LimitPerService || samplingStateKV != nil {
		var samplingState *periodicSamplingState = nil
		if samplingStateKV == nil {
			samplingState = &periodicSamplingState{
				lastSampleTime: 0,
				rwMutex:        &sync.RWMutex{},
			}
			samplingStates.Set(request, samplingState, ttlcache.DefaultTTL)
			s.logger.Debug("Adding request context to cache",
				zap.String("service", entityKeyString),
				zap.String("request context", request),
			)
		} else {
			samplingState = samplingStateKV.Value()
		}
		if samplingState.sample(s.config.NormalSamplingFrequencyMinutes) {
			s.logger.Debug("Capturing normal trace",
				zap.String("traceId", item.trace.rootSpan.TraceID().String()),
				zap.String("entity", entityKeyString),
				zap.String("request", request),
				zap.Float64("latency", item.trace.latency))

			// Capture request context as attribute and push to the latency queue to prioritize the healthy sample too
			item.trace.rootSpan.Attributes().PutStr(AssertsRequestContextAttribute,
				s.spanMatcher.getRequest(item.trace.rootSpan))
			requestState.slowQueue.push(item)
		}
	} else {
		s.logger.Warn("Too many request contexts. Normal traces won't be captured for",
			zap.String("service", entityKeyString),
			zap.String("request context", request),
		)
	}
}

func (s *sampler) updateTrace(namespace string, service string, trace *traceStruct) {
	entityKey := buildEntityKey(s.config, namespace, service)
	request := s.spanMatcher.getRequest(trace.rootSpan)
	trace.isSlow = s.isSlow(namespace, service, trace.rootSpan, request)
	if trace.isSlow {
		trace.latencyThreshold = s.thresholdHelper.getThreshold(namespace, service, request)
	}
	trace.latency = computeLatency(trace.rootSpan)
	trace.requestKey = &RequestKey{
		entityKey: entityKey,
		request:   request,
	}
}

func (s *sampler) isSlow(namespace string, serviceName string, rootSpan *ptrace.Span, request string) bool {
	return computeLatency(rootSpan) > s.thresholdHelper.getThreshold(namespace, serviceName, request)
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
								_ = (*s).nextConsumer.ConsumeTraces(*item.ctx, *buildTrace(item.trace))
							}
						}

						// Flush all the isSlow traces
						if len(_sampler.slowQueue.priorityQueue) > 0 {
							s.logger.Debug("Flushing Slow Traces for",
								zap.String("Service", entityKey),
								zap.String("Request", requestKey),
								zap.Int("Count", len(_sampler.slowQueue.priorityQueue)))
							for _, item := range _sampler.slowQueue.priorityQueue {
								_ = (*s).nextConsumer.ConsumeTraces(*item.ctx, *buildTrace(item.trace))
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
