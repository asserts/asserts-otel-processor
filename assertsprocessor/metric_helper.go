package assertsprocessor

import (
	"context"
	"errors"
	"github.com/jellydator/ttlcache/v3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/puzpuzpuz/xsync/v2"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"
)

const (
	envLabel             = "asserts_env"
	siteLabel            = "asserts_site"
	namespaceLabel       = "namespace"
	serviceLabel         = "service"
	spanKind             = "span_kind"
	statusCode           = "status_code"
	traceSampleTypeLabel = "sample_type"
)

type metricHelper struct {
	logger     *zap.Logger
	config     *Config
	httpServer *http.Server
	metrics    *metrics
	exp        *metricsExporter
	// limit cardinality of request contexts for which metrics are captured
	requestContextsByService *xsync.MapOf[string, *ttlcache.Cache[string, prometheus.Labels]]
	ttl                      time.Duration
	// guard access to config.CaptureAttributesInMetric and latencyHistogram
	rwMutex *sync.RWMutex
}

func newMetricHelper(logger *zap.Logger, config *Config, buildInfo component.BuildInfo) *metricHelper {
	metrics := &metrics{
		logger:             logger,
		config:             config,
		buildInfo:          buildInfo,
		prometheusRegistry: prometheus.NewRegistry(),
	}
	exporter := &metricsExporter{
		logger: logger,
		config: config,
	}
	return &metricHelper{
		logger:                   logger,
		config:                   config,
		ttl:                      time.Minute * time.Duration(config.RequestContextCacheTTL),
		metrics:                  metrics,
		exp:                      exporter,
		requestContextsByService: xsync.NewMapOf[*ttlcache.Cache[string, prometheus.Labels]](),
		rwMutex:                  &sync.RWMutex{},
	}
}

func (p *metricHelper) recordLatency(labels prometheus.Labels, latencySeconds float64) {
	p.rwMutex.RLock()
	defer p.rwMutex.RUnlock()
	p.metrics.latencyHistogram.With(labels).Observe(latencySeconds)
}

func (p *metricHelper) registerMetrics() error {
	return p.metrics.registerMetrics(p.getAttributesAsLabels())
}

func (p *metricHelper) getAttributesAsLabels() []string {
	attributes := make([]string, 0)
	for _, att := range p.config.CaptureAttributesInMetric {
		attributes = append(attributes, att)
	}
	attributes = append(attributes, AssertsRequestTypeAttribute)
	attributes = append(attributes, AssertsRequestContextAttribute)
	attributes = append(attributes, AssertsErrorTypeAttribute)
	return attributes
}

func (p *metricHelper) captureMetrics(span *ptrace.Span, namespace string, service string,
	resourceSpan *ptrace.ResourceSpans) {
	serviceKey := getServiceKey(namespace, service)
	attrValue, _ := span.Attributes().Get(AssertsRequestContextAttribute)
	requestContext := attrValue.AsString()

	cache, _ := p.requestContextsByService.LoadOrCompute(serviceKey, func() *ttlcache.Cache[string, prometheus.Labels] {
		cache := ttlcache.New[string, prometheus.Labels](
			ttlcache.WithTTL[string, prometheus.Labels](p.ttl),
			ttlcache.WithCapacity[string, prometheus.Labels](uint64(p.config.LimitPerService)),
		)
		cache.OnEviction(
			func(ctx context.Context, reason ttlcache.EvictionReason, item *ttlcache.Item[string, prometheus.Labels]) {
				p.logger.Info("Evicted request context from cache",
					zap.String("service", serviceKey),
					zap.String("request context", item.Key()),
				)

				deletedCount := p.metrics.latencyHistogram.DeletePartialMatch(item.Value())

				p.logger.Info("Deleted stale metrics",
					zap.Int("count", deletedCount),
					zap.Any("having label values", item.Value()),
				)
			},
		)
		p.logger.Debug("Created a cache of known request contexts for service - " + serviceKey)

		go cache.Start() // starts automatic expired item deletion
		return cache
	})

	if val := cache.Get(requestContext); cache.Len() < p.config.LimitPerService || val != nil {
		labels := p.buildLabels(namespace, service, span, resourceSpan)
		if val == nil {
			// build labels map that will be used as a key to delete stale
			// metrics when the request context cache entry is evicted
			metricLabels := prometheus.Labels{
				namespaceLabel: namespace,
				serviceLabel:   service,
				applyPromConventions(AssertsRequestContextAttribute): requestContext,
			}
			cache.Set(requestContext, metricLabels, ttlcache.DefaultTTL)
			p.logger.Info("Adding request context to cache",
				zap.String("service", serviceKey),
				zap.String("request context", requestContext),
			)
		}
		latencySeconds := computeLatency(span)
		p.recordLatency(labels, latencySeconds)
	} else {
		p.logger.Warn("Too many request contexts. Metrics won't be captured for",
			zap.String("service", serviceKey),
			zap.String("request context", requestContext),
		)
	}
}

