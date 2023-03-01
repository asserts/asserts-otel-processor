package assertsprocessor

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/tilinna/clock"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
	"go.uber.org/atomic"
	"go.uber.org/zap"
	"regexp"
	"sync"
	"testing"
	"time"
)

var logger, _ = zap.NewProduction()
var config = Config{
	Env:                            "dev",
	Site:                           "us-west-2",
	AssertsServer:                  "http://localhost:8030",
	DefaultLatencyThreshold:        0.5,
	MaxTracesPerMinute:             100,
	MaxTracesPerMinutePerContainer: 5,
}

var th = thresholdHelper{
	logger:     logger,
	config:     &config,
	entityKeys: &sync.Map{},
	thresholds: &sync.Map{},
}

var entityKey = EntityKeyDto{
	Type: "Service", Name: "api-server", Scope: map[string]string{
		"env": "dev", "site": "us-west-2", "namespace": "platform",
	},
}

func TestLatencyIsHighTrue(t *testing.T) {
	var s = sampler{
		logger:          logger,
		config:          &config,
		thresholdHelper: &th,
	}

	testSpan := ptrace.NewSpan()
	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(1e9 + 6e8)

	assert.True(t, s.latencyIsHigh("platform", "api-server", testSpan))
}

func TestLatencyIsHighFalse(t *testing.T) {
	var s = sampler{
		logger:          logger,
		config:          &config,
		thresholdHelper: &th,
	}

	testSpan := ptrace.NewSpan()
	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(1e9 + 4e8)

	assert.False(t, s.latencyIsHigh("platform", "api-server", testSpan))
}

func TestGetTracesQueue(t *testing.T) {
	cache := sync.Map{}
	compile, err := regexp.Compile("https?://.+?(/.+?)\\??")
	assert.Nil(t, err)
	var s = sampler{
		logger:          logger,
		config:          &config,
		thresholdHelper: &th,
		topTracesMap:    &cache,
		requestRegexps: &map[string]regexp.Regexp{
			"http.url": *compile,
		},
	}

	queues := s.getTraceQueues(RequestKey{
		entityKey: entityKey, request: "request",
	})
	assert.NotNil(t, queues)
	assert.NotNil(t, queues.errorQueue)
	assert.NotNil(t, queues.slowQueue)
}

func TestSampleTraceWithError(t *testing.T) {
	cache := sync.Map{}
	compile, err := regexp.Compile("https?://.+?(/.+)")
	assert.Nil(t, err)
	var s = sampler{
		logger:          logger,
		config:          &config,
		thresholdHelper: &th,
		topTracesMap:    &cache,
		requestRegexps: &map[string]regexp.Regexp{
			"http.url": *compile,
		},
	}

	ctx := context.Background()
	testTrace := ptrace.NewTraces()
	resourceSpans := testTrace.ResourceSpans().AppendEmpty()
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceName, "api-server")
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()

	testSpan := scopeSpans.Spans().AppendEmpty()
	testSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	testSpan.Attributes().PutBool("error", true)
	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(1e9 + 4e8)

	s.sampleTrace(entityKey.Scope["namespace"], entityKey.Name, ctx, testTrace, testSpan)

	s.topTracesMap.Range(func(key any, value any) bool {
		stringKey := key.(string)
		traceQueue := *value.(*traceQueues)
		assert.Equal(t, "{, env=dev, namespace=platform, site=us-west-2}#Service#api-server#/api-server/v4/rules", stringKey)
		assert.Equal(t, 0, traceQueue.slowTraceCount())
		assert.Equal(t, 1, traceQueue.errorTraceCount())
		item := *traceQueue.errorQueue.priorityQueue[0]
		assert.Equal(t, testTrace, *item.trace)
		assert.Equal(t, ctx, *item.ctx)
		assert.Equal(t, 0.4, item.latency)
		return true
	})
}

func TestSampleTraceWithHighLatency(t *testing.T) {
	cache := sync.Map{}
	compile, err := regexp.Compile("https?://.+?(/.+)")
	assert.Nil(t, err)
	var s = sampler{
		logger:          logger,
		config:          &config,
		thresholdHelper: &th,
		topTracesMap:    &cache,
		requestRegexps: &map[string]regexp.Regexp{
			"http.url": *compile,
		},
	}

	ctx := context.Background()
	testTrace := ptrace.NewTraces()
	resourceSpans := testTrace.ResourceSpans().AppendEmpty()
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceName, "api-server")
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()

	testSpan := scopeSpans.Spans().AppendEmpty()
	testSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(1e9 + 6e8)

	s.sampleTrace(entityKey.Scope["namespace"], entityKey.Name, ctx, testTrace, testSpan)

	s.topTracesMap.Range(func(key any, value any) bool {
		stringKey := key.(string)
		traceQueue := *value.(*traceQueues)
		assert.Equal(t, "{, env=dev, namespace=platform, site=us-west-2}#Service#api-server#/api-server/v4/rules", stringKey)
		assert.Equal(t, 0, traceQueue.errorTraceCount())
		assert.Equal(t, 1, traceQueue.slowTraceCount())
		item := *traceQueue.slowQueue.priorityQueue[0]
		assert.Equal(t, testTrace, *item.trace)
		assert.Equal(t, ctx, *item.ctx)
		assert.Equal(t, 0.6, item.latency)
		return true
	})
}

