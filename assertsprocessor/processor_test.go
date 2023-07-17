package assertsprocessor

import (
	"context"
	"github.com/puzpuzpuz/xsync/v2"
	"go.opentelemetry.io/collector/consumer"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/tilinna/clock"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
	"go.uber.org/zap"
)

var testConfig = Config{
	Env:                            "dev",
	Site:                           "us-west-2",
	AssertsServer:                  &map[string]string{"endpoint": "http://localhost:8030"},
	CaptureMetrics:                 true,
	CaptureAttributesInMetric:      []string{"attribute"},
	DefaultLatencyThreshold:        0.5,
	LimitPerService:                100,
	LimitPerRequestPerService:      5,
	NormalSamplingFrequencyMinutes: 5,
}

func TestCapabilities(t *testing.T) {
	testLogger, _ := zap.NewProduction()
	p := assertsProcessorImpl{
		logger: testLogger,
	}
	assert.Equal(t, consumer.Capabilities{MutatesData: true}, p.Capabilities())
}

func TestStartAndShutdown(t *testing.T) {
	ctx := context.Background()
	dConsumer := dummyConsumer{
		items: make([]*Item, 0),
	}
	testLogger, _ := zap.NewProduction()
	_th := thresholdHelper{
		logger:              testLogger,
		config:              &testConfig,
		stop:                make(chan bool),
		entityKeys:          xsync.NewMapOf[EntityKeyDto](),
		thresholds:          xsync.NewMapOf[map[string]*ThresholdDto](),
		thresholdSyncTicker: clock.FromContext(ctx).NewTicker(time.Minute),
		rc:                  &assertsClient{},
	}
	configRefresh := configRefresh{
		config:           &testConfig,
		logger:           logger,
		configSyncTicker: clock.FromContext(ctx).NewTicker(time.Minute),
		stop:             make(chan bool),
		restClient:       &assertsClient{},
	}
	p := assertsProcessorImpl{
		logger:        testLogger,
		config:        &testConfig,
		nextConsumer:  dConsumer,
		metricBuilder: newMetricHelper(testLogger, &testConfig, buildInfo),
		sampler: &sampler{
			logger:             testLogger,
			config:             &testConfig,
			nextConsumer:       dConsumer,
			topTracesByService: &sync.Map{},
			stop:               make(chan bool),
			traceFlushTicker:   clock.FromContext(ctx).NewTicker(time.Minute),
			thresholdHelper:    &_th,
		},
		configRefresh: &configRefresh,
	}
	assert.Nil(t, p.Start(ctx, nil))
	assert.Nil(t, p.Shutdown(ctx))
}

type mockEnrichmentProcessor struct {
}

func (mEP *mockEnrichmentProcessor) enrichSpan(namespace string, service string, span *ptrace.Span) {
	span.Attributes().PutStr(AssertsRequestContextAttribute, "/mock-request-context")
}

func TestConsumeTraces(t *testing.T) {
	ctx := context.Background()
	dConsumer := dummyConsumer{
		items: make([]*Item, 0),
	}
	testLogger, _ := zap.NewProduction()
	_th := thresholdHelper{
		logger:              testLogger,
		config:              &testConfig,
		stop:                make(chan bool),
		entityKeys:          xsync.NewMapOf[EntityKeyDto](),
		thresholds:          xsync.NewMapOf[map[string]*ThresholdDto](),
		thresholdSyncTicker: clock.FromContext(ctx).NewTicker(time.Minute),
		rwMutex:             &sync.RWMutex{},
	}
	helper := newMetricHelper(testLogger, &testConfig, buildInfo)
	_ = helper.registerMetrics()
	p := assertsProcessorImpl{
		logger:        testLogger,
		config:        &testConfig,
		nextConsumer:  dConsumer,
		metricBuilder: helper,
		spanEnricher:  &mockEnrichmentProcessor{},
		sampler: &sampler{
			logger:             testLogger,
			config:             &testConfig,
			nextConsumer:       dConsumer,
			topTracesByService: &sync.Map{},
			stop:               make(chan bool),
			traceFlushTicker:   clock.FromContext(ctx).NewTicker(time.Minute),
			thresholdHelper:    &_th,
			metrics:            buildMetrics(),
		},
		rwMutex: &sync.RWMutex{},
	}

	testTrace := ptrace.NewTraces()
	resourceSpans := testTrace.ResourceSpans().AppendEmpty()
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceName, "api-server")
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()

	rootSpan := scopeSpans.Spans().AppendEmpty()
	rootSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	rootSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	rootSpan.SetStartTimestamp(1e9)
	rootSpan.SetEndTimestamp(1e9 + 4e8)

	internalSpan := scopeSpans.Spans().AppendEmpty()
	internalSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 7})
	internalSpan.SetParentSpanID(rootSpan.SpanID())
	internalSpan.SetKind(ptrace.SpanKindInternal)

	clientSpan := scopeSpans.Spans().AppendEmpty()
	clientSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 9})
	clientSpan.SetParentSpanID(rootSpan.SpanID())
	clientSpan.SetKind(ptrace.SpanKindClient)
	clientSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	clientSpan.Attributes().PutBool("error", true)
	clientSpan.SetStartTimestamp(1e9)
	clientSpan.SetEndTimestamp(1e9 + 4e8)

	err := p.ConsumeTraces(ctx, testTrace)
	assert.Nil(t, err)

	value, _ := rootSpan.Attributes().Get(AssertsRequestContextAttribute)
	assert.NotNil(t, value)
	assert.Equal(t, "/mock-request-context", value.Str())

	value, _ = clientSpan.Attributes().Get(AssertsRequestContextAttribute)
	assert.NotNil(t, value)
	assert.Equal(t, "/mock-request-context", value.Str())

	_, found := internalSpan.Attributes().Get(AssertsRequestContextAttribute)
	assert.False(t, found)
}

func TestProcessorIsUpdated(t *testing.T) {
	currConfig := &Config{
		CaptureMetrics: false,
	}
	newConfig := &Config{
		CaptureMetrics: true,
	}

	testLogger, _ := zap.NewProduction()
	p := assertsProcessorImpl{
		logger:  testLogger,
		config:  currConfig,
		rwMutex: &sync.RWMutex{},
	}

	assert.False(t, p.isUpdated(currConfig, currConfig))
	assert.True(t, p.isUpdated(currConfig, newConfig))
}

func TestProcessorOnUpdate(t *testing.T) {
	currConfig := &Config{
		CaptureMetrics: false,
	}
	newConfig := &Config{
		CaptureMetrics: true,
	}

	testLogger, _ := zap.NewProduction()
	p := assertsProcessorImpl{
		logger:  testLogger,
		config:  currConfig,
		rwMutex: &sync.RWMutex{},
	}

	assert.False(t, p.captureMetrics())
	err := p.onUpdate(newConfig)
	assert.Nil(t, err)
	assert.True(t, p.captureMetrics())
}
