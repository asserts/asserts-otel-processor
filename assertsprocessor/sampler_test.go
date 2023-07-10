package assertsprocessor

import (
	"context"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
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
	Env:                        "dev",
	Site:                       "us-west-2",
	AssertsServer:              &map[string]string{"endpoint": "http://localhost:8030"},
	DefaultLatencyThreshold:    0.5,
	LimitPerService:            2,
	LimitPerRequestPerService:  5,
	PrometheusExporterPort:     9466,
	TraceFlushFrequencySeconds: 30,
}

var th = thresholdHelper{
	logger:     logger,
	config:     &config,
	entityKeys: &sync.Map{},
	thresholds: &sync.Map{},
	rwMutex:    &sync.RWMutex{},
}

func TestSpanIsSlowTrue(t *testing.T) {
	var s = sampler{
		logger:          logger,
		config:          &config,
		thresholdHelper: &th,
		metrics:         buildMetrics(),
	}

	testSpan := ptrace.NewSpan()
	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(1e9 + 6e8)

	ts := &traceSegment{
		rootSpan: &testSpan,
	}
	s.updateTrace("platform", "api-server", ts)

	assert.True(t, s.spanIsSlow(&testSpan, ts))
}

func TestSpanIsSlowFalse(t *testing.T) {
	var s = sampler{
		logger:          logger,
		config:          &config,
		thresholdHelper: &th,
		metrics:         buildMetrics(),
	}

	testSpan := ptrace.NewSpan()
	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(1e9 + 4e8)

	ts := &traceSegment{
		rootSpan: &testSpan,
	}
	s.updateTrace("platform", "api-server", ts)

	assert.False(t, s.spanIsSlow(&testSpan, ts))
}

func TestSampleTraceWithErrorSpan(t *testing.T) {
	cache := sync.Map{}
	var s = sampler{
		logger:             logger,
		config:             &config,
		thresholdHelper:    &th,
		topTracesByService: &cache,
		metrics:            buildMetrics(),
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
	rootSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/api-server/v4/rules")
	rootSpan.SetStartTimestamp(1e9)
	rootSpan.SetEndTimestamp(1e9 + 1e8)

	childSpan := scopeSpans.Spans().AppendEmpty()
	childSpan.SetParentSpanID(rootSpan.SpanID())
	childSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 9})
	childSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/payment/pay/:id")
	childSpan.SetKind(ptrace.SpanKindClient)
	childSpan.Status().SetCode(ptrace.StatusCodeError)
	childSpan.SetStartTimestamp(1e9 + 1e8)
	childSpan.SetEndTimestamp(1e9 + 5e8)

	tr := newTrace(
		&traceSegment{
			namespace: "platform",
			service:   "api-server",
			rootSpan:  &rootSpan,
			exitSpans: []*ptrace.Span{&childSpan},
		},
	)

	s.sampleTraces(ctx, []*trace{tr})

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
		assert.Equal(t, 1, len((*item.trace).segments))
		assert.Equal(t, &rootSpan, item.trace.segments[0].rootSpan)
		assert.Equal(t, 1, len((*item.trace).segments[0].exitSpans))
		assert.NotNil(t, &childSpan, item.trace.segments[0].exitSpans[0])
		assert.Equal(t, ctx, *item.ctx)
		assert.Equal(t, 0.1, item.latency)
		sampleType, _ := childSpan.Attributes().Get(AssertsTraceSampleTypeAttribute)
		assert.Equal(t, AssertsTraceSampleTypeError, sampleType.Str())
		return true
	})
}

