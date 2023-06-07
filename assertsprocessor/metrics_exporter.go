package assertsprocessor

import (
	"context"
	"errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

type metricsExporter struct {
	logger     *zap.Logger
	config     *Config
	httpServer *http.Server
}

func (exp *metricsExporter) start(reg *prometheus.Registry) {
	// Create a new ServeMux instead of using the DefaultServeMux. This allows registering
	// a handler function for the same URL pattern again on a different htp server instance
	sm := http.NewServeMux()
	// Expose the registered metrics via HTTP.
	sm.Handle("/metrics", promhttp.HandlerFor(
		reg,
		promhttp.HandlerOpts{},
	))

	exp.httpServer = &http.Server{
		Handler:        sm,
		Addr:           ":" + strconv.FormatUint(exp.config.PrometheusExporterPort, 10),
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	// Add Go module build info.
	reg.MustRegister(collectors.NewBuildInfoCollector())
	reg.MustRegister(collectors.NewGoCollector(
		collectors.WithGoCollectorRuntimeMetrics(collectors.GoRuntimeMetricsRule{Matcher: regexp.MustCompile("/.*")}),
	))

	exp.logger.Info("Starting Prometheus Exporter Listening", zap.Uint64("port", exp.config.PrometheusExporterPort))
	go func() {
		if err := exp.httpServer.ListenAndServe(); err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				exp.logger.Error("Prometheus Exporter is shutdown", zap.Error(err))
			} else if err != nil {
				exp.logger.Fatal("Error starting Prometheus Exporter", zap.Error(err))
			}
		}
	}()
}

func (exp *metricsExporter) stop() error {
	shutdownCtx := context.Background()
	return exp.httpServer.Shutdown(shutdownCtx)
}
