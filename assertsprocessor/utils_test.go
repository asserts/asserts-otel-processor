package assertsprocessor

import (
	"context"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
	"go.uber.org/zap"
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
	assert.Equal(t, 0.4, computeLatency(&testSpan))
}

func TestCompileRequestContextRegexpsSuccess(t *testing.T) {
	logger, _ := zap.NewProduction()
	regexps, err := compileRequestContextRegexps(logger, &Config{
		RequestContextExps: &[]*MatcherDto{
			{
				attrName: "attribute1",
				regex:    "Foo",
			},
			{
				attrName: "attribute2",
				regex:    "Bar.+",
			},
		},
	})
	assert.Nil(t, err)
	assert.NotNil(t, regexps)
	assert.Equal(t, 2, len(*regexps))

	regExp := (*regexps)[0].regex
	assert.NotNil(t, regExp)
	assert.Equal(t, "attribute1", (*regexps)[0].attrName)
	assert.True(t, regExp.MatchString("Foo"))

	regExp = (*regexps)[1].regex
	assert.NotNil(t, regExp)
	assert.Equal(t, "attribute2", (*regexps)[1].attrName)
	assert.True(t, regExp.MatchString("Bart"))
}

func TestCompileRequestContextRegexpsFailure(t *testing.T) {
	logger, _ := zap.NewProduction()
	_, err := compileRequestContextRegexps(logger, &Config{
		RequestContextExps: &[]*MatcherDto{
			{
				attrName: "attribute1",
				regex:    "+",
			},
			{
				attrName: "attribute2",
				regex:    "Bar.+",
			},
		},
	})
	assert.NotNil(t, err)
}

func TestGetExpMatch(t *testing.T) {
	testSpan := ptrace.NewSpan()
	testSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	compile, _ := regexp.Compile("https?://.+?(/.+?/.+)")
	value := getRequest(&[]*Matcher{
		{
			attrName: "http.url",
			regex:    compile,
		},
	}, &testSpan)
	assert.Equal(t, "/342994379019/NodeJSPerf-WithLayer", value)
}

func TestGetExpNoMatch(t *testing.T) {
	testSpan := ptrace.NewSpan()
	testSpan.SetName("BackgroundJob")
	testSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	compile, _ := regexp.Compile("https?://foo.+?(/.+?/.+)")
	value := getRequest(&[]*Matcher{
		{
			attrName: "http.url",
			regex:    compile,
		},
	}, &testSpan)
	assert.Equal(t, "BackgroundJob", value)
}

func TestSpanIterator(t *testing.T) {
	ctx := context.Background()
	testTrace := ptrace.NewTraces()
	resourceSpans := testTrace.ResourceSpans().AppendEmpty()
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceName, "api-server")
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()

	rootSpan := scopeSpans.Spans().AppendEmpty()
	rootSpan.SetTraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 8})
	rootSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	rootSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	nestedSpan1 := scopeSpans.Spans().AppendEmpty()
	nestedSpan1.SetTraceID(rootSpan.TraceID())
	nestedSpan1.SetSpanID([8]byte{2, 1, 3, 4, 5, 6, 7, 8})
	nestedSpan1.SetParentSpanID(rootSpan.SpanID())
	nestedSpan1.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	exitSpan := scopeSpans.Spans().AppendEmpty()
	exitSpan.SetTraceID(rootSpan.TraceID())
	exitSpan.SetSpanID([8]byte{3, 1, 3, 4, 5, 6, 7, 8})
	exitSpan.SetKind(ptrace.SpanKindClient)
	exitSpan.SetParentSpanID(rootSpan.SpanID())
	exitSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	err := spanIterator(ctx, testTrace,
		func(context context.Context, spanStructs *resourceTraces) error {
			assert.Equal(t, "platform", spanStructs.namespace)
			assert.Equal(t, "api-server", spanStructs.service)
			assert.Equal(t, 1, len(*spanStructs.traceById))

			traceIdAsString := rootSpan.TraceID().String()
			trace := (*spanStructs.traceById)[traceIdAsString]
			assert.NotNil(t, trace)
			assert.Equal(t, &rootSpan, trace.rootSpan)
			assert.Equal(t, 1, len(trace.exitSpans))
			assert.Equal(t, 1, len(trace.internalSpans))
			assert.Equal(t, &exitSpan, trace.exitSpans[0])
			assert.Equal(t, &nestedSpan1, trace.internalSpans[0])
			return nil
		})
	assert.Nil(t, err)
}

func TestBuildTrace(t *testing.T) {
	expectedTrace := ptrace.NewTraces()
	resourceSpans := expectedTrace.ResourceSpans().AppendEmpty()
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceName, "api-server")
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()

	rootSpan := scopeSpans.Spans().AppendEmpty()
	rootSpan.SetTraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 8})
	rootSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	rootSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	nestedSpan1 := scopeSpans.Spans().AppendEmpty()
	nestedSpan1.SetTraceID(rootSpan.TraceID())
	nestedSpan1.SetSpanID([8]byte{2, 1, 3, 4, 5, 6, 7, 8})
	nestedSpan1.SetParentSpanID(rootSpan.SpanID())
	nestedSpan1.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	exitSpan := scopeSpans.Spans().AppendEmpty()
	exitSpan.SetTraceID(rootSpan.TraceID())
	exitSpan.SetSpanID([8]byte{3, 1, 3, 4, 5, 6, 7, 8})
	exitSpan.SetKind(ptrace.SpanKindClient)
	exitSpan.SetParentSpanID(rootSpan.SpanID())
	exitSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	assert.Equal(t, &expectedTrace, buildTrace(&traceStruct{
		resourceSpan:  &resourceSpans,
		rootSpan:      &rootSpan,
		internalSpans: []*ptrace.Span{&nestedSpan1},
		exitSpans:     []*ptrace.Span{&exitSpan},
	}))
}
