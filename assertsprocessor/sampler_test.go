package assertsprocessor

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/tilinna/clock"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
	"go.uber.org/zap"
)

var logger, _ = zap.NewProduction()
var config = Config{
	Env:                       "dev",
	Site:                      "us-west-2",
	AssertsServer:             &map[string]string{"endpoint": "http://localhost:8030"},
	DefaultLatencyThreshold:   0.5,
	LimitPerService:           2,
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

	assert.True(t, s.isSlow("platform", "api-server", &testSpan, "/api"))
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

	assert.False(t, s.isSlow("platform", "api-server", &testSpan, "/api"))
}

func TestSampleTraceWithError(t *testing.T) {
	cache := sync.Map{}
	compile, err := regexp.Compile("https?://.+?(/.+)")
	assert.Nil(t, err)
	var s = sampler{
		logger:             logger,
		config:             &config,
		thresholdHelper:    &th,
		topTracesByService: &cache,
		spanMatcher: &spanMatcher{
			spanAttrMatchers: []*spanAttrMatcher{
				{
					attrName: "http.url",
					regex:    compile,
				},
			},
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
	childSpan.SetKind(ptrace.SpanKindClient)
	childSpan.Status().SetCode(ptrace.StatusCodeError)
	childSpan.SetStartTimestamp(1e9 + 1e8)
	childSpan.SetEndTimestamp(1e9 + 5e8)

	traceById := map[string]*traceStruct{}
	traceById[rootSpan.TraceID().String()] = &traceStruct{
		rootSpan:  &rootSpan,
		exitSpans: []*ptrace.Span{&childSpan},
	}

	s.sampleTraces(ctx, &resourceTraces{
		namespace: "platform", service: "api-server",
		traceById: &traceById,
	})

	s.topTracesByService.Range(func(key any, value any) bool {
		stringKey := key.(string)
		serviceQueue := *value.(*serviceQueues)
		assert.Equal(t, "{env=dev, namespace=platform, site=us-west-2}#Service#api-server", stringKey)
		assert.Equal(t, 1, serviceQueue.requestCount)
		assert.NotNil(t, serviceQueue.getRequestState("/api-server/v4/rules"))
		assert.Equal(t, 0, serviceQueue.getRequestState("/api-server/v4/rules").slowTraceCount())
		assert.Equal(t, 1, serviceQueue.getRequestState("/api-server/v4/rules").errorTraceCount())
		item := *serviceQueue.getRequestState("/api-server/v4/rules").errorQueue.priorityQueue[0]
		assert.NotNil(t, item.trace)
		assert.Equal(t, &rootSpan, item.trace.rootSpan)
		assert.Equal(t, 1, len((*item.trace).exitSpans))
		assert.NotNil(t, &childSpan, item.trace.exitSpans[0])
		assert.Equal(t, ctx, *item.ctx)
		assert.Equal(t, 0.7, item.latency)
		_, found := rootSpan.Attributes().Get(AssertsRequestContextAttribute)
		assert.True(t, found)
		return true
	})
}

func TestSampleTraceWithHighLatency(t *testing.T) {
	cache := sync.Map{}
	compile, err := regexp.Compile("https?://.+?(/.+)")
	assert.Nil(t, err)
	s := sampler{
		logger:             logger,
		config:             &config,
		thresholdHelper:    &th,
		topTracesByService: &cache,
		spanMatcher: &spanMatcher{
			spanAttrMatchers: []*spanAttrMatcher{
				{
					attrName: "http.url",
					regex:    compile,
				},
			},
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
	rootSpan.SetTraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 8})
	rootSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	rootSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	rootSpan.SetStartTimestamp(1e9)
	rootSpan.SetEndTimestamp(1e9 + 7e8)

	childSpan := scopeSpans.Spans().AppendEmpty()
	childSpan.SetTraceID(rootSpan.TraceID())
	childSpan.SetParentSpanID(rootSpan.SpanID())
	childSpan.SetKind(ptrace.SpanKindClient)
	childSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 9})
	childSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	childSpan.SetStartTimestamp(1e9 + 1e8)
	childSpan.SetEndTimestamp(1e9 + 5e8)

	traceById := map[string]*traceStruct{}
	traceById[rootSpan.TraceID().String()] = &traceStruct{
		rootSpan:  &rootSpan,
		exitSpans: []*ptrace.Span{&childSpan},
	}

	s.sampleTraces(ctx, &resourceTraces{
		namespace: "platform", service: "api-server",
		traceById: &traceById,
	})

	s.topTracesByService.Range(func(key any, value any) bool {
		stringKey := key.(string)
		serviceQueue := *value.(*serviceQueues)
		assert.Equal(t, "{env=dev, namespace=platform, site=us-west-2}#Service#api-server", stringKey)
		assert.Equal(t, 1, serviceQueue.requestCount)
		assert.NotNil(t, serviceQueue.getRequestState("/api-server/v4/rules"))
		assert.Equal(t, 1, serviceQueue.getRequestState("/api-server/v4/rules").slowTraceCount())
		assert.Equal(t, 0, serviceQueue.getRequestState("/api-server/v4/rules").errorTraceCount())
		item := *serviceQueue.getRequestState("/api-server/v4/rules").slowQueue.priorityQueue[0]
		assert.NotNil(t, item.trace)
		assert.Equal(t, &rootSpan, item.trace.rootSpan)
		assert.Equal(t, 1, len((*item.trace).exitSpans))
		assert.NotNil(t, &childSpan, item.trace.exitSpans[0])
		assert.Equal(t, ctx, *item.ctx)
		assert.Equal(t, 0.7, item.latency)
		_, found := rootSpan.Attributes().Get(AssertsRequestContextAttribute)
		assert.True(t, found)
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
		spanMatcher: &spanMatcher{
			spanAttrMatchers: []*spanAttrMatcher{
				{
					attrName: "http.url",
					regex:    compile,
				},
			},
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
	childSpan.SetKind(ptrace.SpanKindInternal)
	childSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 9})
	childSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	childSpan.SetStartTimestamp(1e9 + 2e8)
	childSpan.SetEndTimestamp(1e9 + 3e8)

	traceById := map[string]*traceStruct{}
	traceById[rootSpan.TraceID().String()] = &traceStruct{
		rootSpan:      &rootSpan,
		internalSpans: []*ptrace.Span{&childSpan},
	}

	s.sampleTraces(ctx, &resourceTraces{
		namespace: "platform", service: "api-server",
		traceById: &traceById,
	})

	s.topTracesByService.Range(func(key any, value any) bool {
		stringKey := key.(string)
		serviceQueue := *value.(*serviceQueues)
		assert.Equal(t, "{env=dev, namespace=platform, site=us-west-2}#Service#api-server", stringKey)
		assert.Equal(t, 1, serviceQueue.requestCount)
		assert.NotNil(t, serviceQueue.getRequestState("/api-server/v4/rules"))
		assert.Equal(t, 1, serviceQueue.getRequestState("/api-server/v4/rules").slowTraceCount())
		assert.Equal(t, 0, serviceQueue.getRequestState("/api-server/v4/rules").errorTraceCount())
		item := *serviceQueue.getRequestState("/api-server/v4/rules").slowQueue.priorityQueue[0]
		assert.NotNil(t, item.trace)
		assert.Equal(t, &rootSpan, item.trace.rootSpan)
		assert.Equal(t, 1, len((*item.trace).internalSpans))
		assert.NotNil(t, &childSpan, item.trace.internalSpans[0])
		assert.Equal(t, ctx, *item.ctx)
		assert.Equal(t, 0.4, item.latency)
		_, found := rootSpan.Attributes().Get(AssertsRequestContextAttribute)
		assert.True(t, found)
		return true
	})
}

func TestWithNoRootTrace(t *testing.T) {
	var s = sampler{
		topTracesByService: &sync.Map{},
	}
	ctx := context.Background()
	traceById := map[string]*traceStruct{}
	traceById["invalid"] = &traceStruct{}

	s.sampleTraces(ctx, &resourceTraces{
		namespace: "platform", service: "api-server",
		traceById: &traceById,
	})

	s.topTracesByService.Range(func(key any, value any) bool {
		assert.Fail(t, "topTracesByService map should be empty")
		return true
	})
}

func TestTraceCardinalityLimit(t *testing.T) {
	cache := sync.Map{}
	compile, err := regexp.Compile("https?://.+?(/.+)")
	assert.Nil(t, err)
	var s = sampler{
		logger:             logger,
		config:             &config,
		thresholdHelper:    &th,
		topTracesByService: &cache,
		spanMatcher: &spanMatcher{
			spanAttrMatchers: []*spanAttrMatcher{
				{
					attrName: "http.url",
					regex:    compile,
				},
			},
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
	rootSpan.SetStartTimestamp(1e9)
	rootSpan.SetEndTimestamp(1e9 + 7e8)

	traceById := map[string]*traceStruct{}
	traceById[rootSpan.TraceID().String()] = &traceStruct{
		rootSpan: &rootSpan,
	}

	rootSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v1/rules")
	s.sampleTraces(ctx, &resourceTraces{
		namespace: "platform", service: "api-server",
		traceById: &traceById,
	})
	value, _ := s.topTracesByService.Load("{env=dev, namespace=platform, site=us-west-2}#Service#api-server")
	serviceQueue := value.(*serviceQueues)
	assert.Equal(t, 1, serviceQueue.requestCount)

	rootSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v2/rules")
	s.sampleTraces(ctx, &resourceTraces{
		namespace: "platform", service: "api-server",
		traceById: &traceById,
	})
	assert.Equal(t, 2, serviceQueue.requestCount)

	rootSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v3/rules")
	s.sampleTraces(ctx, &resourceTraces{
		namespace: "platform", service: "api-server",
		traceById: &traceById,
	})
	assert.Equal(t, 2, serviceQueue.requestCount)
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
		spanMatcher: &spanMatcher{
			spanAttrMatchers: []*spanAttrMatcher{
				{
					attrName: "http.url",
					regex:    compile,
				},
			},
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
	latencySpan.SetName("LatencySpan")
	latencySpan.SetTraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 8})
	latencySpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	latencySpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	latencySpan.SetStartTimestamp(1e9)
	latencySpan.SetEndTimestamp(1e9 + 6e8)

	traceById := map[string]*traceStruct{}
	traceById[latencySpan.TraceID().String()] = &traceStruct{
		resourceSpan: &resourceSpans,
		rootSpan:     &latencySpan,
	}

	errorTrace := ptrace.NewTraces()
	resourceSpans = errorTrace.ResourceSpans().AppendEmpty()
	attributes = resourceSpans.Resource().Attributes()
	attributes.PutStr(conventions.AttributeServiceName, "api-server")
	attributes.PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans = resourceSpans.ScopeSpans().AppendEmpty()

	errorSpan := scopeSpans.Spans().AppendEmpty()
	errorSpan.SetName("ErrorSpan")
	errorSpan.SetTraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 7})
	errorSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 9})
	errorSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	errorSpan.Attributes().PutBool("error", true)
	errorSpan.SetStartTimestamp(1e9)
	errorSpan.SetEndTimestamp(1e9 + 3e8)

	traceById[errorSpan.TraceID().String()] = &traceStruct{
		resourceSpan: &resourceSpans,
		rootSpan:     &errorSpan,
	}

	normalTrace := ptrace.NewTraces()
	resourceSpans = normalTrace.ResourceSpans().AppendEmpty()
	attributes = resourceSpans.Resource().Attributes()
	attributes.PutStr(conventions.AttributeServiceName, "api-server")
	attributes.PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans = resourceSpans.ScopeSpans().AppendEmpty()

	normalSpan := scopeSpans.Spans().AppendEmpty()
	normalSpan.SetName("NormalSpan")
	normalSpan.SetTraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 6})
	normalSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 10})
	normalSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	normalSpan.SetStartTimestamp(1e9)
	normalSpan.SetEndTimestamp(1e9 + 3e8)

	traceById[normalSpan.TraceID().String()] = &traceStruct{
		resourceSpan: &resourceSpans,
		rootSpan:     &normalSpan,
	}

	s.sampleTraces(ctx, &resourceTraces{
		namespace: "platform", service: "api-server",
		traceById: &traceById,
	})

	// Check there is one entry for the service
	serviceNames := make([]string, 0)
	requests := make([]string, 0)
	s.topTracesByService.Range(func(key any, value any) bool {
		serviceNames = append(serviceNames, key.(string))
		value.(*serviceQueues).requestStates.Range(func(key any, value any) bool {
			requests = append(requests, key.(string))
			return true
		})
		return true
	})
	assert.Equal(t, []string{"{env=dev, namespace=platform, site=us-west-2}#Service#api-server"}, serviceNames)
	assert.Equal(t, []string{"/api-server/v4/rules"}, requests)

	serviceNames = make([]string, 0)
	go func() { s.startTraceFlusher() }()
	time.Sleep(2 * time.Second)
	s.topTracesByService.Range(func(key any, value any) bool {
		stringKey := key.(string)
		serviceNames = append(serviceNames, key.(string))
		serviceQueue := *value.(*serviceQueues)
		assert.Equal(t, "{env=dev, namespace=platform, site=us-west-2}#Service#api-server", stringKey)
		assert.Equal(t, 0, serviceQueue.requestCount)
		assert.Equal(t, fmt.Sprint(&sync.Map{}), fmt.Sprint(serviceQueue.requestStates))
		assert.NotEqual(t, fmt.Sprint(&sync.Map{}), fmt.Sprint(serviceQueue.periodicSamplingStates))
		return true
	})
	assert.Equal(t, []string{"{env=dev, namespace=platform, site=us-west-2}#Service#api-server"}, serviceNames)
	assert.Equal(t, []string{"/api-server/v4/rules"}, requests)
	s.stopProcessing()
	time.Sleep(1 * time.Second)
}
