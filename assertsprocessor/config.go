package assertsprocessor

import (
	"regexp"
)

type AssertsServer struct {
	endpoint string `mapstructure:"endpoint"`
	user     string `mapstructure:"user"`
	password string `mapstructure:"password"`
}

type Config struct {
	AssertsServer                  *AssertsServer     `mapstructure:"asserts_server"`
	Env                            string             `mapstructure:"asserts_env"`
	Site                           string             `mapstructure:"asserts_site"`
	AttributeExps                  *map[string]string `mapstructure:"span_attribute_match_regex"`
	RequestContextExps             *map[string]string `mapstructure:"request_context_regex"`
	CaptureAttributesInMetric      []string           `mapstructure:"attributes_as_metric_labels"`
	DefaultLatencyThreshold        float64            `mapstructure:"sampling_latency_threshold_seconds"`
	MaxTracesPerMinute             int                `mapstructure:"max_traces_per_minute"`
	MaxTracesPerMinutePerContainer int                `mapstructure:"max_traces_per_minute_per_container"`
	NormalSamplingFrequencyMinutes int                `mapstructure:"normal_trace_sampling_rate_minutes"`
	TraceFlushIntervalSeconds      int                `mapstructure:"trace_flush_interval_seconds"`
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
	return nil
}
