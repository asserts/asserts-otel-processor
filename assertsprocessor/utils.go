package assertsprocessor

import (
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"net/http"
	"regexp"
	"strings"
	"time"
)

func (p *assertsProcessorImpl) startExporter() {
	s := &http.Server{
		Addr:           ":9465",
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	// Add Go module build info.
	p.prometheusRegistry.MustRegister(collectors.NewBuildInfoCollector())
	p.prometheusRegistry.MustRegister(collectors.NewGoCollector(
		collectors.WithGoCollectorRuntimeMetrics(collectors.GoRuntimeMetricsRule{Matcher: regexp.MustCompile("/.*")}),
	))
	p.prometheusRegistry.Register(p.latencyHistogram)

	// Expose the registered metrics via HTTP.
	http.Handle("/metrics", promhttp.HandlerFor(
		p.prometheusRegistry,
		promhttp.HandlerOpts{},
	))

	p.logger.Info("Starting Prometheus Exporter Listening on port 9465")
	p.logger.Fatal("Error starting Prometheus Server", zap.Error(s.ListenAndServe()))
}

func applyPromConventions(text string) string {
	replacer := strings.NewReplacer(
		" ", "_",
		",", "_",
		"\t", "_",
		"/", "_",
		"\\", "_",
		".", "_",
		"-", "_",
		":", "_",
		"=", "_",
		"“", "_",
		"@", "_",
		"<", "_",
		">", "_",
		"%", "_percent",
	)
	return strings.ToLower(replacer.Replace(text))
}
