package assertsprocessor

import (
	"fmt"
	"regexp"
)

type MatcherDto struct {
	SpanKind    string `mapstructure:"span_kind" json:"span_kind"`
	AttrName    string `mapstructure:"attr_name" json:"attr_name"`
	Regex       string `mapstructure:"regex" json:"regex"`
	Replacement string `mapstructure:"replacement" json:"replacement"`
}

type Config struct {
	AssertsServer                  *map[string]string            `mapstructure:"asserts_server" json:"asserts_server"`
	Env                            string                        `mapstructure:"asserts_env" json:"asserts_env"`
	Site                           string                        `mapstructure:"asserts_site" json:"asserts_site"`
	CaptureMetrics                 bool                          `mapstructure:"capture_metrics" json:"capture_metrics"`
	RequestContextExps             map[string][]*MatcherDto      `mapstructure:"request_context_regex" json:"request_context_regex"`
	ErrorTypeConfigs               map[string][]*ErrorTypeConfig `mapstructure:"error_type_config" json:"error_type_config"`
	CaptureAttributesInMetric      []string                      `mapstructure:"attributes_as_metric_labels" json:"attributes_as_metric_labels"`
	DefaultLatencyThreshold        float64                       `mapstructure:"sampling_latency_threshold_seconds" json:"sampling_latency_threshold_seconds"`
	LimitPerService                int                           `mapstructure:"trace_rate_limit_per_service" json:"trace_rate_limit_per_service"`
	LimitPerRequestPerService      int                           `mapstructure:"trace_rate_limit_per_service_per_request" json:"trace_rate_limit_per_service_per_request"`
	RequestContextCacheTTL         int                           `mapstructure:"request_context_cache_ttl_minutes" json:"request_context_cache_ttl_minutes"`
	NormalSamplingFrequencyMinutes int                           `mapstructure:"normal_trace_sampling_rate_minutes" json:"normal_trace_sampling_rate_minutes"`
	PrometheusExporterPort         uint64                        `mapstructure:"prometheus_exporter_port" json:"prometheus_exporter_port"`
	TraceFlushFrequencySeconds     int                           `mapstructure:"trace_flush_frequency_seconds" json:"trace_flush_frequency_seconds"`
}

// Validate implements the component.ConfigValidator interface.
// Checks for any invalid regexp
func (config *Config) Validate() error {
	for serviceKey, serviceRequestContextExps := range config.RequestContextExps {
		for _, attrRegex := range serviceRequestContextExps {
			_, err := regexp.Compile(attrRegex.Regex)
			if err != nil {
				return ValidationError{
					message: fmt.Sprintf("Invalid regexp %s: for service: %s, span_kind: %s and attribute: %s",
						attrRegex.Regex, serviceKey, attrRegex.SpanKind, attrRegex.AttrName),
				}
			}
		}
	}

	for attrName, errorTypeConfigs := range config.ErrorTypeConfigs {
		for _, theConfig := range errorTypeConfigs {
			_, err := theConfig.compile()
			if err != nil {
				return ValidationError{
					message: fmt.Sprintf("Invalid regexp %s: for attribute: %s and error type: %s",
						theConfig.ValueExpr, attrName, theConfig.ErrorType),
				}
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
