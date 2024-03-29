package assertsprocessor

import (
	"context"
	"github.com/jellydator/ttlcache/v3"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"sync"

	"github.com/tilinna/clock"
	"go.opentelemetry.io/collector/consumer"
	"go.uber.org/zap"
)

const (
	AssertsTraceSampleTypeAttribute = "asserts.sample.type"
	AssertsTraceSampleTypeNormal    = "normal"
	AssertsTraceSampleTypeSlow      = "slow"
	AssertsTraceSampleTypeError     = "error"
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
	metrics            *metrics
	rwMutex            *sync.RWMutex // guard access to config.IgnoreClientError
}

func (s *sampler) startProcessing() {
	s.thresholdHelper.startUpdates()
	s.startTraceFlusher()
}

func (s *sampler) stopProcessing() {
	s.thresholdHelper.stopUpdates()
	s.stopTraceFlusher()
}

func (s *sampler) sampleTraces(ctx context.Context, traces []*trace) {
	for _, tr := range traces {
		sampled := false
		for _, ts := range tr.segments {
			if ts.getMainSpan() == nil {
				continue
			}
			s.updateTrace(ts.namespace, ts.service, ts)
			entityKeyString := ts.requestKey.entityKey.AsString()

			// Get the trace queue for the entity and request
			perService, _ := s.topTracesByService.LoadOrStore(entityKeyString, newServiceQueues(s.config))
			request := ts.requestKey.request
			requestState := perService.(*serviceQueues).getRequestState(request)
			if requestState == nil {
				s.logger.Warn("Too many requests in Entity. Dropping",
					zap.String("Entity", entityKeyString),
					zap.String("Request", request))
				return
			}

			item := Item{
				trace:   tr,
				ctx:     &ctx,
				latency: ts.latency,
			}
			for _, span := range ts.getNonInternalSpans() {
				if spanHasError(span) && !s.ignoreErrorType(span) {
					s.logger.Debug("Capturing error trace",
						zap.String("traceId", span.TraceID().String()),
						zap.String("service", entityKeyString),
						zap.String("request", request),
						zap.Float64("latency", ts.latency))
					span.Attributes().PutStr(AssertsTraceSampleTypeAttribute, AssertsTraceSampleTypeError)

					if !sampled {
						item.sampleType = AssertsTraceSampleTypeError
						requestState.errorQueue.push(&item)
						sampled = true
					}
				} else if s.spanIsSlow(span, ts) {
					s.logger.Debug("Capturing slow trace",
						zap.String("traceId", span.TraceID().String()),
						zap.String("service", entityKeyString),
						zap.String("request", request),
						zap.Float64("latency", ts.latency))
					span.Attributes().PutStr(AssertsTraceSampleTypeAttribute, AssertsTraceSampleTypeSlow)

					if !sampled {
						item.sampleType = AssertsTraceSampleTypeSlow
						requestState.slowQueue.push(&item)
						sampled = true
					}
				}
			}
		}
		if !sampled {
			sampled = s.captureNormalTraceSample(ctx, tr)
		}
		s.metrics.incrTotalCounts(tr)
	}
}

func (s *sampler) captureNormalTraceSample(ctx context.Context, tr *trace) bool {
	for _, ts := range tr.segments {
		if ts.getMainSpan() == nil {
			continue
		}
		item := Item{
			trace:   tr,
			ctx:     &ctx,
			latency: ts.latency,
		}
		if s.captureNormalSample(ts, &item) {
			return true
		}
	}
	return false
}

func (s *sampler) captureNormalSample(ts *traceSegment, item *Item) bool {
	sampled := false
	// Capture healthy samples based on configured sampling rate
	entityKeyString := ts.requestKey.entityKey.AsString()
	request := ts.requestKey.request
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
			s.logger.Info("Adding request context to sampling states cache",
				zap.String("service", entityKeyString),
				zap.String("request context", request),
			)
		} else {
			samplingState = samplingStateKV.Value()
		}
		if samplingState.sample(s.config.NormalSamplingFrequencyMinutes) {
			sampled = true
			s.logger.Debug("Capturing normal trace",
				zap.String("traceId", ts.getMainSpan().TraceID().String()),
				zap.String("entity", entityKeyString),
				zap.String("request", request),
				zap.Float64("latency", ts.latency))

			// Capture request context as attribute and push to the latency queue to prioritize the healthy sample too
			ts.getMainSpan().Attributes().PutStr(AssertsTraceSampleTypeAttribute, AssertsTraceSampleTypeNormal)
			item.sampleType = AssertsTraceSampleTypeNormal
			requestState.slowQueue.push(item)
		}
	} else {
		s.logger.Warn("Too many request contexts. Normal traces won't be captured for",
			zap.String("service", entityKeyString),
			zap.String("request context", request),
		)
	}
	return sampled
}

