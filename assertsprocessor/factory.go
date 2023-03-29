package assertsprocessor

import (
	"context"
	"github.com/jellydator/ttlcache/v3"
	"github.com/puzpuzpuz/xsync/v2"
	"sync"
	"time"

	"github.com/tilinna/clock"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
	"go.uber.org/zap"
)

const (
	// The value of "type" key in configuration.
	typeStr = "assertsprocessor"
	// The stability level of the processor.
	stability = component.StabilityLevelDevelopment
)

// NewFactory creates a factory for the assertsotelprocessor processor.
func NewFactory() processor.Factory {
	return processor.NewFactory(
		typeStr,
		createDefaultConfig,
		processor.WithTraces(createTracesProcessor, stability),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		AssertsServer: &map[string]string{
			"endpoint": "https://chief.app.dev.asserts.ai",
		},
		Env:                            "dev",
		Site:                           "us-west-2",
		DefaultLatencyThreshold:        3,
		LimitPerService:                100,
		LimitPerRequestPerService:      3,
		RequestContextCacheTTL:         60,
		NormalSamplingFrequencyMinutes: 5,
		PrometheusExporterPort:         9465,
		TraceFlushFrequencySeconds:     30,
	}
}

func createTracesProcessor(ctx context.Context, params processor.CreateSettings, cfg component.Config, nextConsumer consumer.Traces) (processor.Traces, error) {
	return newProcessor(params.Logger, ctx, cfg, nextConsumer)
}

func newProcessor(logger *zap.Logger, ctx context.Context, config component.Config, nextConsumer consumer.Traces) (*assertsProcessorImpl, error) {
	logger.Info("Creating assertsotelprocessor")
	pConfig := config.(*Config)

	spanMatcher := &spanMatcher{}
	err := spanMatcher.compileRequestContextRegexps(logger, pConfig)
	if err != nil {
		return nil, err
	}

	thresholdsHelper := thresholdHelper{
		config:              pConfig,
		logger:              logger,
		thresholdSyncTicker: clock.FromContext(ctx).NewTicker(time.Minute),
		thresholds:          &sync.Map{},
		entityKeys:          &sync.Map{},
		stop:                make(chan bool),
	}

	metricsHelper := metricHelper{
		logger:                   logger,
		config:                   pConfig,
		spanMatcher:              spanMatcher,
		requestContextsByService: xsync.NewMapOf[*ttlcache.Cache[string, string]](),
	}
	err = metricsHelper.buildHistogram()

	if err != nil {
		return nil, err
	}

	traceSampler := sampler{
		logger:             logger,
		config:             pConfig,
		thresholdHelper:    &thresholdsHelper,
		topTracesByService: &sync.Map{},
		traceFlushTicker:   clock.FromContext(ctx).NewTicker(time.Duration(pConfig.TraceFlushFrequencySeconds) * time.Second),
		nextConsumer:       nextConsumer,
		spanMatcher:        spanMatcher,
		stop:               make(chan bool),
	}

	p := &assertsProcessorImpl{
		logger:        logger,
		config:        pConfig,
		nextConsumer:  nextConsumer,
		metricBuilder: &metricsHelper,
		sampler:       &traceSampler,
	}

	go metricsHelper.startExporter()
	return p, nil
}
