package assertsprocessor

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"regexp"
	"testing"
)

func TestBuildLabels(t *testing.T) {
	logger, _ := zap.NewProduction()
	p := newMetricHelper(
		logger,
		&Config{
			Env:  "dev",
			Site: "us-west-2",
			CaptureAttributesInMetric: []string{"rpc.system", "rpc.service", "rpc.method",
				"aws.table.name", "aws.queue.url", "host.name"},
		},
		&spanMatcher{},
	)

	resourceSpans := ptrace.NewTraces().ResourceSpans().AppendEmpty()
	resourceSpans.Resource().Attributes().PutStr("host.name", "192.168.1.19")

	testSpan := ptrace.NewSpan()
	testSpan.SetKind(ptrace.SpanKindClient)
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
	expectedLabels["rpc_service"] = "DynamoDb"
	expectedLabels["rpc_method"] = "GetItem"
	expectedLabels["aws_table_name"] = "ride-bookings"
	expectedLabels["rpc_system"] = "aws-api"
	expectedLabels["host_name"] = "192.168.1.19"
	expectedLabels["span_kind"] = "Client"
	expectedLabels["aws_queue_url"] = ""

	actualLabels := p.buildLabels("ride-services", "payment", "GetItem", &testSpan, &resourceSpans)
	assert.Equal(t, expectedLabels, actualLabels)
}

func TestCaptureMetrics(t *testing.T) {
	logger, _ := zap.NewProduction()
	compile, _ := regexp.Compile("(.+)")

	matcher := spanMatcher{
		spanAttrMatchers: map[string][]*spanAttrMatcher{
			"default": {
				{
					attrName:    "rpc.method",
					regex:       compile,
					replacement: "$1",
				},
			},
		},
	}
	p := newMetricHelper(
		logger,
		&Config{
			Env:  "dev",
			Site: "us-west-2",
			CaptureAttributesInMetric: []string{"rpc.system", "rpc.service", "rpc.method",
				"aws.table.name", "aws.queue.url", "host.name"},
			LimitPerService: 100,
		},
		&matcher,
	)
	err := p.init()
	assert.Nil(t, err)
	resourceSpans := ptrace.NewTraces().ResourceSpans().AppendEmpty()

	testSpan := ptrace.NewSpan()
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

	p.captureMetrics("ride-services", "payment", &testSpan, &resourceSpans)
}

func TestMetricCardinalityLimit(t *testing.T) {
	logger, _ := zap.NewProduction()
	compile, _ := regexp.Compile("https?://.+?(/.+?/.+)")
	matcher := spanMatcher{
		spanAttrMatchers: map[string][]*spanAttrMatcher{
			"default": {
				{
					attrName:    "http.url",
					regex:       compile,
					replacement: "$1",
				},
			},
		},
	}
	p := newMetricHelper(
		logger,
		&Config{
			Env:             "dev",
			Site:            "us-west-2",
			LimitPerService: 2,
		},
		&matcher,
	)
	_ = p.init()
	resourceSpans := ptrace.NewTraces().ResourceSpans().AppendEmpty()

	testSpan := ptrace.NewSpan()
	testSpan.Attributes().PutStr("http.url", "http://cart:8080/cart/anonymous-1")

	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(1e9 + 6e8)

	p.captureMetrics("robot-shop", "cart", &testSpan, &resourceSpans)
	assert.Equal(t, 1, p.requestContextsByService.Size())
	cache, _ := p.requestContextsByService.Load("robot-shop#cart")
	assert.Equal(t, 1, cache.Len())

	testSpan.Attributes().PutStr("http.url", "http://cart:8080/cart/anonymous-2")
	p.captureMetrics("robot-shop", "cart", &testSpan, &resourceSpans)
	assert.Equal(t, 2, cache.Len())

	testSpan.Attributes().PutStr("http.url", "http://cart:8080/cart/anonymous-3")
	p.captureMetrics("robot-shop", "cart", &testSpan, &resourceSpans)
	assert.Equal(t, 2, cache.Len())
	assert.NotNil(t, cache.Get("/cart/anonymous-1"))
	assert.NotNil(t, cache.Get("/cart/anonymous-2"))
	assert.Nil(t, cache.Get("/cart/anonymous-3"))
}