func TestSampleTraceWithIgnorableClientErrorSpan(t *testing.T) {
	config.IgnoreClientErrors = true
	cache := sync.Map{}
	var s = sampler{
		logger:             logger,
		config:             &config,
		thresholdHelper:    &th,
		topTracesByService: &cache,
		metrics:            buildMetrics(),
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
	rootSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/api-server/v4/rules")
	rootSpan.SetStartTimestamp(1e9)
	rootSpan.SetEndTimestamp(1e9 + 1e8)

	childSpan := scopeSpans.Spans().AppendEmpty()
	childSpan.SetParentSpanID(rootSpan.SpanID())
	childSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 9})
	childSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/payment/pay/:id")
	childSpan.Attributes().PutStr(AssertsErrorTypeAttribute, "client_errors")
	childSpan.SetKind(ptrace.SpanKindClient)
	childSpan.Status().SetCode(ptrace.StatusCodeError)
	childSpan.SetStartTimestamp(1e9 + 1e8)
	childSpan.SetEndTimestamp(1e9 + 5e8)

	tr := newTrace(
		&traceSegment{
			namespace: "platform",
			service:   "api-server",
			rootSpan:  &rootSpan,
			exitSpans: []*ptrace.Span{&childSpan},
		},
	)

	s.sampleTraces(ctx, []*trace{tr})

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
		assert.Equal(t, 1, len((*item.trace).segments))
		assert.Equal(t, &rootSpan, item.trace.segments[0].rootSpan)
		assert.Equal(t, 1, len((*item.trace).segments[0].exitSpans))
		assert.NotNil(t, &childSpan, item.trace.segments[0].exitSpans[0])
		assert.Equal(t, ctx, *item.ctx)
		assert.Equal(t, 0.1, item.latency)
		sampleType, _ := rootSpan.Attributes().Get(AssertsTraceSampleTypeAttribute)
		assert.Equal(t, AssertsTraceSampleTypeNormal, sampleType.Str())
		return true
	})
}

func TestSampleTraceWithSlowSpan(t *testing.T) {
	cache := sync.Map{}
	s := sampler{
		logger:             logger,
		config:             &config,
		thresholdHelper:    &th,
		topTracesByService: &cache,
		metrics:            buildMetrics(),
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
	rootSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/api-server/v4/rules")
	rootSpan.SetStartTimestamp(1e9)
	rootSpan.SetEndTimestamp(1e9 + 7e8)

	childSpan := scopeSpans.Spans().AppendEmpty()
	childSpan.SetTraceID(rootSpan.TraceID())
	childSpan.SetParentSpanID(rootSpan.SpanID())
	childSpan.SetKind(ptrace.SpanKindClient)
	childSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 9})
	childSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/payment/pay/:id")
	childSpan.SetStartTimestamp(1e9 + 1e8)
	childSpan.SetEndTimestamp(1e9 + 5e8)

	tr := newTrace(
		&traceSegment{
			namespace: "platform",
			service:   "api-server",
			rootSpan:  &rootSpan,
			exitSpans: []*ptrace.Span{&childSpan},
		},
	)

	s.sampleTraces(ctx, []*trace{tr})

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
		assert.Equal(t, 1, len((*item.trace).segments))
		assert.Equal(t, &rootSpan, item.trace.segments[0].rootSpan)
		assert.Equal(t, 1, len((*item.trace).segments[0].exitSpans))
		assert.NotNil(t, &childSpan, item.trace.segments[0].exitSpans[0])
		assert.Equal(t, ctx, *item.ctx)
		assert.Equal(t, 0.7, item.latency)
		sampleType, _ := rootSpan.Attributes().Get(AssertsTraceSampleTypeAttribute)
		assert.Equal(t, AssertsTraceSampleTypeSlow, sampleType.Str())
		return true
	})
}

