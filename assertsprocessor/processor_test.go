package assertsprocessor

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/tilinna/clock"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
	"go.uber.org/zap"
	"regexp"
	"sync"
	"testing"
	"time"
)

var testConfig = Config{
	Env:           "dev",
	Site:          "us-west-2",
	AssertsServer: &map[string]string{"endpoint": "http://localhost:8030"},
	AttributeExps: &map[string]string{
		"attribute": ".+",
	},
	RequestContextExps: &map[string]string{
		"attribute": ".+",
	},
	CaptureAttributesInMetric:      []string{"attribute"},
	DefaultLatencyThreshold:        0.5,
	MaxTracesPerMinute:             100,
	MaxTracesPerMinutePerContainer: 5,
	NormalSamplingFrequencyMinutes: 5,
}

func TestStart(t *testing.T) {
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
		logger:       testLogger,
		config:       &testConfig,
		nextConsumer: dConsumer,
		metricBuilder: &metricHelper{
			logger:                testLogger,
			config:                &testConfig,
			attributeValueRegExps: &map[string]*regexp.Regexp{},
		},
		thresholdsHelper: &_th,
		sampler: &sampler{
			logger:               testLogger,
			config:               &testConfig,
			nextConsumer:         dConsumer,
			topTracesMap:         &sync.Map{},
			healthySamplingState: &sync.Map{},
			stop:                 make(chan bool),
			traceFlushTicker:     clock.FromContext(ctx).NewTicker(time.Minute),
			thresholdHelper:      &_th,
			requestRegexps:       &map[string]*regexp.Regexp{},
		},
	}
	assert.Nil(t, p.Start(ctx, nil))
}

//func TestShutdown(t *testing.T) {
//	ctx := context.Background()
//	dConsumer := dummyConsumer{
//		items: make([]*Item, 0),
//	}
//	testLogger, _ := zap.NewProduction()
//	_th := thresholdHelper{
//		logger:              testLogger,
//		config:              &testConfig,
//		stop:                make(chan bool),
//		entityKeys:          &sync.Map{},
//		thresholds:          &sync.Map{},
//		thresholdSyncTicker: clock.FromContext(ctx).NewTicker(time.Minute),
//	}
//	p := assertsProcessorImpl{
//		logger:       testLogger,
//		config:       &testConfig,
//		nextConsumer: dConsumer,
//		metricBuilder: &metricHelper{
//			logger:                testLogger,
//			config:                &testConfig,
//			attributeValueRegExps: &map[string]regexp.Regexp{},
//		},
//		thresholdsHelper: &_th,
//		sampler: &sampler{
//			logger:               testLogger,
//			config:               &testConfig,
//			nextConsumer:         dConsumer,
//			topTracesMap:         &sync.Map{},
//			healthySamplingState: &sync.Map{},
//			stop:                 make(chan bool),
//			traceFlushTicker:     clock.FromContext(ctx).NewTicker(time.Minute),
//			thresholdHelper:      &_th,
//			requestRegexps:       &map[string]regexp.Regexp{},
//		},
//	}
//}

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
		logger:       testLogger,
		config:       &testConfig,
		nextConsumer: dConsumer,
		metricBuilder: &metricHelper{
			logger:                testLogger,
			config:                &testConfig,
			attributeValueRegExps: &map[string]*regexp.Regexp{},
		},
		thresholdsHelper: &_th,
		sampler: &sampler{
			logger:               testLogger,
			config:               &testConfig,
			nextConsumer:         dConsumer,
			topTracesMap:         &sync.Map{},
			healthySamplingState: &sync.Map{},
			stop:                 make(chan bool),
			traceFlushTicker:     clock.FromContext(ctx).NewTicker(time.Minute),
			thresholdHelper:      &_th,
			requestRegexps:       &map[string]*regexp.Regexp{},
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
	nestedSpan.Attributes().PutStr("http.url", "https://localhost:8030/api-server/v4/rules")
	nestedSpan.Attributes().PutBool("error", true)
	nestedSpan.SetStartTimestamp(1e9)
	nestedSpan.SetEndTimestamp(1e9 + 4e8)

	err := p.ConsumeTraces(ctx, testTrace)
	assert.Nil(t, err)
}
