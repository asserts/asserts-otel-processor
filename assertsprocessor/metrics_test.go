package assertsprocessor

import (
	"github.com/google/go-cmp/cmp"
	"github.com/jellydator/ttlcache/v3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/puzpuzpuz/xsync/v2"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"regexp"
	"testing"
	"time"
)

func TestBuildLabels(t *testing.T) {
	logger, _ := zap.NewProduction()
	p := metricHelper{
		logger: logger,
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
	expectedLabels[envLabel] = "dev"
	expectedLabels[siteLabel] = "us-west-2"
	expectedLabels[namespaceLabel] = "ride-services"
	expectedLabels[serviceLabel] = "payment"
	expectedLabels[requestContextLabel] = "GetItem"
	expectedLabels["rpc_service"] = "DynamoDb"
	expectedLabels["rpc_method"] = "GetItem"
	expectedLabels["aws_table_name"] = "ride-bookings"
	expectedLabels["aws_queue_url"] = ""
	expectedLabels["rpc_system"] = "aws-api"

	actualLabels := p.buildLabels("ride-services", "payment", "GetItem", &testSpan)
	assert.True(t, cmp.Equal(&expectedLabels, &actualLabels))
}

func TestCaptureMetrics(t *testing.T) {
	logger, _ := zap.NewProduction()
	compile, _ := regexp.Compile("(.+)")
	matcher := spanMatcher{
		spanAttrMatchers: []*spanAttrMatcher{
			{
				attrName: "rpc.method",
				regex:    compile,
			},
		},
	}
	p := metricHelper{
		logger: logger,
		config: &Config{
			Env:  "dev",
			Site: "us-west-2",
			CaptureAttributesInMetric: []string{"rpc.system", "rpc.service", "rpc.method",
				"aws.table.name", "aws.queue.url"},
			LimitPerService: 100,
		},
		spanMatcher:              &matcher,
		requestContextsByService: xsync.NewMapOf[*ttlcache.Cache[string, string]](),
	}
	_ = p.buildHistogram()
	testSpan := ptrace.NewSpan()
	testSpan.Attributes().PutStr("rpc.system", "aws-api")
	testSpan.Attributes().PutStr("rpc.service", "DynamoDb")
	testSpan.Attributes().PutStr("rpc.method", "GetItem")
	testSpan.Attributes().PutStr("aws.table.name", "ride-bookings")
	testSpan.Attributes().PutStr("aws.table.nameee", "ride-bookings")

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
	expectedLabels["aws_queue_url"] = ""
	expectedLabels["rpc_system"] = "aws-api"

	p.captureMetrics("ride-services", "payment", &testSpan)
	metric, err := p.latencyHistogram.GetMetricWith(expectedLabels)
	assert.Nil(t, err)
	assert.NotNil(t, metric)
}

func TestRequestContextCardinalityExplosion(t *testing.T) {
	logger, _ := zap.NewProduction()
	compile, _ := regexp.Compile("https?://.+?(/.+?/.+)")
	matcher := spanMatcher{
		spanAttrMatchers: []*spanAttrMatcher{
			{
				attrName: "http.url",
				regex:    compile,
			},
		},
	}
	p := metricHelper{
		logger: logger,
		config: &Config{
			Env:             "dev",
			Site:            "us-west-2",
			LimitPerService: 2,
		},
		spanMatcher:              &matcher,
		requestContextsByService: xsync.NewMapOf[*ttlcache.Cache[string, string]](),
	}
	_ = p.buildHistogram()
	testSpan := ptrace.NewSpan()
	testSpan.Attributes().PutStr("http.url", "http://cart:8080/cart/anonymous-1")

	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(1e9 + 6e8)

	p.captureMetrics("robot-shop", "cart", &testSpan)
	assert.Equal(t, 1, p.requestContextsByService.Size())
	cache, _ := p.requestContextsByService.Load("cart#robot-shop")
	assert.Equal(t, 1, cache.Len())

	testSpan.Attributes().PutStr("http.url", "http://cart:8080/cart/anonymous-2")
	p.captureMetrics("robot-shop", "cart", &testSpan)
	assert.Equal(t, 2, cache.Len())

	testSpan.Attributes().PutStr("http.url", "http://cart:8080/cart/anonymous-3")
	p.captureMetrics("robot-shop", "cart", &testSpan)
	assert.Equal(t, 2, cache.Len())
	assert.NotNil(t, cache.Get("/cart/anonymous-1"))
	assert.NotNil(t, cache.Get("/cart/anonymous-2"))
	assert.Nil(t, cache.Get("/cart/anonymous-3"))
}

func TestStartExporter(t *testing.T) {
	logger, _ := zap.NewProduction()
	p := metricHelper{
		logger: logger,
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