func TestSampleTraceWithTwoSegments(t *testing.T) {
	cache := sync.Map{}
	s := sampler{
		logger:             logger,
		config:             &config,
		thresholdHelper:    &th,
		topTracesByService: &cache,
		metrics:            buildMetrics(),
	}

	ctx := context.Background()
	testTrace := ptrace.NewTraces()
	paymentResourceSpans := testTrace.ResourceSpans().AppendEmpty()
	paymentAttributes := paymentResourceSpans.Resource().Attributes()
	paymentAttributes.PutStr(conventions.AttributeServiceName, "payment")
	paymentAttributes.PutStr(conventions.AttributeServiceNamespace, "robot-shop")
	paymentScopeSpans := paymentResourceSpans.ScopeSpans().AppendEmpty()

	paymentRootSpan := paymentScopeSpans.Spans().AppendEmpty()
	paymentRootSpan.SetTraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 8})
	paymentRootSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	paymentRootSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/payment/pay/<id>")
	paymentRootSpan.SetStartTimestamp(1e9)
	paymentRootSpan.SetEndTimestamp(1e9 + 7e8)

	paymentExitSpan := paymentScopeSpans.Spans().AppendEmpty()
	paymentExitSpan.SetTraceID(paymentRootSpan.TraceID())
	paymentExitSpan.SetParentSpanID(paymentRootSpan.SpanID())
	paymentExitSpan.SetKind(ptrace.SpanKindClient)
	paymentExitSpan.Status().SetCode(ptrace.StatusCodeError)
	paymentExitSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 9})
	paymentExitSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/payment/cart/<id>")
	paymentExitSpan.SetStartTimestamp(1e9 + 7e8)
	paymentExitSpan.SetEndTimestamp(1e9 + 8e8)

	cartResourceSpans := testTrace.ResourceSpans().AppendEmpty()
	cartAttributes := cartResourceSpans.Resource().Attributes()
	cartAttributes.PutStr(conventions.AttributeServiceName, "cart")
	cartAttributes.PutStr(conventions.AttributeServiceNamespace, "robot-shop")
	cartScopeSpans := cartResourceSpans.ScopeSpans().AppendEmpty()

	cartEntrySpan := cartScopeSpans.Spans().AppendEmpty()
	cartEntrySpan.SetTraceID(paymentRootSpan.TraceID())
	cartEntrySpan.SetParentSpanID(paymentExitSpan.SpanID())
	cartEntrySpan.SetKind(ptrace.SpanKindClient)
	cartEntrySpan.Status().SetCode(ptrace.StatusCodeError)
	cartEntrySpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 0})
	cartEntrySpan.Attributes().PutStr(AssertsRequestContextAttribute, "/cart/delete/:id")
	cartEntrySpan.SetStartTimestamp(1e9 + 8e8)
	cartEntrySpan.SetEndTimestamp(1e9 + 9e8)

	tr := newTrace(
		&traceSegment{
			namespace: "robot-shop",
			service:   "payment",
			rootSpan:  &paymentRootSpan,
			exitSpans: []*ptrace.Span{&paymentExitSpan},
		},
		&traceSegment{
			namespace:  "robot-shop",
			service:    "cart",
			entrySpans: []*ptrace.Span{&cartEntrySpan},
		},
	)

	s.sampleTraces(ctx, []*trace{tr})

	{
		value, _ := s.topTracesByService.Load("{env=dev, namespace=robot-shop, site=us-west-2}#Service#payment")
		serviceQueue := *value.(*serviceQueues)
		assert.Equal(t, 1, serviceQueue.requestCount)
		assert.NotNil(t, serviceQueue.getRequestState("/payment/pay/<id>"))
		assert.Equal(t, 1, serviceQueue.getRequestState("/payment/pay/<id>").slowTraceCount())
		assert.Equal(t, 0, serviceQueue.getRequestState("/payment/pay/<id>").errorTraceCount())
		item := *serviceQueue.getRequestState("/payment/pay/<id>").slowQueue.priorityQueue[0]
		assert.NotNil(t, item.trace)
		assert.Equal(t, 2, len((*item.trace).segments))
		assert.Equal(t, &paymentRootSpan, item.trace.segments[0].rootSpan)
		assert.Equal(t, 1, len((*item.trace).segments[0].exitSpans))
		assert.NotNil(t, &paymentExitSpan, item.trace.segments[0].exitSpans[0])
		assert.Equal(t, 1, len((*item.trace).segments[1].entrySpans))
		assert.NotNil(t, &cartEntrySpan, item.trace.segments[1].entrySpans[0])
		assert.Equal(t, ctx, *item.ctx)
		assert.Equal(t, 0.7, item.latency)
		sampleType, _ := paymentRootSpan.Attributes().Get(AssertsTraceSampleTypeAttribute)
		assert.Equal(t, AssertsTraceSampleTypeSlow, sampleType.Str())
		sampleType, _ = paymentExitSpan.Attributes().Get(AssertsTraceSampleTypeAttribute)
		assert.Equal(t, AssertsTraceSampleTypeError, sampleType.Str())
	}

	{
		value, _ := s.topTracesByService.Load("{env=dev, namespace=robot-shop, site=us-west-2}#Service#cart")
		serviceQueue := *value.(*serviceQueues)
		assert.Equal(t, 1, serviceQueue.requestCount)
		assert.NotNil(t, serviceQueue.getRequestState("/cart/delete/:id"))
		assert.Equal(t, 0, serviceQueue.getRequestState("/cart/delete/:id").slowTraceCount())
		assert.Equal(t, 0, serviceQueue.getRequestState("/cart/delete/:id").errorTraceCount())
		sampleType, _ := cartEntrySpan.Attributes().Get(AssertsTraceSampleTypeAttribute)
		assert.Equal(t, AssertsTraceSampleTypeError, sampleType.Str())
	}
}

