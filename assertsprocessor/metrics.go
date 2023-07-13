package assertsprocessor

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"sort"
	"strings"
)

type metrics struct {
	logger             *zap.Logger
	config             *Config
	prometheusRegistry *prometheus.Registry
	latencyHistogram   *prometheus.HistogramVec
	totalTraceCount    *prometheus.CounterVec
	sampledTraceCount  *prometheus.CounterVec
	totalSpanCount     *prometheus.CounterVec
	sampledSpanCount   *prometheus.CounterVec
}

func (m *metrics) registerMetrics(captureAttributesInMetric []string) error {
	// Start the prometheus server on port 9465
	m.prometheusRegistry = prometheus.NewRegistry()

	var traceCountLabels = []string{envLabel, siteLabel}
	var sampledTraceCountLabels = []string{envLabel, siteLabel, traceSampleTypeLabel}
	var spanCountLabels = []string{envLabel, siteLabel, namespaceLabel, serviceLabel}
	var err error

	// Create Counter for total trace count
	m.totalTraceCount, err = m.register("trace", "count_total", traceCountLabels, "Total Trace Counter")
	if err != nil {
		return err
	}
	// Create Counter for sampled trace count
	m.sampledTraceCount, err = m.register("trace", "sampled_count_total", sampledTraceCountLabels, "Sampled Trace Counter")
	if err != nil {
		return err
	}
	// Create Counter for total spans count
	m.totalSpanCount, err = m.register("span", "count_total", spanCountLabels, "Total Span Counter")
	if err != nil {
		return err
	}
	// Create Counter for sampled spans count
	m.sampledSpanCount, err = m.register("span", "sampled_count_total", spanCountLabels, "Sampled Span Counter")
	if err != nil {
		return err
	}

	return m.registerLatencyHistogram(captureAttributesInMetric)
}

func (m *metrics) register(subsystem string, name string, labels []string, msg string) (*prometheus.CounterVec, error) {
	m.logger.Info(msg+" with ", zap.String("labels", strings.Join(labels, ", ")))

	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "asserts",
		Subsystem: subsystem,
		Name:      name,
	}, labels)
	err := m.prometheusRegistry.Register(counter)
	if err != nil {
		m.logger.Fatal("Error registering "+msg+" Vector", zap.Error(err))
		return nil, err
	}
	return counter, nil
}

func (m *metrics) registerLatencyHistogram(captureAttributesInMetric []string) error {
	var spanMetricLabels = []string{envLabel, siteLabel, namespaceLabel, serviceLabel, spanKind}

	if captureAttributesInMetric != nil {
		for _, label := range captureAttributesInMetric {
			spanMetricLabels = append(spanMetricLabels, applyPromConventions(label))
		}
	}
	sort.Strings(spanMetricLabels)
	m.logger.Info("Registering Latency Histogram with ", zap.String("labels", strings.Join(spanMetricLabels, ", ")))

	m.latencyHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "otel",
		Subsystem: "span",
		Name:      "latency_seconds",
		Buckets:   m.config.LatencyHistogramBuckets,
	}, spanMetricLabels)
	err := m.prometheusRegistry.Register(m.latencyHistogram)
	if err != nil {
		m.logger.Fatal("Error registering Latency Histogram Metric Vector", zap.Error(err))
		return err
	}

	return nil
}

func (m *metrics) unregisterMetrics() {
	m.latencyHistogram.Reset()
	m.totalTraceCount.Reset()
	m.sampledTraceCount.Reset()
	m.totalSpanCount.Reset()
	m.sampledSpanCount.Reset()

	m.prometheusRegistry.Unregister(m.latencyHistogram)
	m.prometheusRegistry.Unregister(m.totalTraceCount)
	m.prometheusRegistry.Unregister(m.sampledTraceCount)
	m.prometheusRegistry.Unregister(m.totalSpanCount)
	m.prometheusRegistry.Unregister(m.sampledSpanCount)
}

func (m *metrics) incrTotalCounts(tr *trace) {
	m.incrTotalTraceCount()
	m.incrTotalSpanCount(tr)
}

func (m *metrics) incrSampledCounts(tr *trace, sampleType string) {
	m.incrSampledTraceCount(sampleType)
	m.incrSampledSpanCount(tr)
}

func (m *metrics) incrTotalTraceCount() {
	sampledTraceCountLabels := map[string]string{
		envLabel:  m.config.Env,
		siteLabel: m.config.Site,
	}
	m.totalTraceCount.With(sampledTraceCountLabels).Inc()
}

func (m *metrics) incrSampledTraceCount(sampleType string) {
	sampledTraceCountLabels := map[string]string{
		envLabel:             m.config.Env,
		siteLabel:            m.config.Site,
		traceSampleTypeLabel: sampleType,
	}
	m.sampledTraceCount.With(sampledTraceCountLabels).Inc()
}

func (m *metrics) incrTotalSpanCount(tr *trace) {
	m.incrSpanCount(tr, m.totalSpanCount)
}

func (m *metrics) incrSampledSpanCount(tr *trace) {
	m.incrSpanCount(tr, m.sampledSpanCount)
}

func (m *metrics) incrSpanCount(tr *trace, spanCounter *prometheus.CounterVec) {
	for _, ts := range tr.segments {
		spanCountLabels := map[string]string{
			envLabel:       m.config.Env,
			siteLabel:      m.config.Site,
			namespaceLabel: ts.namespace,
			serviceLabel:   ts.service,
		}
		count := float64(ts.getSpanCount())
		spanCounter.With(spanCountLabels).Add(count)
	}
}
