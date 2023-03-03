package assertsprocessor

import (
	"context"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
	"go.uber.org/zap"
	"regexp"
	"testing"
)

func TestBuildEntityKey(t *testing.T) {
	assert.Equal(t, EntityKeyDto{
		Type: "Service", Name: "payment-service",
		Scope: map[string]string{"env": "dev", "site": "us-west-2", "namespace": "payments"},
	}, buildEntityKey(&Config{
		Env: "dev", Site: "us-west-2",
	}, "payments", "payment-service"))
}

func TestComputeLatency(t *testing.T) {
	testSpan := ptrace.NewSpan()
	testSpan.SetStartTimestamp(1e9)
	testSpan.SetEndTimestamp(1e9 + 4e8)
	assert.Equal(t, 0.4, computeLatency(testSpan))
}

func TestCompileRequestContextRegexpsSuccess(t *testing.T) {
	logger, _ := zap.NewProduction()
	regexps, err := compileRequestContextRegexps(logger, &Config{
		RequestContextExps: &map[string]string{
			"attribute1": "Foo",
			"attribute2": "Bar.+",
		},
	})
	assert.Nil(t, err)
	assert.NotNil(t, regexps)
	assert.Equal(t, 2, len(*regexps))

	regExp := (*regexps)["attribute1"]
	assert.NotNil(t, regExp)
	assert.True(t, regExp.MatchString("Foo"))

	regExp = (*regexps)["attribute2"]
	assert.NotNil(t, regExp)
	assert.True(t, regExp.MatchString("Bart"))
}

func TestCompileRequestContextRegexpsFailure(t *testing.T) {
	logger, _ := zap.NewProduction()
	_, err := compileRequestContextRegexps(logger, &Config{
		RequestContextExps: &map[string]string{
			"attribute1": "+",
			"attribute2": "Bar.+",
		},
	})
	assert.NotNil(t, err)
}

func TestGetExpMatch(t *testing.T) {
	testSpan := ptrace.NewSpan()
	testSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	compile, _ := regexp.Compile("https?://.+?(/.+?/.+)")
	value := getRequest(&map[string]*regexp.Regexp{
		"http.url": compile,
	}, testSpan)
	assert.Equal(t, "/342994379019/NodeJSPerf-WithLayer", value)
}

func TestGetExpNoMatch(t *testing.T) {
	testSpan := ptrace.NewSpan()
	testSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	compile, _ := regexp.Compile("https?://foo.+?(/.+?/.+)")
	value := getRequest(&map[string]*regexp.Regexp{
		"http.url": compile,
	}, testSpan)
	assert.Equal(t, "", value)
}

func TestSpanIterator(t *testing.T) {
	ctx := context.Background()
	testTrace := ptrace.NewTraces()
	resourceSpans := testTrace.ResourceSpans().AppendEmpty()
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceName, "api-server")
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()

	rootSpan := scopeSpans.Spans().AppendEmpty()
	rootSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	rootSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	nestedSpan1 := scopeSpans.Spans().AppendEmpty()
	nestedSpan1.SetSpanID([8]byte{2, 1, 3, 4, 5, 6, 7, 8})
	nestedSpan1.SetParentSpanID(rootSpan.SpanID())
	nestedSpan1.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	nestedSpan2 := scopeSpans.Spans().AppendEmpty()
	nestedSpan2.SetSpanID([8]byte{3, 1, 3, 4, 5, 6, 7, 8})
	nestedSpan2.SetParentSpanID(rootSpan.SpanID())
	nestedSpan2.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	production, _ := zap.NewProduction()
	err := spanIterator(production, ctx, testTrace,
		func(context context.Context, trace ptrace.Traces, traceId string, spanStructs *resourceSpanGroup) error {
			assert.Equal(t, "platform", spanStructs.namespace)
			assert.Equal(t, "api-server", spanStructs.service)
			assert.Equal(t, 1, len(spanStructs.rootSpans))
			assert.Equal(t, rootSpan, spanStructs.rootSpans[0])
			assert.Equal(t, 2, len(spanStructs.nestedSpans))
			assert.Equal(t, nestedSpan1, spanStructs.nestedSpans[0])
			assert.Equal(t, nestedSpan2, spanStructs.nestedSpans[1])
			return nil
		})
	assert.Nil(t, err)
}