func TestSampleNormalTrace(t *testing.T) {
	cache := sync.Map{}
	var s = sampler{
		logger:             logger,
		config:             &config,
		thresholdHelper:    &th,
		topTracesByService: &cache,
		metrics:            buildMetrics(),
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
	rootSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/api-server/v4/rules")
	rootSpan.SetStartTimestamp(1e9)
	rootSpan.SetEndTimestamp(1e9 + 4e8)

	childSpan := scopeSpans.Spans().AppendEmpty()
	childSpan.SetParentSpanID(rootSpan.SpanID())
	childSpan.SetKind(ptrace.SpanKindInternal)
	childSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 9})
	childSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/api-server/v4/rules")
	childSpan.SetStartTimestamp(1e9 + 2e8)
	childSpan.SetEndTimestamp(1e9 + 3e8)

	tr := newTrace(
		&traceSegment{
			namespace:     "platform",
			service:       "api-server",
			rootSpan:      &rootSpan,
			internalSpans: []*ptrace.Span{&childSpan},
		},
	)

	s.sampleTraces(ctx, []*trace{tr})

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
		assert.Equal(t, 1, len((*item.trace).segments))
		assert.Equal(t, &rootSpan, item.trace.segments[0].rootSpan)
		assert.Equal(t, 1, len((*item.trace).segments[0].internalSpans))
		assert.NotNil(t, &childSpan, item.trace.segments[0].internalSpans[0])
		assert.Equal(t, ctx, *item.ctx)
		assert.Equal(t, 0.4, item.latency)
		_, found := rootSpan.Attributes().Get(AssertsRequestContextAttribute)
		assert.True(t, found)
		return true
	})
}

func TestWithNoRootTrace(t *testing.T) {
	var s = sampler{
		logger:             logger,
		config:             &config,
		topTracesByService: &sync.Map{},
		metrics:            buildMetrics(),
	}
	ctx := context.Background()
	tr := newTrace(
		&traceSegment{},
	)

	s.sampleTraces(ctx, []*trace{tr})

	s.topTracesByService.Range(func(key any, value any) bool {
		assert.Fail(t, "topTracesByService map should be empty")
		return true
	})
}

