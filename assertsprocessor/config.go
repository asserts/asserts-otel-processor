package assertsprocessor

import (
	"fmt"
	"regexp"
)

type MatcherDto struct {
	SpanKind    string `json:"span_kind"`
	AttrName    string `json:"attr_name"`
	Regex       string `json:"regex"`
	Replacement string `json:"replacement"`
}

type Config struct {
	AssertsServer                  *map[string]string            `json:"asserts_server"`
	Env                            string                        `json:"asserts_env"`
	Site                           string                        `json:"asserts_site"`
	CaptureMetrics                 bool                          `json:"capture_metrics"`
	RequestContextExps             map[string][]*MatcherDto      `json:"request_context_regex"`
	ErrorTypeConfigs               map[string][]*ErrorTypeConfig `json:"error_type_config"`
	CaptureAttributesInMetric      []string                      `json:"attributes_as_metric_labels"`
	DefaultLatencyThreshold        float64                       `json:"sampling_latency_threshold_seconds"`
	LimitPerService                int                           `json:"trace_rate_limit_per_service"`
	LimitPerRequestPerService      int                           `json:"trace_rate_limit_per_service_per_request"`
	RequestContextCacheTTL         int                           `json:"request_context_cache_ttl_minutes"`
	NormalSamplingFrequencyMinutes int                           `json:"normal_trace_sampling_rate_minutes"`
	PrometheusExporterPort         uint64                        `json:"prometheus_exporter_port"`
	TraceFlushFrequencySeconds     int                           `json:"trace_flush_frequency_seconds"`
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
