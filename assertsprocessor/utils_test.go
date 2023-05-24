package assertsprocessor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
)

func TestGetServiceKey(t *testing.T) {
	assert.Equal(t, "service", getServiceKey("", "service"))
	assert.Equal(t, "namespace#service", getServiceKey("namespace", "service"))
}

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

func TestSpanIterator(t *testing.T) {
	testTrace := ptrace.NewTraces()
	resourceSpans := testTrace.ResourceSpans().AppendEmpty()
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceName, "api-server")
	resourceSpans.Resource().Attributes().PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()

	rootSpan := scopeSpans.Spans().AppendEmpty()
	rootSpan.SetTraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 8})
	rootSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	rootSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	nestedSpan := scopeSpans.Spans().AppendEmpty()
	nestedSpan.SetTraceID(rootSpan.TraceID())
	nestedSpan.SetSpanID([8]byte{2, 1, 3, 4, 5, 6, 7, 8})
	nestedSpan.SetParentSpanID(rootSpan.SpanID())
	nestedSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	exitSpan := scopeSpans.Spans().AppendEmpty()
	exitSpan.SetTraceID(rootSpan.TraceID())
	exitSpan.SetSpanID([8]byte{3, 1, 3, 4, 5, 6, 7, 8})
	exitSpan.SetKind(ptrace.SpanKindClient)
	exitSpan.SetParentSpanID(rootSpan.SpanID())
	exitSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	traces := convertToTraces(testTrace)
	assert.Equal(t, 1, len(traces))
	assert.Equal(t, 1, len(traces[0].segments))

	ts := traces[0].segments[0]
	assert.Equal(t, rootSpan.TraceID().String(), ts.getMainSpan().TraceID().String())
	assert.Equal(t, "platform", ts.namespace)
	assert.Equal(t, "api-server", ts.service)
	assert.Equal(t, &rootSpan, ts.rootSpan)
	assert.Equal(t, 1, len(ts.exitSpans))
	assert.Equal(t, 1, len(ts.internalSpans))
	assert.Equal(t, &exitSpan, ts.exitSpans[0])
	assert.Equal(t, &nestedSpan, ts.internalSpans[0])
}

func TestSpansOfATraceSplitAcrossMultipleResourceSpans(t *testing.T) {
	testTrace := ptrace.NewTraces()
	resourceSpans1 := testTrace.ResourceSpans().AppendEmpty()
	resourceSpans1.Resource().Attributes().PutStr(conventions.AttributeServiceName, "api-server")
	resourceSpans1.Resource().Attributes().PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans1 := resourceSpans1.ScopeSpans().AppendEmpty()

	rootSpan := scopeSpans1.Spans().AppendEmpty()
	rootSpan.SetTraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 8})
	rootSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	rootSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	resourceSpans2 := testTrace.ResourceSpans().AppendEmpty()
	resourceSpans2.Resource().Attributes().PutStr(conventions.AttributeServiceName, "api-server")
	resourceSpans2.Resource().Attributes().PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans2 := resourceSpans2.ScopeSpans().AppendEmpty()

	nestedSpan := scopeSpans2.Spans().AppendEmpty()
	nestedSpan.SetTraceID(rootSpan.TraceID())
	nestedSpan.SetSpanID([8]byte{2, 1, 3, 4, 5, 6, 7, 8})
	nestedSpan.SetParentSpanID(rootSpan.SpanID())
	nestedSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	resourceSpans3 := testTrace.ResourceSpans().AppendEmpty()
	resourceSpans3.Resource().Attributes().PutStr(conventions.AttributeServiceName, "api-server")
	resourceSpans3.Resource().Attributes().PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans3 := resourceSpans3.ScopeSpans().AppendEmpty()

	exitSpan := scopeSpans3.Spans().AppendEmpty()
	exitSpan.SetTraceID(rootSpan.TraceID())
	exitSpan.SetSpanID([8]byte{3, 1, 3, 4, 5, 6, 7, 8})
	exitSpan.SetKind(ptrace.SpanKindClient)
	exitSpan.SetParentSpanID(rootSpan.SpanID())
	exitSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	traces := convertToTraces(testTrace)
	assert.Equal(t, 1, len(traces))
	assert.Equal(t, 1, len(traces[0].segments))

	ts := traces[0].segments[0]
	assert.Equal(t, rootSpan.TraceID().String(), ts.getMainSpan().TraceID().String())
	assert.Equal(t, "platform", ts.namespace)
	assert.Equal(t, "api-server", ts.service)
	assert.Equal(t, &rootSpan, ts.rootSpan)
	assert.Equal(t, 1, len(ts.exitSpans))
	assert.Equal(t, 1, len(ts.internalSpans))
	assert.Equal(t, &exitSpan, ts.exitSpans[0])
	assert.Equal(t, &nestedSpan, ts.internalSpans[0])
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

	exitSpan := scopeSpans.Spans().AppendEmpty()
	exitSpan.SetTraceID(rootSpan.TraceID())
	exitSpan.SetSpanID([8]byte{3, 1, 3, 4, 5, 6, 7, 8})
	exitSpan.SetKind(ptrace.SpanKindClient)
	exitSpan.SetParentSpanID(rootSpan.SpanID())
	exitSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	nestedSpan1 := scopeSpans.Spans().AppendEmpty()
	nestedSpan1.SetTraceID(rootSpan.TraceID())
	nestedSpan1.SetSpanID([8]byte{2, 1, 3, 4, 5, 6, 7, 8})
	nestedSpan1.SetParentSpanID(rootSpan.SpanID())
	nestedSpan1.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	tr := newTrace(
		&traceSegment{
			resourceSpans: &resourceSpans,
			rootSpan:      &rootSpan,
			internalSpans: []*ptrace.Span{&nestedSpan1},
			exitSpans:     []*ptrace.Span{&exitSpan},
		},
	)
	assert.Equal(t, &expectedTrace, buildTrace(tr))
}
