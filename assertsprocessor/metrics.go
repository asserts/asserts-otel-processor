package assertsprocessor

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"sort"
	"strings"
)

type metrics struct {
	logger             *zap.Logger
	prometheusRegistry *prometheus.Registry
	latencyHistogram   *prometheus.HistogramVec
	totalTraceCount    *prometheus.CounterVec
	sampledTraceCount  *prometheus.CounterVec
	totalSpansCount    *prometheus.CounterVec
	sampledSpansCount  *prometheus.CounterVec
}

func (m *metrics) registerMetrics(captureAttributesInMetric []string) error {
	// Start the prometheus server on port 9465
	m.prometheusRegistry = prometheus.NewRegistry()

	var traceCountLabels = []string{envLabel, siteLabel, namespaceLabel, serviceLabel}
	var sampledTraceCountLabels = []string{envLabel, siteLabel, namespaceLabel, traceSampleTypeLabel, serviceLabel}
	var spanCountLabels = []string{envLabel, siteLabel}
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
	m.totalSpansCount, err = m.register("spans", "count_total", spanCountLabels, "Total Spans Counter")
	if err != nil {
		return err
	}
	// Create Counter for sampled spans count
	m.sampledSpansCount, err = m.register("spans", "sampled_count_total", spanCountLabels, "Total Spans Counter")
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
	m.totalSpansCount.Reset()
	m.sampledSpansCount.Reset()

	m.prometheusRegistry.Unregister(m.latencyHistogram)
	m.prometheusRegistry.Unregister(m.totalTraceCount)
	m.prometheusRegistry.Unregister(m.sampledTraceCount)
	m.prometheusRegistry.Unregister(m.totalSpansCount)
	m.prometheusRegistry.Unregister(m.sampledSpansCount)
}