func TestTraceCardinalityLimit(t *testing.T) {
	cache := sync.Map{}
	var s = sampler{
		logger:             logger,
		config:             &config,
		thresholdHelper:    &th,
		topTracesByService: &cache,
		metrics:            buildMetrics(),
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

	tr := newTrace(
		&traceSegment{
			namespace: "platform",
			service:   "api-server",
			rootSpan:  &rootSpan,
		},
	)

	rootSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/api-server/v1/rules")
	s.sampleTraces(ctx, []*trace{tr})
	value, _ := s.topTracesByService.Load("{env=dev, namespace=platform, site=us-west-2}#Service#api-server")
	serviceQueue := value.(*serviceQueues)
	assert.Equal(t, 1, serviceQueue.requestCount)

	rootSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/api-server/v2/rules")
	s.sampleTraces(ctx, []*trace{tr})
	assert.Equal(t, 2, serviceQueue.requestCount)

	rootSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/api-server/v3/rules")
	s.sampleTraces(ctx, []*trace{tr})
	assert.Equal(t, 2, serviceQueue.requestCount)
}

func TestFlushTraces(t *testing.T) {
	cache := sync.Map{}
	ctx := context.Background()
	dConsumer := dummyConsumer{
		items: make([]*Item, 0),
	}
	var s = sampler{
		logger:             logger,
		config:             &config,
		thresholdHelper:    &th,
		topTracesByService: &cache,
		traceFlushTicker:   clock.FromContext(ctx).NewTicker(time.Millisecond),
		nextConsumer:       dConsumer,
		stop:               make(chan bool, 5),
		metrics:            buildMetrics(),
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
	latencySpan.Attributes().PutStr(AssertsRequestContextAttribute, "/api-server/v4/rules")
	latencySpan.SetStartTimestamp(1e9)
	latencySpan.SetEndTimestamp(1e9 + 6e8)

	tr1 := newTrace(
		&traceSegment{
			namespace:     "platform",
			service:       "api-server",
			resourceSpans: &resourceSpans,
			rootSpan:      &latencySpan,
		},
	)

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
	errorSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/api-server/v4/rules")
	errorSpan.Attributes().PutBool("error", true)
	errorSpan.Status().SetCode(ptrace.StatusCodeError)
	errorSpan.SetStartTimestamp(1e9)
	errorSpan.SetEndTimestamp(1e9 + 3e8)

	tr2 := newTrace(
		&traceSegment{
			namespace:     "platform",
			service:       "api-server",
			resourceSpans: &resourceSpans,
			rootSpan:      &errorSpan,
		},
	)

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
	normalSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/api-server/v4/rules")
	normalSpan.SetStartTimestamp(1e9)
	normalSpan.SetEndTimestamp(1e9 + 3e8)

	tr3 := newTrace(
		&traceSegment{
			namespace:     "platform",
			service:       "api-server",
			resourceSpans: &resourceSpans,
			rootSpan:      &normalSpan,
		},
	)

	s.sampleTraces(ctx, []*trace{tr1, tr2, tr3})

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
	time.Sleep(2 * time.Millisecond)
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
	time.Sleep(1 * time.Millisecond)
}

func buildMetrics() *metrics {
	reg := &metrics{
		config: &config,
	}
	reg.sampledTraceCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "asserts",
		Subsystem: "trace",
		Name:      "sampled_count_total",
	}, []string{envLabel, siteLabel, traceSampleTypeLabel})

	reg.totalTraceCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "asserts",
		Subsystem: "trace",
		Name:      "count_total",
	}, []string{envLabel, siteLabel})

	reg.totalSpanCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "asserts",
		Subsystem: "spans",
		Name:      "count_total",
	}, []string{envLabel, siteLabel, namespaceLabel, serviceLabel})

	reg.sampledSpanCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "asserts",
		Subsystem: "spans",
		Name:      "sampled_count_total",
	}, []string{envLabel, siteLabel, namespaceLabel, serviceLabel})
	return reg
}
