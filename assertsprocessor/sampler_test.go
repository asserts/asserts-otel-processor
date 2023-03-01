package assertsprocessor

import (
	"context"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/stretchr/testify/assert"
	"github.com/tilinna/clock"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
	"go.uber.org/zap"
	"regexp"
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

var entityKeys = cmap.New[EntityKeyDto]()
var thresholdCache = cmap.New[cmap.ConcurrentMap[string, ThresholdDto]]()
var th = thresholdHelper{
	logger:     logger,
	config:     &config,
	entityKeys: entityKeys,
	thresholds: thresholdCache,
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

func TestGetEntityMap(t *testing.T) {
	cache := cmap.New[cmap.ConcurrentMap[string, *traceQueues]]()
	var s = sampler{
		logger:          logger,
		config:          &config,
		thresholdHelper: &th,
		topTraces:       &cache,
	}
	assert.Equal(t, 0, cache.Count())
	assert.NotNil(t, s.getEntityMap(entityKey))
	assert.Equal(t, 1, cache.Count())
}

func TestGetTracesQueue(t *testing.T) {
	cache := cmap.New[cmap.ConcurrentMap[string, *traceQueues]]()
	compile, err := regexp.Compile("https?://.+?(/.+?)\\??")
	assert.Nil(t, err)
	var s = sampler{
		logger:          logger,
		config:          &config,
		thresholdHelper: &th,
		topTraces:       &cache,
		requestRegexps: &map[string]regexp.Regexp{
			"http.url": *compile,
		},
	}

	testSpan := ptrace.NewSpan()
	testSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	queues := s.getTraceQueues(entityKey, testSpan)
	assert.NotNil(t, queues)
	assert.NotNil(t, queues.errorQueue)
	assert.NotNil(t, queues.latencyQueue)
}

func TestSampleTraceWithError(t *testing.T) {
	cache := cmap.New[cmap.ConcurrentMap[string, *traceQueues]]()
	compile, err := regexp.Compile("https?://.+?(/.+)")
	assert.Nil(t, err)
	var s = sampler{
		logger:          logger,
		config:          &config,
		thresholdHelper: &th,
		topTraces:       &cache,
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

	assert.Equal(t, 1, s.topTraces.Count())
	_map, found := s.topTraces.Get(entityKey.AsString())
	assert.True(t, found)
	assert.Equal(t, 1, _map.Count())
	queues, b := _map.Get("/api-server/v4/rules")
	assert.True(t, b)

	assert.Equal(t, 1, queues.errorTraceCount())
	assert.Equal(t, Item{
		trace:   &testTrace,
		ctx:     &ctx,
		latency: 0.4,
	}, *queues.errorQueue.priorityQueue[0])
}

func TestSampleTraceWithHighLatency(t *testing.T) {
	cache := cmap.New[cmap.ConcurrentMap[string, *traceQueues]]()
	compile, err := regexp.Compile("https?://.+?(/.+)")
	assert.Nil(t, err)
	var s = sampler{
		logger:          logger,
		config:          &config,
		thresholdHelper: &th,
		topTraces:       &cache,
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

	assert.Equal(t, 1, s.topTraces.Count())
	_map, found := s.topTraces.Get(entityKey.AsString())
	assert.True(t, found)
	assert.Equal(t, 1, _map.Count())
	queues, b := _map.Get("/api-server/v4/rules")
	assert.True(t, b)

	assert.Equal(t, 1, queues.latencyTraceCount())
	assert.Equal(t, Item{
		trace:   &testTrace,
		ctx:     &ctx,
		latency: 0.6,
	}, *queues.latencyQueue.priorityQueue[0])
}

func TestFlushTraces(t *testing.T) {
	cache := cmap.New[cmap.ConcurrentMap[string, *traceQueues]]()
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
		topTraces:       &cache,
		requestRegexps: &map[string]regexp.Regexp{
			"http.url": *compile,
		},
		traceFlushTicker: clock.FromContext(ctx).NewTicker(3 * time.Second),
		nextConsumer:     dConsumer,
		stop:             make(chan bool),
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

	assert.Equal(t, 1, s.topTraces.Count())
	_map, found := s.topTraces.Get(entityKey.AsString())
	assert.True(t, found)
	assert.Equal(t, 1, _map.Count())
	queues, b := _map.Get("/api-server/v4/rules")
	assert.True(t, b)

	assert.Equal(t, 1, queues.latencyTraceCount())
	assert.Equal(t, Item{
		trace:   &latencyTrace,
		ctx:     &ctx,
		latency: 0.6,
	}, *queues.latencyQueue.priorityQueue[0])

	assert.Equal(t, 1, queues.errorTraceCount())
	assert.Equal(t, Item{
		trace:   &errorTrace,
		ctx:     &ctx,
		latency: 0.3,
	}, *queues.errorQueue.priorityQueue[0])

	go func() { s.flushTraces() }()
	time.Sleep(5 * time.Second)
	assert.Equal(t, 0, s.topTraces.Count())
	s.stopFlushing()
	time.Sleep(5 * time.Second)
}