func TestSampleNormalTrace(t *testing.T) {
	cache := sync.Map{}
	compile, err := regexp.Compile("https?://.+?(/.+)")
	assert.Nil(t, err)
	var s = sampler{
		logger:          logger,
		config:          &config,
		thresholdHelper: &th,
		topTracesMap:    &cache,
		requestRegexps: &map[string]regexp.Regexp{
			"http.url": *compile,
		},
		healthySamplingState: &sync.Map{},
	}

	ctx := context.Background()
	testTrace := ptrace.NewTraces()
	resourceSpans := testTrace.ResourceSpans().AppendEmpty()
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceName, "api-server")
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()

	testSpan := scopeSpans.Spans().AppendEmpty()
	testSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(1e9 + 3e8)

	s.sampleTrace(entityKey.Scope["namespace"], entityKey.Name, ctx, testTrace, testSpan)

	s.topTracesMap.Range(func(key any, value any) bool {
		stringKey := key.(string)
		traceQueue := *value.(*traceQueues)
		assert.Equal(t, "{, env=dev, namespace=platform, site=us-west-2}#Service#api-server#/api-server/v4/rules", stringKey)
		assert.Equal(t, 0, traceQueue.errorTraceCount())
		assert.Equal(t, 1, traceQueue.slowTraceCount())
		item := *traceQueue.slowQueue.priorityQueue[0]
		assert.Equal(t, testTrace, *item.trace)
		assert.Equal(t, ctx, *item.ctx)
		assert.Equal(t, 0.3, item.latency)
		return true
	})
}

func TestFlushTraces(t *testing.T) {
	cache := sync.Map{}
	compile, err := regexp.Compile("https?://.+?(/.+)")
	assert.Nil(t, err)

	ctx := context.Background()
	dConsumer := dummyConsumer{
		items: make([]*Item, 0),
	}
	var s = sampler{
		logger:          logger,
		config:          &config,
		thresholdHelper: &th,
		topTracesMap:    &cache,
		requestRegexps: &map[string]regexp.Regexp{
			"http.url": *compile,
		},
		traceFlushTicker: clock.FromContext(ctx).NewTicker(time.Second),
		nextConsumer:     dConsumer,
		stop:             make(chan bool, 5),
	}

	latencyTrace := ptrace.NewTraces()
	resourceSpans := latencyTrace.ResourceSpans().AppendEmpty()
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceName, "api-server")
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()

	latencySpan := scopeSpans.Spans().AppendEmpty()
	latencySpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	latencySpan.SetStartTimestamp(1e9)
	latencySpan.SetEndTimestamp(1e9 + 6e8)

	s.sampleTrace(entityKey.Scope["namespace"], entityKey.Name, ctx, latencyTrace, latencySpan)

	errorTrace := ptrace.NewTraces()
	resourceSpans = errorTrace.ResourceSpans().AppendEmpty()
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceName, "api-server")
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans = resourceSpans.ScopeSpans().AppendEmpty()

	errorSpan := scopeSpans.Spans().AppendEmpty()
	errorSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	errorSpan.Attributes().PutBool("error", true)
	errorSpan.SetStartTimestamp(1e9)
	errorSpan.SetEndTimestamp(1e9 + 3e8)

	s.sampleTrace(entityKey.Scope["namespace"], entityKey.Name, ctx, errorTrace, errorSpan)

	counter := atomic.Int32{}
	s.topTracesMap.Range(func(key any, value any) bool {
		counter.Inc()
		return true
	})
	assert.Equal(t, int32(1), counter.Load())

	counter = atomic.Int32{}
	go func() { s.flushTraces() }()
	time.Sleep(2 * time.Second)
	s.topTracesMap.Range(func(key any, value any) bool {
		counter.Inc()
		return true
	})
	assert.Equal(t, int32(0), counter.Load())
	s.stopFlushing()
	time.Sleep(1 * time.Second)
}