func (p *metricHelper) buildLabels(namespace string, service string, span *ptrace.Span,
	resourceSpan *ptrace.ResourceSpans) prometheus.Labels {

	p.rwMutex.RLock()
	defer p.rwMutex.RUnlock()

	labels := prometheus.Labels{
		envLabel:       p.config.Env,
		siteLabel:      p.config.Site,
		namespaceLabel: namespace,
		serviceLabel:   service,
	}

	capturedResourceAttributes := make([]string, 0)
	capturedSpanAttributes := make([]string, 0)
	for _, labelName := range p.getAttributesAsLabels() {
		value, present := span.Attributes().Get(labelName)
		if !present {
			value, present = resourceSpan.Resource().Attributes().Get(labelName)
			if present {
				capturedResourceAttributes = append(capturedResourceAttributes, labelName)
			}
		} else {
			capturedSpanAttributes = append(capturedSpanAttributes, labelName)
		}
		if present {
			labels[applyPromConventions(labelName)] = value.AsString()
		} else {
			labels[applyPromConventions(labelName)] = ""
		}
	}
	labels[spanKind] = span.Kind().String()
	labels[statusCode] = span.Status().Code().String()
	p.logger.Debug("Captured Metric labels",
		zap.String("traceId", span.TraceID().String()),
		zap.String("spanId", span.SpanID().String()),
		zap.String("capturedSpanAttributes", strings.Join(capturedSpanAttributes, ", ")),
		zap.String("capturedResourceAttributes", strings.Join(capturedResourceAttributes, ", ")),
	)
	return labels
}

func (p *metricHelper) startExporter() {
	p.exp.start(p.metrics.prometheusRegistry)
}

func (p *metricHelper) stopExporter() error {
	p.metrics.unregisterMetrics()
	return p.exp.stop()
}

// configListener interface implementation
func (p *metricHelper) isUpdated(currConfig *Config, newConfig *Config) bool {
	p.rwMutex.RLock()
	defer p.rwMutex.RUnlock()

	return p.isCaptureAttributesInMetricUpdated(currConfig, newConfig) ||
		p.isLatencyHistogramBucketsUpdated(currConfig, newConfig)
}

func (p *metricHelper) isCaptureAttributesInMetricUpdated(currConfig *Config, newConfig *Config) bool {
	updated := !reflect.DeepEqual(currConfig.CaptureAttributesInMetric, newConfig.CaptureAttributesInMetric)
	if updated {
		p.logger.Info("Change detected in config CaptureAttributesInMetric",
			zap.Any("Current", currConfig.CaptureAttributesInMetric),
			zap.Any("New", newConfig.CaptureAttributesInMetric),
		)
	} else {
		p.logger.Debug("No change detected in config CaptureAttributesInMetric")
	}
	return updated
}

func (p *metricHelper) isLatencyHistogramBucketsUpdated(currConfig *Config, newConfig *Config) bool {
	updated := len(newConfig.LatencyHistogramBuckets) > 0 &&
		!reflect.DeepEqual(currConfig.LatencyHistogramBuckets, newConfig.LatencyHistogramBuckets)
	if updated {
		p.logger.Info("Change detected in config LatencyHistogramBuckets",
			zap.Any("Current", currConfig.LatencyHistogramBuckets),
			zap.Any("New", newConfig.LatencyHistogramBuckets),
		)
	} else {
		p.logger.Debug("No change detected in config LatencyHistogramBuckets")
	}
	return updated
}

func (p *metricHelper) onUpdate(newConfig *Config) error {
	p.rwMutex.Lock()
	defer p.rwMutex.Unlock()

	// This is a bit tricky! We cannot simply register the metric again with different labels
	// We have to throw away the existing prometheus registry, shutdown the prometheus exporter
	// and redo all of that work again
	err := p.stopExporter()
	if err == nil {
		currConfigCaptureAttributesInMetric := p.config.CaptureAttributesInMetric
		currConfigLatencyHistogramBuckets := p.config.LatencyHistogramBuckets
		// use new config
		p.config.CaptureAttributesInMetric = newConfig.CaptureAttributesInMetric
		if len(newConfig.LatencyHistogramBuckets) > 0 {
			p.config.LatencyHistogramBuckets = newConfig.LatencyHistogramBuckets
		}

		// create new prometheus registry and register metrics
		err = p.registerMetrics()
		if err == nil {
			p.logger.Info("Updated config",
				zap.Any("CaptureAttributesInMetric", newConfig.CaptureAttributesInMetric),
				zap.Any("LatencyHistogramBuckets", newConfig.LatencyHistogramBuckets),
			)
		} else {
			p.logger.Error("Ignoring config CaptureAttributesInMetric and LatencyHistogramBuckets "+
				"due to error registering new latency histogram",
				zap.Error(err),
			)
			// latency histogram registration failed, reverting to old config
			// create new prometheus registry and register metrics again
			p.config.CaptureAttributesInMetric = currConfigCaptureAttributesInMetric
			p.config.LatencyHistogramBuckets = currConfigLatencyHistogramBuckets
			_ = p.registerMetrics()
		}

		p.startExporter()
	} else {
		err = errors.New("error stopping http server exporting prometheus metrics")
		p.logger.Error("Ignoring config CaptureAttributesInMetric and LatencyHistogramBuckets", zap.Error(err))
	}
	return err
}
