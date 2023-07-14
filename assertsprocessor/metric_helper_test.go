package assertsprocessor

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"testing"
	"time"
)

var buildInfo = component.BuildInfo{}

func TestBuildLabels(t *testing.T) {
	logger, _ := zap.NewProduction()
	attributes := []string{"rpc.system", "rpc.service", "rpc.method",
		"aws.table.name", "aws.queue.url", "host.name"}
	c := &Config{
		Env:                       "dev",
		Site:                      "us-west-2",
		CaptureAttributesInMetric: attributes,
	}
	config.CaptureAttributesInMetric = attributes
	p := newMetricHelper(logger, c, buildInfo)

	resourceSpans := ptrace.NewTraces().ResourceSpans().AppendEmpty()
	resourceSpans.Resource().Attributes().PutStr("host.name", "192.168.1.19")

	testSpan := ptrace.NewSpan()
	testSpan.SetKind(ptrace.SpanKindClient)
	testSpan.Attributes().PutStr(AssertsRequestTypeAttribute, "outbound")
	testSpan.Attributes().PutStr(AssertsRequestContextAttribute, "GetItem")
	testSpan.Attributes().PutStr("rpc.system", "aws-api")
	testSpan.Attributes().PutStr("rpc.service", "DynamoDb")
	testSpan.Attributes().PutStr("rpc.method", "GetItem")
	testSpan.Attributes().PutStr("aws.table.name", "ride-bookings")

	expectedLabels := prometheus.Labels{}
	expectedLabels[envLabel] = "dev"
	expectedLabels[siteLabel] = "us-west-2"
	expectedLabels[namespaceLabel] = "ride-services"
	expectedLabels[serviceLabel] = "payment"
	expectedLabels[requestContextLabel] = "GetItem"
	expectedLabels[requestTypeLabel] = "outbound"
	expectedLabels[errorTypeLabel] = ""
	expectedLabels["rpc_service"] = "DynamoDb"
	expectedLabels["rpc_method"] = "GetItem"
	expectedLabels["aws_table_name"] = "ride-bookings"
	expectedLabels["rpc_system"] = "aws-api"
	expectedLabels["host_name"] = "192.168.1.19"
	expectedLabels["span_kind"] = "Client"
	expectedLabels["aws_queue_url"] = ""

	actualLabels := p.buildLabels("ride-services", "payment", &testSpan, &resourceSpans)
	assert.Equal(t, expectedLabels, actualLabels)
}

func TestCaptureMetrics(t *testing.T) {
	logger, _ := zap.NewProduction()

	attributes := []string{"rpc.system", "rpc.service", "rpc.method",
		"aws.table.name", "aws.queue.url", "host.name"}
	c := &Config{
		Env:             "dev",
		Site:            "us-west-2",
		LimitPerService: 100,
	}
	config.CaptureAttributesInMetric = attributes
	p := newMetricHelper(logger, c, buildInfo)
	err := p.registerMetrics()
	assert.Nil(t, err)
	resourceSpans := ptrace.NewTraces().ResourceSpans().AppendEmpty()

	testSpan := ptrace.NewSpan()
	testSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/request")
	testSpan.Attributes().PutStr(AssertsRequestTypeAttribute, "inbound")
	testSpan.Attributes().PutStr(AssertsErrorTypeAttribute, "client_errors")
	testSpan.Attributes().PutStr("rpc.system", "aws-api")
	testSpan.Attributes().PutStr("rpc.service", "DynamoDb")
	testSpan.Attributes().PutStr("rpc.method", "GetItem")
	testSpan.Attributes().PutStr("aws.table.name", "ride-bookings")
	testSpan.Attributes().PutStr("aws.table.nameee", "ride-bookings")
	testSpan.SetKind(ptrace.SpanKindClient)

	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(1e9 + 6e8)

	expectedLabels := prometheus.Labels{}
	expectedLabels[envLabel] = "dev"
	expectedLabels[siteLabel] = "us-west-2"
	expectedLabels[namespaceLabel] = "ride-services"
	expectedLabels[serviceLabel] = "payment"
	expectedLabels[requestContextLabel] = "GetItem"
	expectedLabels["rpc_service"] = "DynamoDb"
	expectedLabels["rpc_method"] = "GetItem"
	expectedLabels["aws_table_name"] = "ride-bookings"
	expectedLabels["rpc_system"] = "aws-api"
	expectedLabels["span_kind"] = "Client"

	p.captureMetrics(&testSpan, "ride-services", "payment", &resourceSpans)
}

