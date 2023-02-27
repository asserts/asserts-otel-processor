package assertsprocessor

import (
	"github.com/google/go-cmp/cmp"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"regexp"
	"testing"
)

func TestShouldCaptureMetrics(t *testing.T) {
	systemPattern, _ := regexp.Compile("aws-api")
	servicePattern, _ := regexp.Compile("(Sqs)|(DynamoDb)")
	logger, _ := zap.NewProduction()
	p := &assertsProcessorImpl{
		logger: logger,
		attributeValueRegExps: &map[string]regexp.Regexp{
			"rpc.system":  *systemPattern,
			"rpc.service": *servicePattern,
		},
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
	p := &assertsProcessorImpl{
		logger: logger,
		attributeValueRegExps: &map[string]regexp.Regexp{
			"rpc.system":  *systemPattern,
			"rpc.service": *servicePattern,
		},
		config: Config{
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

func TestComputeLatency(t *testing.T) {
	systemPattern, _ := regexp.Compile("aws-api")
	servicePattern, _ := regexp.Compile("(Sqs)|(DynamoDb)")
	logger, _ := zap.NewProduction()
	p := &assertsProcessorImpl{
		logger: logger,
		attributeValueRegExps: &map[string]regexp.Regexp{
			"rpc.system":  *systemPattern,
			"rpc.service": *servicePattern,
		},
		config: Config{
			DefaultLatencyThreshold: 0.5,
		},
		latencyBounds: cmap.New[cmap.ConcurrentMap[string, LatencyBound]](),
		entityKeys:    cmap.New[EntityKeyDto](),
	}

	testSpan := ptrace.NewSpan()
	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(1e9 + 4e8)
	assert.Equal(t, 0.4, p.computeLatency(testSpan))
}

func TestShouldCaptureTraceDefaultThreshold(t *testing.T) {
	systemPattern, _ := regexp.Compile("aws-api")
	servicePattern, _ := regexp.Compile("(Sqs)|(DynamoDb)")
	logger, _ := zap.NewProduction()
	p := &assertsProcessorImpl{

		logger: logger,
		attributeValueRegExps: &map[string]regexp.Regexp{
			"rpc.system":  *systemPattern,
			"rpc.service": *servicePattern,
		},
		config: Config{
			Env: "dev", Site: "us-west-2",
			DefaultLatencyThreshold: 0.5,
		},
		latencyBounds: cmap.New[cmap.ConcurrentMap[string, LatencyBound]](),
		entityKeys:    cmap.New[EntityKeyDto](),
	}

	testSpan := ptrace.NewSpan()
	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(2e9)
	assert.True(t, p.shouldCaptureTrace("n", "s", "123", testSpan))

	dto := EntityKeyDto{
		EntityType: "Service", Name: "s", Scope: map[string]string{
			"asserts_env": "dev", "asserts_site": "us-west-2", "namespace": "n",
		},
	}
	load, ok := p.entityKeys.Get(dto.AsString())
	assert.True(t, ok)
	assert.Equal(t, load, dto)

	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(1e9 + 4e8)
	assert.False(t, p.shouldCaptureTrace("n", "s", "123", testSpan))
}

func TestShouldCaptureTraceDynamicThreshold(t *testing.T) {
	systemPattern, _ := regexp.Compile("aws-api")
	servicePattern, _ := regexp.Compile("(Sqs)|(DynamoDb)")
	logger, _ := zap.NewProduction()
	p := &assertsProcessorImpl{

		logger: logger,
		attributeValueRegExps: &map[string]regexp.Regexp{
			"rpc.system":  *systemPattern,
			"rpc.service": *servicePattern,
		},
		config: Config{
			Env: "dev", Site: "us-west-2",
			DefaultLatencyThreshold: 0.5,
		},
		latencyBounds: cmap.New[cmap.ConcurrentMap[string, LatencyBound]](),
		entityKeys:    cmap.New[EntityKeyDto](),
	}

	dto := EntityKeyDto{
		EntityType: "Service", Name: "s", Scope: map[string]string{
			"asserts_env": "dev", "asserts_site": "us-west-2", "namespace": "n",
		},
	}
	p.entityKeys.Set(dto.AsString(), dto)
	p.latencyBounds.Set(dto.AsString(), cmap.New[LatencyBound]())
	get, _ := p.latencyBounds.Get(dto.AsString())
	get.Set("/v1/latency-thresholds", LatencyBound{
		Upper: 1,
	})

	testSpan := ptrace.NewSpan()
	testSpan.SetName("/v1/latency-thresholds")

	// Latency threshold higher than the default, but lower than the dynamic threshold
	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(1e9 + 7e8)
	assert.False(t, p.shouldCaptureTrace("n", "s", "123", testSpan))
}
