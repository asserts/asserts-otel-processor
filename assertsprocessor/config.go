package assertsprocessor

import (
	"fmt"
	"regexp"
)

type MatcherDto struct {
	AttrName    string `mapstructure:"attr_name"`
	Regex       string `mapstructure:"regex"`
	Replacement string `mapstructure:"replacement"`
}

type Config struct {
	AssertsServer                  *map[string]string       `mapstructure:"asserts_server"`
	Env                            string                   `mapstructure:"asserts_env"`
	Site                           string                   `mapstructure:"asserts_site"`
	CaptureMetrics                 bool                     `mapstructure:"capture_metrics"`
	RequestContextExps             map[string][]*MatcherDto `mapstructure:"request_context_regex"`
	CaptureAttributesInMetric      []string                 `mapstructure:"attributes_as_metric_labels"`
	DefaultLatencyThreshold        float64                  `mapstructure:"sampling_latency_threshold_seconds"`
	LimitPerService                int                      `mapstructure:"trace_rate_limit_per_service"`
	LimitPerRequestPerService      int                      `mapstructure:"trace_rate_limit_per_service_per_request"`
	RequestContextCacheTTL         int                      `mapstructure:"request_context_cache_ttl_minutes"`
	NormalSamplingFrequencyMinutes int                      `mapstructure:"normal_trace_sampling_rate_minutes"`
	PrometheusExporterPort         uint64                   `mapstructure:"prometheus_exporter_port"`
	TraceFlushFrequencySeconds     int                      `mapstructure:"trace_flush_frequency_seconds"`
}

// Validate implements the component.ConfigValidator interface.
// Checks for any invalid regexp
func (config *Config) Validate() error {
	for _, serviceRequestContextExps := range config.RequestContextExps {
		for _, attrRegex := range serviceRequestContextExps {
			_, err := regexp.Compile(attrRegex.Regex)
			if err != nil {
				return err
			}
		}
	}

	if config.LimitPerService < config.LimitPerRequestPerService {
		return ValidationError{
			message: fmt.Sprintf("LimitPerService: %d < LimitPerRequestPerService: %d",
				config.LimitPerService, config.LimitPerRequestPerService),
		}
	}
	return nil
}

type ValidationError struct {
	message string
	error
}

func (v ValidationError) Error() string {
	return v.message
}
