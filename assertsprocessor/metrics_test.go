package assertsprocessor

import (
	"github.com/google/go-cmp/cmp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"regexp"
	"testing"
	"time"
)

func TestShouldCaptureMetrics(t *testing.T) {
	logger, _ := zap.NewProduction()
	p := metricHelper{
		logger: logger,
		config: &Config{
			AttributeExps: &map[string]string{
				"rpc.system":  "aws-api",
				"rpc.service": "(Sqs)|(DynamoDb)",
			},
		},
		attributeValueRegExps: &map[string]*regexp.Regexp{},
	}

	err := p.compileSpanFilterRegexps()
	if err != nil {
		return
	}

	testSpan := ptrace.NewSpan()
	testSpan.Attributes().PutStr("rpc.system", "aws-api")
	testSpan.Attributes().PutStr("rpc.service", "DynamoDb")
	assert.True(t, p.shouldCaptureMetrics(testSpan))

	testSpan.Attributes().PutStr("rpc.service", "Sqs")
	assert.True(t, p.shouldCaptureMetrics(testSpan))

	testSpan.Attributes().PutStr("rpc.system", "kafka")
	assert.False(t, p.shouldCaptureMetrics(testSpan))

	testSpan.Attributes().PutStr("rpc.system", "aws-api")
	testSpan.Attributes().PutStr("rpc.service", "Ecs")
	assert.False(t, p.shouldCaptureMetrics(testSpan))
}

func TestBuildLabels(t *testing.T) {
	systemPattern, _ := regexp.Compile("aws-api")
	servicePattern, _ := regexp.Compile("(Sqs)|(DynamoDb)")
	logger, _ := zap.NewProduction()
	p := metricHelper{
		logger: logger,
		attributeValueRegExps: &map[string]*regexp.Regexp{
			"rpc.system":  systemPattern,
			"rpc.service": servicePattern,
		},
		config: &Config{
			Env:  "dev",
			Site: "us-west-2",
			CaptureAttributesInMetric: []string{"rpc.system", "rpc.service", "rpc.method",
				"aws.table.name", "aws.queue.url"},
		},
	}

	testSpan := ptrace.NewSpan()
	testSpan.Attributes().PutStr("rpc.system", "aws-api")
	testSpan.Attributes().PutStr("rpc.service", "DynamoDb")
	testSpan.Attributes().PutStr("rpc.method", "GetItem")
	testSpan.Attributes().PutStr("aws.table.name", "ride-bookings")

	expectedLabels := prometheus.Labels{}
	expectedLabels["asserts_env"] = "dev"
	expectedLabels["asserts_site"] = "us-west-2"
	expectedLabels["namespace"] = "ride-services"
	expectedLabels["service"] = "payment"
	expectedLabels["rpc_service"] = "DynamoDb"
	expectedLabels["rpc_method"] = "GetItem"
	expectedLabels["aws_table_name"] = "ride-bookings"
	expectedLabels["aws_queue_url"] = ""
	expectedLabels["rpc_system"] = "aws-api"

	actualLabels := p.buildLabels("ride-services", "payment", testSpan)
	assert.True(t, cmp.Equal(&expectedLabels, &actualLabels))
}

func TestCaptureMetrics(t *testing.T) {
	systemPattern, _ := regexp.Compile("aws-api")
	servicePattern, _ := regexp.Compile("(Sqs)|(DynamoDb)")
	logger, _ := zap.NewProduction()
	p := metricHelper{
		logger: logger,
		attributeValueRegExps: &map[string]*regexp.Regexp{
			"rpc.system":  systemPattern,
			"rpc.service": servicePattern,
		},
		config: &Config{
			Env:  "dev",
			Site: "us-west-2",
			CaptureAttributesInMetric: []string{"rpc.system", "rpc.service", "rpc.method",
				"aws.table.name", "aws.queue.url"},
		},
	}
	_ = p.buildHistogram()
	testSpan := ptrace.NewSpan()
	testSpan.Attributes().PutStr("rpc.system", "aws-api")
	testSpan.Attributes().PutStr("rpc.service", "DynamoDb")
	testSpan.Attributes().PutStr("rpc.method", "GetItem")
	testSpan.Attributes().PutStr("aws.table.name", "ride-bookings")

	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(1e9 + 6e8)

	expectedLabels := prometheus.Labels{}
	expectedLabels["asserts_env"] = "dev"
	expectedLabels["asserts_site"] = "us-west-2"
	expectedLabels["namespace"] = "ride-services"
	expectedLabels["service"] = "payment"
	expectedLabels["rpc_service"] = "DynamoDb"
	expectedLabels["rpc_method"] = "GetItem"
	expectedLabels["aws_table_name"] = "ride-bookings"
	expectedLabels["aws_queue_url"] = ""
	expectedLabels["rpc_system"] = "aws-api"

	p.captureMetrics("ride-services", "payment", testSpan)
	metric, err := p.latencyHistogram.GetMetricWith(expectedLabels)
	assert.Nil(t, err)
	assert.NotNil(t, metric)
}

func TestStartExporter(t *testing.T) {
	systemPattern, _ := regexp.Compile("aws-api")
	servicePattern, _ := regexp.Compile("(Sqs)|(DynamoDb)")
	logger, _ := zap.NewProduction()
	p := metricHelper{
		logger: logger,
		attributeValueRegExps: &map[string]*regexp.Regexp{
			"rpc.system":  systemPattern,
			"rpc.service": servicePattern,
		},
		config: &Config{
			Env:                    "dev",
			Site:                   "us-west-2",
			PrometheusExporterPort: 19465,
			CaptureAttributesInMetric: []string{"rpc.system", "rpc.service", "rpc.method",
				"aws.table.name", "aws.queue.url"},
		},
	}
	_ = p.buildHistogram()
	go p.startExporter()
	time.Sleep(2 * time.Second)
}
