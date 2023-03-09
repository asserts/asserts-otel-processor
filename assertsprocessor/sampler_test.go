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
	Env:                       "dev",
	Site:                      "us-west-2",
	AssertsServer:             &map[string]string{"endpoint": "http://localhost:8030"},
	DefaultLatencyThreshold:   0.5,
	LimitPerService:           100,
	LimitPerRequestPerService: 5,
}

var th = thresholdHelper{
	logger:     logger,
	config:     &config,
	entityKeys: &sync.Map{},
	thresholds: &sync.Map{},
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

	assert.True(t, s.isSlow("platform", "api-server", testSpan, "/api"))
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

	assert.False(t, s.isSlow("platform", "api-server", testSpan, "/api"))
}

//func TestSampleTraceWithError(t *testing.T) {
//	cache := sync.Map{}
//	compile, err := regexp.Compile("https?://.+?(/.+)")
//	assert.Nil(t, err)
//	var s = sampler{
//		logger:          logger,
//		config:          &config,
//		thresholdHelper: &th,
//		topTracesByService:    &cache,
//		requestRegexps: &map[string]regexp.Regexp{
//			"http.url": *compile,
//		},
//		healthySamplingState: &sync.Map{},
//	}
//
//	ctx := context.Background()
//	testTrace := ptrace.NewTraces()
//	resourceSpans := testTrace.ResourceSpans().AppendEmpty()
//	attributes := resourceSpans.Resource().Attributes()
//	attributes.PutStr(conventions.AttributeServiceName, "api-server")
//	attributes.PutStr(conventions.AttributeServiceNamespace, "platform")
//	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()
//
//	rootSpan := scopeSpans.Spans().AppendEmpty()
//	rootSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
//	rootSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
//	rootSpan.SetStartTimestamp(1e9)
//	rootSpan.SetEndTimestamp(1e9 + 7e8)
//
//	childSpan := scopeSpans.Spans().AppendEmpty()
//	childSpan.SetParentSpanID(rootSpan.SpanID())
//	childSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 9})
//	childSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
//	childSpan.Status()
//	childSpan.SetStartTimestamp(1e9 + 1e8)
//	childSpan.SetEndTimestamp(1e9 + 5e8)
//
//	s.sampleTrace(ctx, testTrace, "", &resourceSpanGroup{
//		namespace: "platform", service: "api-server",
//		rootSpans:          []ptrace.Span{rootSpan},
//		nestedSpans:        []ptrace.Span{childSpan},
//		resourceAttributes: &attributes,
//	})
//
//	s.topTracesByService.Range(func(key any, value any) bool {
//		stringKey := key.(string)
//		traceQueue := *value.(*traceSampler)
//		assert.Equal(t, "{, env=dev, namespace=platform, site=us-west-2}#Service#api-server#/api-server/v4/rules", stringKey)
//		assert.Equal(t, 0, traceQueue.slowTraceCount())
//		assert.Equal(t, 1, traceQueue.errorTraceCount())
//		item := *traceQueue.errorQueue.priorityQueue[0]
//		assert.Equal(t, testTrace, *item.trace)
//		assert.Equal(t, ctx, *item.ctx)
//		assert.Equal(t, 0.7, item.latency)
//		return true
//	})
//}

