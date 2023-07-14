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
		LatencyHistogramBuckets:        []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 90, 120},
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
	return newProcessor(params.Logger, params.BuildInfo, ctx, cfg, nextConsumer)
}

func newProcessor(logger *zap.Logger, buildInfo component.BuildInfo, ctx context.Context, config component.Config,
	nextConsumer consumer.Traces) (*assertsProcessorImpl, error) {

	logger.Info("Creating assertsotelprocessor")
	pConfig := config.(*Config)

	restClient := restClientFactory(logger, pConfig)

	configRefresh := configRefresh{
		config:           pConfig,
		logger:           logger,
		configSyncTicker: clock.FromContext(ctx).NewTicker(time.Minute),
		stop:             make(chan bool),
		restClient:       restClient,
	}

	// First up, fetch the latest collector config from asserts api server
	if newConfig, fetchError := configRefresh.fetchConfig(restClient); fetchError == nil {
		// If config is fetched successfully from api server then for the following
		// attributes it takes precedence over the local collector config
		pConfig.CaptureMetrics = newConfig.CaptureMetrics
		pConfig.CaptureAttributesInMetric = newConfig.CaptureAttributesInMetric
		pConfig.DefaultLatencyThreshold = newConfig.DefaultLatencyThreshold
		pConfig.CustomAttributeConfigs = newConfig.CustomAttributeConfigs
		pConfig.IgnoreClientErrors = newConfig.IgnoreClientErrors
		if len(newConfig.LatencyHistogramBuckets) > 0 {
			pConfig.LatencyHistogramBuckets = newConfig.LatencyHistogramBuckets
		}
	}

	_spanEnrichmentProcessor, err := buildEnrichmentProcessor(logger, pConfig)
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
		rc:                  restClient,
		rwMutex:             &sync.RWMutex{},
	}

	metricsHelper := newMetricHelper(logger, pConfig, buildInfo)
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
		stop:               make(chan bool),
		metrics:            metricsHelper.metrics,
		rwMutex:            &sync.RWMutex{},
	}

	p := &assertsProcessorImpl{
		logger:        logger,
		config:        pConfig,
		spanEnricher:  _spanEnrichmentProcessor,
		nextConsumer:  nextConsumer,
		metricBuilder: metricsHelper,
		sampler:       &traceSampler,
		rwMutex:       &sync.RWMutex{},
	}

	listeners := make([]configListener, 0)
	listeners = append(listeners, _spanEnrichmentProcessor)
	listeners = append(listeners, &thresholdsHelper)
	listeners = append(listeners, metricsHelper)
	listeners = append(listeners, p)
	listeners = append(listeners, &traceSampler)
	configRefresh.configListeners = listeners
	p.configRefresh = &configRefresh

	metricsHelper.startExporter()
	return p, nil
}
