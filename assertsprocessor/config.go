package assertsprocessor

import (
	"fmt"
)

type SpanAttribute struct {
	AttributeName    string                 `mapstructure:"attr_name" json:"attr_name"`
	AttributeConfigs []*SpanAttributeConfig `mapstructure:"attr_configs" json:"attr_configs"`
}

type SpanAttributeConfig struct {
	Namespace string                   `mapstructure:"namespace" json:"namespace"`
	Service   string                   `mapstructure:"service" json:"service"`
	Rules     []*CustomAttributeConfig `mapstructure:"rules" json:"rules"`
}

type Config struct {
	AssertsServer                  *map[string]string                             `mapstructure:"asserts_server" json:"asserts_server"`
	Env                            string                                         `mapstructure:"asserts_env" json:"asserts_env"`
	Site                           string                                         `mapstructure:"asserts_site" json:"asserts_site"`
	AssertsTenant                  string                                         `mapstructure:"asserts_tenant" json:"asserts_tenant"`
	CaptureMetrics                 bool                                           `mapstructure:"capture_metrics" json:"capture_metrics"`
	CustomAttributeConfigs         map[string]map[string][]*CustomAttributeConfig `mapstructure:"custom_attributes" json:"custom_attributes"`
	SpanAttributes                 []*SpanAttribute                               `mapstructure:"span_attributes" json:"span_attributes"`
	CaptureAttributesInMetric      []string                                       `mapstructure:"attributes_as_metric_labels" json:"attributes_as_metric_labels"`
	DefaultLatencyThreshold        float64                                        `mapstructure:"sampling_latency_threshold_seconds" json:"sampling_latency_threshold_seconds"`
	LatencyHistogramBuckets        []float64                                      `mapstructure:"latency_histogram_buckets" json:"latency_histogram_buckets"`
	IgnoreClientErrors             bool                                           `mapstructure:"ignore_client_errors" json:"ignore_client_errors"`
	SampleTraces                   bool                                           `mapstructure:"sample_traces" json:"sample_traces"`
	LimitPerService                int                                            `mapstructure:"trace_rate_limit_per_service" json:"trace_rate_limit_per_service"`
	LimitPerRequestPerService      int                                            `mapstructure:"trace_rate_limit_per_service_per_request" json:"trace_rate_limit_per_service_per_request"`
	RequestContextCacheTTL         int                                            `mapstructure:"request_context_cache_ttl_minutes" json:"request_context_cache_ttl_minutes"`
	NormalSamplingFrequencyMinutes int                                            `mapstructure:"normal_trace_sampling_rate_minutes" json:"normal_trace_sampling_rate_minutes"`
	PrometheusExporterPort         uint64                                         `mapstructure:"prometheus_exporter_port" json:"prometheus_exporter_port"`
	TraceFlushFrequencySeconds     int                                            `mapstructure:"trace_flush_frequency_seconds" json:"trace_flush_frequency_seconds"`
}

// Validate implements the component.ConfigValidator interface.
// Checks for any invalid regexp
func (config *Config) Validate() error {
	if config.Env == "" {
		return ValidationError{
			message: fmt.Sprintf("Env property is not set"),
		}
	}
	for targetAtt, byServiceKey := range config.CustomAttributeConfigs {
		for serviceKey, configs := range byServiceKey {
			for _, _config := range configs {
				err := _config.validate(targetAtt, serviceKey)
				if err != nil {
					return err
				}
			}
		}
	}
	for _, spanAttribute := range config.SpanAttributes {
		attrName := spanAttribute.AttributeName
		for _, attrConfig := range spanAttribute.AttributeConfigs {
			serviceKey := getKey(attrConfig)
			for _, rule := range attrConfig.Rules {
				err := rule.validate(attrName, serviceKey)
				if err != nil {
					return err
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