func TestSampleTraceWithHighLatency(t *testing.T) {
	cache := sync.Map{}
	compile, err := regexp.Compile("https?://.+?(/.+)")
	assert.Nil(t, err)
	var s = sampler{
		logger:             logger,
		config:             &config,
		thresholdHelper:    &th,
		topTracesByService: &cache,
		requestRegexps: &map[string]*regexp.Regexp{
			"http.url": compile,
		},
	}

	ctx := context.Background()
	testTrace := ptrace.NewTraces()
	resourceSpans := testTrace.ResourceSpans().AppendEmpty()
	attributes := resourceSpans.Resource().Attributes()
	attributes.PutStr(conventions.AttributeServiceName, "api-server")
	attributes.PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()

	rootSpan := scopeSpans.Spans().AppendEmpty()
	rootSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	rootSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	rootSpan.SetStartTimestamp(1e9)
	rootSpan.SetEndTimestamp(1e9 + 7e8)

	childSpan := scopeSpans.Spans().AppendEmpty()
	childSpan.SetParentSpanID(rootSpan.SpanID())
	childSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 9})
	childSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	childSpan.SetStartTimestamp(1e9 + 1e8)
	childSpan.SetEndTimestamp(1e9 + 5e8)

	s.sampleTrace(ctx, testTrace, "", &resourceSpanGroup{
		namespace: "platform", service: "api-server",
		rootSpans:          []ptrace.Span{rootSpan},
		nestedSpans:        []ptrace.Span{childSpan},
		resourceAttributes: &attributes,
	})

	s.topTracesByService.Range(func(key any, value any) bool {
		stringKey := key.(string)
		serviceQueue := *value.(*serviceQueues)
		assert.Equal(t, "{env=dev, namespace=platform, site=us-west-2}#Service#api-server", stringKey)
		assert.Equal(t, 1, serviceQueue.requestCount)
		assert.NotNil(t, serviceQueue.getRequestState("/api-server/v4/rules"))
		assert.NotNil(t, 1, serviceQueue.getRequestState("/api-server/v4/rules").slowTraceCount())
		assert.NotNil(t, 0, serviceQueue.getRequestState("/api-server/v4/rules").errorTraceCount())
		item := *serviceQueue.getRequestState("/api-server/v4/rules").slowQueue.priorityQueue[0]
		assert.Equal(t, testTrace, *item.trace)
		assert.Equal(t, ctx, *item.ctx)
		assert.Equal(t, 0.7, item.latency)
		return true
	})
}

