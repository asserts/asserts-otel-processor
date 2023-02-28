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
	value := getExp(&map[string]regexp.Regexp{
		"http.url": *compile,
	}, testSpan)
	assert.Equal(t, "/342994379019/NodeJSPerf-WithLayer", value)
}

func TestGetExpNoMatch(t *testing.T) {
	testSpan := ptrace.NewSpan()
	testSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	compile, _ := regexp.Compile("https?://foo.+?(/.+?/.+)")
	value := getExp(&map[string]regexp.Regexp{
		"http.url": *compile,
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

	testSpan := scopeSpans.Spans().AppendEmpty()

	testSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	testSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	var expected = map[string]any{
		"namespace": "platform", "service": "api-server", "ctx": ctx, "trace": testTrace, "span": testSpan,
	}

	var actual = map[string]any{}

	err := spanIterator(ctx, testTrace, func(ns string, name string, ctx context.Context, trace ptrace.Traces, span ptrace.Span) error {
		actual["namespace"] = ns
		actual["service"] = name
		actual["ctx"] = ctx
		actual["trace"] = trace
		actual["span"] = span
		return nil
	})
	assert.Nil(t, err)
	assert.Equal(t, expected, actual)
}

func TestTraceHasErrorFalse(t *testing.T) {
	ctx := context.Background()
	testTrace := ptrace.NewTraces()
	resourceSpans := testTrace.ResourceSpans().AppendEmpty()
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceName, "api-server")
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()

	testSpan := scopeSpans.Spans().AppendEmpty()

	testSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	testSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	assert.False(t, traceHasError(ctx, testTrace))
}

func TestTraceHasErrorTrue(t *testing.T) {
	ctx := context.Background()
	testTrace := ptrace.NewTraces()
	resourceSpans := testTrace.ResourceSpans().AppendEmpty()
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceName, "api-server")
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()

	testSpan := scopeSpans.Spans().AppendEmpty()

	testSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	testSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")
	testSpan.Attributes().PutBool("error", true)

	assert.True(t, traceHasError(ctx, testTrace))
}
