package assertsprocessor

import (
	"context"
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

	spanMatcher := &spanMatcher{
		logger: logger,
	}
	err := spanMatcher.compileRequestContextRegexps(pConfig)
	if err != nil {
		return nil, err
	}

	assertsClient := assertsClient{
		config: pConfig,
		logger: logger,
	}

	thresholdsHelper := thresholdHelper{
		config:              pConfig,
		logger:              logger,
		thresholdSyncTicker: clock.FromContext(ctx).NewTicker(time.Minute),
		thresholds:          &sync.Map{},
		entityKeys:          &sync.Map{},
		stop:                make(chan bool),
		rc:                  &assertsClient,
		rwMutex:             &sync.RWMutex{},
	}

	metricsHelper := newMetricHelper(logger, pConfig, spanMatcher)
	err = metricsHelper.registerMetrics()
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
		metricHelper:       metricsHelper,
	}

	p := &assertsProcessorImpl{
		logger:        logger,
		config:        pConfig,
		nextConsumer:  nextConsumer,
		metricBuilder: metricsHelper,
		sampler:       &traceSampler,
		rwMutex:       &sync.RWMutex{},
	}

	listeners := make([]configListener, 0)
	listeners = append(listeners, spanMatcher)
	listeners = append(listeners, &thresholdsHelper)
	listeners = append(listeners, p)
	configRefresh := configRefresh{
		config:           pConfig,
		logger:           logger,
		configSyncTicker: clock.FromContext(ctx).NewTicker(time.Minute),
		stop:             make(chan bool),
		restClient:       &assertsClient,
		spanMatcher:      spanMatcher,
		configListeners:  listeners,
	}
	p.configRefresh = &configRefresh

	metricsHelper.startExporter()
	return p, nil
}