func TestSampleNormalTrace(t *testing.T) {
	cache := sync.Map{}
	compile, err := regexp.Compile("https?://.+?(/.+)")
	assert.Nil(t, err)
	var s = sampler{
		logger:             logger,
		config:             &config,
		thresholdHelper:    &th,
		topTracesByService: &cache,
		requestRegexps: &map[string]*regexp.Regexp{
			"http.url": compile,
		},
	}

	ctx := context.Background()
	testTrace := ptrace.NewTraces()
	resourceSpans := testTrace.ResourceSpans().AppendEmpty()
	attributes := resourceSpans.Resource().Attributes()
	attributes.PutStr(conventions.AttributeServiceName, "api-server")
	attributes.PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()

	rootSpan := scopeSpans.Spans().AppendEmpty()
	rootSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	rootSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	rootSpan.SetStartTimestamp(1e9)
	rootSpan.SetEndTimestamp(1e9 + 4e8)

	childSpan := scopeSpans.Spans().AppendEmpty()
	childSpan.SetParentSpanID(rootSpan.SpanID())
	childSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 9})
	childSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	childSpan.SetStartTimestamp(1e9 + 2e8)
	childSpan.SetEndTimestamp(1e9 + 3e8)

	s.sampleTrace(ctx, testTrace, "", &resourceSpanGroup{
		namespace: "platform", service: "api-server",
		rootSpans:          []ptrace.Span{rootSpan},
		nestedSpans:        []ptrace.Span{childSpan},
		resourceAttributes: &attributes,
	})

	s.topTracesByService.Range(func(key any, value any) bool {
		stringKey := key.(string)
		serviceQueue := *value.(*serviceQueues)
		assert.Equal(t, "{env=dev, namespace=platform, site=us-west-2}#Service#api-server", stringKey)
		assert.Equal(t, 1, serviceQueue.requestCount)
		assert.NotNil(t, serviceQueue.getRequestState("/api-server/v4/rules"))
		assert.NotNil(t, 1, serviceQueue.getRequestState("/api-server/v4/rules").slowTraceCount())
		assert.NotNil(t, 0, serviceQueue.getRequestState("/api-server/v4/rules").errorTraceCount())
		item := *serviceQueue.getRequestState("/api-server/v4/rules").slowQueue.priorityQueue[0]
		assert.Equal(t, testTrace, *item.trace)
		assert.Equal(t, ctx, *item.ctx)
		assert.Equal(t, 0.4, item.latency)
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
		logger:             logger,
		config:             &config,
		thresholdHelper:    &th,
		topTracesByService: &cache,
		requestRegexps: &map[string]*regexp.Regexp{
			"http.url": compile,
		},
		traceFlushTicker: clock.FromContext(ctx).NewTicker(time.Second),
		nextConsumer:     dConsumer,
		stop:             make(chan bool, 5),
	}

	latencyTrace := ptrace.NewTraces()
	resourceSpans := latencyTrace.ResourceSpans().AppendEmpty()
	attributes := resourceSpans.Resource().Attributes()
	attributes.PutStr(conventions.AttributeServiceName, "api-server")
	attributes.PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()

	latencySpan := scopeSpans.Spans().AppendEmpty()
	latencySpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	latencySpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	latencySpan.SetStartTimestamp(1e9)
	latencySpan.SetEndTimestamp(1e9 + 6e8)

	s.sampleTrace(ctx, latencyTrace, "", &resourceSpanGroup{
		namespace: "platform", service: "api-server",
		rootSpans:          []ptrace.Span{latencySpan},
		nestedSpans:        []ptrace.Span{},
		resourceAttributes: &attributes,
	})

	errorTrace := ptrace.NewTraces()
	resourceSpans = errorTrace.ResourceSpans().AppendEmpty()
	attributes = resourceSpans.Resource().Attributes()
	attributes.PutStr(conventions.AttributeServiceName, "api-server")
	attributes.PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans = resourceSpans.ScopeSpans().AppendEmpty()

	errorSpan := scopeSpans.Spans().AppendEmpty()
	errorSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 9})
	errorSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	errorSpan.Attributes().PutBool("error", true)
	errorSpan.SetStartTimestamp(1e9)
	errorSpan.SetEndTimestamp(1e9 + 3e8)

	s.sampleTrace(ctx, errorTrace, "", &resourceSpanGroup{
		namespace: "platform", service: "api-server",
		rootSpans:          []ptrace.Span{errorSpan},
		nestedSpans:        []ptrace.Span{},
		resourceAttributes: &attributes,
	})

	normalTrace := ptrace.NewTraces()
	resourceSpans = normalTrace.ResourceSpans().AppendEmpty()
	attributes = resourceSpans.Resource().Attributes()
	attributes.PutStr(conventions.AttributeServiceName, "api-server")
	attributes.PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans = resourceSpans.ScopeSpans().AppendEmpty()

	normalSpan := scopeSpans.Spans().AppendEmpty()
	normalSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 10})
	normalSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	normalSpan.SetStartTimestamp(1e9)
	normalSpan.SetEndTimestamp(1e9 + 3e8)

	s.sampleTrace(ctx, errorTrace, "", &resourceSpanGroup{
		namespace: "platform", service: "api-server",
		rootSpans:          []ptrace.Span{normalSpan},
		nestedSpans:        []ptrace.Span{},
		resourceAttributes: &attributes,
	})

	counter := atomic.Int32{}
	s.topTracesByService.Range(func(key any, value any) bool {
		counter.Inc()
		return true
	})
	assert.Equal(t, int32(1), counter.Load())

	counter = atomic.Int32{}
	go func() { s.startTraceFlusher() }()
	time.Sleep(2 * time.Second)
	s.topTracesByService.Range(func(key any, value any) bool {
		counter.Inc()
		return true
	})
	assert.Equal(t, int32(0), counter.Load())
	s.stopProcessing()
	time.Sleep(1 * time.Second)
}