func TestMetricCardinalityLimit(t *testing.T) {
	logger, _ := zap.NewProduction()

	c := &Config{
		Env:             "dev",
		Site:            "us-west-2",
		LimitPerService: 2,
	}
	config.CaptureAttributesInMetric = []string{}
	p := newMetricHelper(logger, c, buildInfo)
	_ = p.registerMetrics()
	resourceSpans := ptrace.NewTraces().ResourceSpans().AppendEmpty()

	testSpan := ptrace.NewSpan()
	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(1e9 + 6e8)
	testSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/cart/#val1")
	p.captureMetrics(&testSpan, "robot-shop", "cart", &resourceSpans)
	assert.Equal(t, 1, p.requestContextsByService.Size())
	cache, _ := p.requestContextsByService.Load("robot-shop#cart")
	assert.Equal(t, 1, cache.Len())

	testSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/cart/#val2")
	p.captureMetrics(&testSpan, "robot-shop", "cart", &resourceSpans)
	assert.Equal(t, 2, cache.Len())

	testSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/cart/#val3")
	p.captureMetrics(&testSpan, "robot-shop", "cart", &resourceSpans)
	assert.Equal(t, 2, cache.Len())
	assert.NotNil(t, cache.Get("/cart/#val1"))
	assert.NotNil(t, cache.Get("/cart/#val2"))
	assert.Nil(t, cache.Get("/cart/#val3"))
}

func TestCacheEviction(t *testing.T) {
	logger, _ := zap.NewProduction()

	c := &Config{
		Env:                    "dev",
		Site:                   "us-west-2",
		LimitPerService:        2,
		RequestContextCacheTTL: 1,
	}
	config.CaptureAttributesInMetric = []string{}
	p := newMetricHelper(logger, c, buildInfo)
	// overwrite ttl to a smaller value of 5 millis in this unit test
	p.ttl = time.Millisecond * time.Duration(p.config.RequestContextCacheTTL)
	_ = p.registerMetrics()
	resourceSpans := ptrace.NewTraces().ResourceSpans().AppendEmpty()

	testSpan := ptrace.NewSpan()
	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(1e9 + 6e8)
	testSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/cart/#val1")
	p.captureMetrics(&testSpan, "robot-shop", "cart", &resourceSpans)
	assert.Equal(t, 1, p.requestContextsByService.Size())
	cache, _ := p.requestContextsByService.Load("robot-shop#cart")
	assert.Equal(t, 1, cache.Len())
	assert.Equal(t,
		prometheus.Labels{
			namespaceLabel: "robot-shop",
			serviceLabel:   "cart",
			applyPromConventions(AssertsRequestContextAttribute): "/cart/#val1",
		},
		cache.Get("/cart/#val1").Value())

	time.Sleep(5 * time.Millisecond)

	testSpan.Attributes().PutStr(AssertsRequestContextAttribute, "/cart/#val2")
	p.captureMetrics(&testSpan, "robot-shop", "cart", &resourceSpans)
	assert.Equal(t, 1, cache.Len())
}

func TestMetricHelperIsCaptureAttributesInMetricUpdated(t *testing.T) {
	currConfig := &Config{
		CaptureAttributesInMetric: []string{"rpc.system", "rpc.service"},
	}
	newConfig := &Config{
		CaptureAttributesInMetric: []string{"rpc.system", "rpc.service", "rpc.method"},
	}
	p := newMetricHelper(logger, currConfig, buildInfo)

	assert.False(t, p.isUpdated(currConfig, currConfig))
	assert.True(t, p.isUpdated(currConfig, newConfig))
}

func TestMetricHelperIsLatencyHistogramBucketsUpdated(t *testing.T) {
	currConfig := &Config{
		LatencyHistogramBuckets: []float64{1, 2.5, 5, 10},
	}
	newConfig := &Config{
		LatencyHistogramBuckets: []float64{1, 2.5, 5, 10, 25},
	}
	p := newMetricHelper(logger, currConfig, buildInfo)

	assert.False(t, p.isUpdated(currConfig, currConfig))
	assert.True(t, p.isUpdated(currConfig, newConfig))
}

func TestMetricHelperEmptyLatencyHistogramBuckets(t *testing.T) {
	currConfig := &Config{
		LatencyHistogramBuckets: []float64{1, 2.5, 5, 10},
	}
	newConfig := &Config{}
	p := newMetricHelper(logger, currConfig, buildInfo)

	assert.False(t, p.isUpdated(currConfig, currConfig))
	assert.False(t, p.isUpdated(currConfig, newConfig))
}

func TestMetricHelperOnUpdate(t *testing.T) {
	currConfig := &Config{
		CaptureAttributesInMetric: []string{"rpc.system", "rpc.service"},
		LatencyHistogramBuckets:   []float64{1, 2.5, 5, 10},
		PrometheusExporterPort:    9466,
	}
	newConfig := &Config{
		CaptureAttributesInMetric: []string{"rpc.system", "rpc.service", "rpc.method"},
		LatencyHistogramBuckets:   []float64{1, 2.5, 5, 10, 25},
	}

	logger, _ := zap.NewProduction()
	p := newMetricHelper(logger, currConfig, buildInfo)
	_ = p.registerMetrics()
	p.startExporter()

	assert.Nil(t, p.onUpdate(newConfig))
}