func (s *sampler) updateTrace(namespace string, service string, ts *traceSegment) {
	entityKey := buildEntityKey(s.config, namespace, service)
	attrValue, _ := ts.getMainSpan().Attributes().Get(AssertsRequestContextAttribute)
	request := attrValue.Str()
	ts.latency = computeLatency(ts.getMainSpan())
	ts.requestKey = &RequestKey{
		entityKey: entityKey,
		request:   request,
	}
}

func (s *sampler) ignoreErrorType(span *ptrace.Span) bool {
	errorType, errorTypePresent := span.Attributes().Get(AssertsErrorTypeAttribute)
	return s.ignoreClientErrors() && errorTypePresent && "client_errors" == errorType.AsString()
}

func (s *sampler) spanIsSlow(span *ptrace.Span, ts *traceSegment) bool {
	attrValue, _ := span.Attributes().Get(AssertsRequestContextAttribute)
	request := attrValue.Str()
	latencyThreshold := s.thresholdHelper.getThreshold(ts.namespace, ts.service, request)
	latency := computeLatency(span)
	if latency > latencyThreshold {
		return true
	}
	return false
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
					var errorTraceCount = 0
					var slowTraceCount = 0

					sq.clearRequestStates().Range(func(key1 any, value1 any) bool {
						var requestKey = key1.(string)
						var _sampler = value1.(*traceSampler)

						// Flush all the errors
						if len(_sampler.errorQueue.priorityQueue) > 0 {
							errorTraceCount += len(_sampler.errorQueue.priorityQueue)
							s.logger.Debug("Flushing Error Traces for",
								zap.String("Service", entityKey),
								zap.String("Request", requestKey),
								zap.Int("Count", len(_sampler.errorQueue.priorityQueue)))
							for _, item := range _sampler.errorQueue.priorityQueue {
								s.metrics.incrSampledCounts(item.trace, item.sampleType)
								_ = (*s).nextConsumer.ConsumeTraces(*item.ctx, *buildTrace(item.trace))
							}
						}

						// Flush all the isSlow traces
						if len(_sampler.slowQueue.priorityQueue) > 0 {
							slowTraceCount += len(_sampler.slowQueue.priorityQueue)
							s.logger.Debug("Flushing Slow Traces for",
								zap.String("Service", entityKey),
								zap.String("Request", requestKey),
								zap.Int("Count", len(_sampler.slowQueue.priorityQueue)))
							for _, item := range _sampler.slowQueue.priorityQueue {
								s.metrics.incrSampledCounts(item.trace, item.sampleType)
								_ = (*s).nextConsumer.ConsumeTraces(*item.ctx, *buildTrace(item.trace))
							}
						}
						return true
					})
					if errorTraceCount > 0 || slowTraceCount > 0 {
						s.logger.Info("# of traces flushed for",
							zap.String("Service", entityKey),
							zap.Int("Error traces", errorTraceCount),
							zap.Int("Slow traces", slowTraceCount),
						)
					} else {
						s.logger.Info("No traces to flush for",
							zap.String("Service", entityKey),
						)
					}
					return true
				})
			}
		}
	}()
}

func (s *sampler) ignoreClientErrors() bool {
	s.rwMutex.RLock()
	defer s.rwMutex.RUnlock()

	return s.config.IgnoreClientErrors
}

// configListener interface implementation
func (s *sampler) isUpdated(currConfig *Config, newConfig *Config) bool {
	s.rwMutex.RLock()
	defer s.rwMutex.RUnlock()

	updated := currConfig.IgnoreClientErrors != newConfig.IgnoreClientErrors
	if updated {
		s.logger.Info("Change detected in config IgnoreClientErrors",
			zap.Any("Current", currConfig.IgnoreClientErrors),
			zap.Any("New", newConfig.IgnoreClientErrors),
		)
	} else {
		s.logger.Debug("No change detected in config IgnoreClientErrors")
	}
	return updated
}

func (s *sampler) onUpdate(newConfig *Config) error {
	s.rwMutex.Lock()
	defer s.rwMutex.Unlock()

	s.config.IgnoreClientErrors = newConfig.IgnoreClientErrors
	s.logger.Info("Updated config IgnoreClientErrors",
		zap.Bool("New", s.config.IgnoreClientErrors),
	)
	return nil
}
