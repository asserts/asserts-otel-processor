package assertsprocessor

import (
	"fmt"
	"regexp"
)

type AssertsServer struct {
	endpoint string `mapstructure:"endpoint"`
	user     string `mapstructure:"user"`
	password string `mapstructure:"password"`
}

type Config struct {
	AssertsServer                  *map[string]string `mapstructure:"asserts_server"`
	Env                            string             `mapstructure:"asserts_env"`
	Site                           string             `mapstructure:"asserts_site"`
	AttributeExps                  *map[string]string `mapstructure:"span_attribute_match_regex"`
	RequestContextExps             *map[string]string `mapstructure:"request_context_regex"`
	CaptureAttributesInMetric      []string           `mapstructure:"attributes_as_metric_labels"`
	DefaultLatencyThreshold        float64            `mapstructure:"sampling_latency_threshold_seconds"`
	LimitPerService                int                `mapstructure:"trace_rate_limit_per_service"`
	LimitPerRequestPerService      int                `mapstructure:"trace_rate_limit_per_service_per_request"`
	NormalSamplingFrequencyMinutes int                `mapstructure:"normal_trace_sampling_rate_minutes"`
	PrometheusExporterPort         uint64             `mapstructure:"prometheus_exporter_port"`
	TraceFlushFrequencySeconds     int                `mapstructure:"trace_flush_frequency_seconds"`
}

// Validate implements the component.ConfigValidator interface.
// Checks for any invalid regexp
func (config *Config) Validate() error {
	for _, exp := range *config.AttributeExps {
		_, err := regexp.Compile(exp)
		if err != nil {
			return err
		}
	}

	for _, exp := range *config.RequestContextExps {
		_, err := regexp.Compile(exp)
		if err != nil {
			return err
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
