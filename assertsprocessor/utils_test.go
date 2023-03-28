package assertsprocessor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
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

func TestSpanHasError(t *testing.T) {
	testSpan := ptrace.NewSpan()
	assert.False(t, spanHasError(&testSpan))
	testSpan.Status().SetCode(ptrace.StatusCodeError)
	assert.True(t, spanHasError(&testSpan))
}

func TestGetMainSpan(t *testing.T) {
	rootSpan := ptrace.NewSpan()
	rootSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	entrySpan := ptrace.NewSpan()
	entrySpan.SetSpanID([8]byte{2, 1, 3, 4, 5, 6, 7, 8})
	exitSpan := ptrace.NewSpan()
	exitSpan.SetSpanID([8]byte{3, 1, 3, 4, 5, 6, 7, 8})

	ts1 := traceStruct{
		rootSpan:   &rootSpan,
		entrySpans: []*ptrace.Span{&entrySpan},
		exitSpans:  []*ptrace.Span{&exitSpan},
	}
	ts2 := traceStruct{
		entrySpans: []*ptrace.Span{&entrySpan},
		exitSpans:  []*ptrace.Span{&exitSpan},
	}
	ts3 := traceStruct{
		exitSpans: []*ptrace.Span{&exitSpan},
	}

	assert.Equal(t, &rootSpan, ts1.getMainSpan())
	assert.Equal(t, &entrySpan, ts2.getMainSpan())
	assert.Equal(t, &exitSpan, ts3.getMainSpan())
}

func TestHasError(t *testing.T) {
	rootSpan := ptrace.NewSpan()
	rootSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	entrySpan := ptrace.NewSpan()
	entrySpan.SetSpanID([8]byte{2, 1, 3, 4, 5, 6, 7, 8})
	exitSpan := ptrace.NewSpan()
	exitSpan.SetSpanID([8]byte{3, 1, 3, 4, 5, 6, 7, 8})

	ts1 := traceStruct{
		rootSpan:   &rootSpan,
		entrySpans: []*ptrace.Span{&entrySpan},
		exitSpans:  []*ptrace.Span{&exitSpan},
	}
	ts2 := traceStruct{
		entrySpans: []*ptrace.Span{&entrySpan},
		exitSpans:  []*ptrace.Span{&exitSpan},
	}
	ts3 := traceStruct{
		exitSpans: []*ptrace.Span{&exitSpan},
	}

	assert.False(t, ts1.hasError())
	rootSpan.Status().SetCode(ptrace.StatusCodeError)
	assert.True(t, ts1.hasError())

	assert.False(t, ts2.hasError())
	entrySpan.Status().SetCode(ptrace.StatusCodeError)
	assert.True(t, ts2.hasError())

	assert.False(t, ts3.hasError())
	exitSpan.Status().SetCode(ptrace.StatusCodeError)
	assert.True(t, ts3.hasError())
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
