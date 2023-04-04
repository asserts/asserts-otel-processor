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
	RequestContextExps: &[]*MatcherDto{{
		AttrName:    "attribute",
		Regex:       ".+",
		Replacement: "$1",
	}},
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
