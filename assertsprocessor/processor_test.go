package assertsprocessor

import (
	"context"
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
	Env:            "dev",
	Site:           "us-west-2",
	AssertsServer:  &map[string]string{"endpoint": "http://localhost:8030"},
	CaptureMetrics: true,
	RequestContextExps: map[string][]*MatcherDto{
		"default": {
			{
				AttrName: "attribute",
				Regex:    ".+",
			},
		},
	},
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
		entityKeys:          &sync.Map{},
		thresholds:          &sync.Map{},
		thresholdSyncTicker: clock.FromContext(ctx).NewTicker(time.Minute),
		rc:                  &assertsClient{},
	}
	configRefresh := configRefresh{
		config:           &testConfig,
		logger:           logger,
		configSyncTicker: clock.FromContext(ctx).NewTicker(time.Minute),
		stop:             make(chan bool),
		restClient:       &assertsClient{},
		spanMatcher:      &spanMatcher{},
	}
	p := assertsProcessorImpl{
		logger:        testLogger,
		config:        &testConfig,
		nextConsumer:  dConsumer,
		metricBuilder: newMetricHelper(testLogger, &testConfig, &spanMatcher{}),
		sampler: &sampler{
			logger:             testLogger,
			config:             &testConfig,
			nextConsumer:       dConsumer,
			topTracesByService: &sync.Map{},
			stop:               make(chan bool),
			traceFlushTicker:   clock.FromContext(ctx).NewTicker(time.Minute),
			thresholdHelper:    &_th,
			spanMatcher:        &spanMatcher{},
		},
		configRefresh: &configRefresh,
	}
	assert.Nil(t, p.Start(ctx, nil))
	assert.Nil(t, p.Shutdown(ctx))
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
		entityKeys:          &sync.Map{},
		thresholds:          &sync.Map{},
		thresholdSyncTicker: clock.FromContext(ctx).NewTicker(time.Minute),
		rwMutex:             &sync.RWMutex{},
	}
	helper := newMetricHelper(testLogger, &testConfig, &spanMatcher{})
	_ = helper.registerMetrics()
	p := assertsProcessorImpl{
		logger:        testLogger,
		config:        &testConfig,
		nextConsumer:  dConsumer,
		metricBuilder: helper,
		sampler: &sampler{
			logger:             testLogger,
			config:             &testConfig,
			nextConsumer:       dConsumer,
			topTracesByService: &sync.Map{},
			stop:               make(chan bool),
			traceFlushTicker:   clock.FromContext(ctx).NewTicker(time.Minute),
			thresholdHelper:    &_th,
			spanMatcher:        &spanMatcher{},
			metricHelper:       buildMetricHelper(),
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

	nestedSpan := scopeSpans.Spans().AppendEmpty()
	nestedSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 9})
	nestedSpan.SetParentSpanID(rootSpan.SpanID())
	nestedSpan.SetKind(ptrace.SpanKindClient)
	nestedSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	nestedSpan.Attributes().PutBool("error", true)
	nestedSpan.SetStartTimestamp(1e9)
	nestedSpan.SetEndTimestamp(1e9 + 4e8)

	err := p.ConsumeTraces(ctx, testTrace)
	assert.Nil(t, err)
}

func TestProcessorIsUpdated(t *testing.T) {
	prevConfig := &Config{
		CaptureMetrics: false,
	}
	currentConfig := &Config{
		CaptureMetrics: true,
	}

	testLogger, _ := zap.NewProduction()
	p := assertsProcessorImpl{
		logger:  testLogger,
		config:  prevConfig,
		rwMutex: &sync.RWMutex{},
	}

	assert.False(t, p.isUpdated(prevConfig, prevConfig))
	assert.True(t, p.isUpdated(prevConfig, currentConfig))
}

func TestProcessorOnUpdate(t *testing.T) {
	prevConfig := &Config{
		CaptureMetrics: false,
	}
	currentConfig := &Config{
		CaptureMetrics: true,
	}

	testLogger, _ := zap.NewProduction()
	p := assertsProcessorImpl{
		logger:  testLogger,
		config:  prevConfig,
		rwMutex: &sync.RWMutex{},
	}

	assert.False(t, p.captureMetrics())
	err := p.onUpdate(currentConfig)
	assert.Nil(t, err)
	assert.True(t, p.captureMetrics())
}
