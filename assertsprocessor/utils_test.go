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

func TestConvertToTraces(t *testing.T) {
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

func TestConvertToTracesMultipleResourceSpans2OneSegment(t *testing.T) {
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

func TestConvertToTracesOneTraceMultipleSegments(t *testing.T) {
	testTrace := ptrace.NewTraces()
	resourceSpans1 := testTrace.ResourceSpans().AppendEmpty()
	resourceSpans1.Resource().Attributes().PutStr(conventions.AttributeServiceName, "api-server")
	resourceSpans1.Resource().Attributes().PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans1 := resourceSpans1.ScopeSpans().AppendEmpty()

	rootSpan := scopeSpans1.Spans().AppendEmpty()
	rootSpan.SetTraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 8})
	rootSpan.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	rootSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	exitSpan := scopeSpans1.Spans().AppendEmpty()
	exitSpan.SetTraceID(rootSpan.TraceID())
	exitSpan.SetSpanID([8]byte{3, 1, 3, 4, 5, 6, 7, 8})
	exitSpan.SetKind(ptrace.SpanKindClient)
	exitSpan.SetParentSpanID(rootSpan.SpanID())
	exitSpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	resourceSpans2 := testTrace.ResourceSpans().AppendEmpty()
	resourceSpans2.Resource().Attributes().PutStr(conventions.AttributeServiceName, "model-builder")
	resourceSpans2.Resource().Attributes().PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans2 := resourceSpans2.ScopeSpans().AppendEmpty()

	entrySpan := scopeSpans2.Spans().AppendEmpty()
	entrySpan.SetTraceID(rootSpan.TraceID())
	entrySpan.SetSpanID([8]byte{2, 1, 3, 4, 5, 6, 7, 8})
	entrySpan.SetKind(ptrace.SpanKindServer)
	entrySpan.SetParentSpanID(exitSpan.SpanID())
	entrySpan.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	traces := convertToTraces(testTrace)
	assert.Equal(t, 1, len(traces))
	assert.Equal(t, 2, len(traces[0].segments))

	ts1 := traces[0].segments[0]
	assert.Equal(t, rootSpan.TraceID().String(), ts1.getMainSpan().TraceID().String())
	assert.Equal(t, "platform", ts1.namespace)
	assert.Equal(t, "api-server", ts1.service)
	assert.Equal(t, &rootSpan, ts1.rootSpan)
	assert.Equal(t, 1, len(ts1.exitSpans))
	assert.Equal(t, &exitSpan, ts1.exitSpans[0])

	ts2 := traces[0].segments[1]
	assert.Equal(t, rootSpan.TraceID().String(), ts2.getMainSpan().TraceID().String())
	assert.Equal(t, "platform", ts2.namespace)
	assert.Equal(t, "model-builder", ts2.service)
	assert.Equal(t, 1, len(ts2.entrySpans))
	assert.Equal(t, &entrySpan, ts2.entrySpans[0])
}

func TestConvertToTracesMultipleTraces(t *testing.T) {
	testTrace := ptrace.NewTraces()
	resourceSpans1 := testTrace.ResourceSpans().AppendEmpty()
	resourceSpans1.Resource().Attributes().PutStr(conventions.AttributeServiceName, "api-server")
	resourceSpans1.Resource().Attributes().PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans1 := resourceSpans1.ScopeSpans().AppendEmpty()

	rootSpan1 := scopeSpans1.Spans().AppendEmpty()
	rootSpan1.SetTraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 8})
	rootSpan1.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	rootSpan1.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	rootSpan2 := scopeSpans1.Spans().AppendEmpty()
	rootSpan2.SetTraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 9})
	rootSpan2.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 9})
	rootSpan2.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	resourceSpans2 := testTrace.ResourceSpans().AppendEmpty()
	resourceSpans2.Resource().Attributes().PutStr(conventions.AttributeServiceName, "model-builder")
	resourceSpans2.Resource().Attributes().PutStr(conventions.AttributeServiceNamespace, "platform")
	scopeSpans2 := resourceSpans2.ScopeSpans().AppendEmpty()

	childSpan1 := scopeSpans2.Spans().AppendEmpty()
	childSpan1.SetTraceID(rootSpan1.TraceID())
	childSpan1.SetSpanID([8]byte{2, 1, 3, 4, 5, 6, 7, 8})
	childSpan1.SetKind(ptrace.SpanKindServer)
	childSpan1.SetParentSpanID(rootSpan1.SpanID())
	childSpan1.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	childSpan2 := scopeSpans2.Spans().AppendEmpty()
	childSpan2.SetTraceID(rootSpan2.TraceID())
	childSpan2.SetSpanID([8]byte{2, 1, 3, 4, 5, 6, 7, 9})
	childSpan2.SetKind(ptrace.SpanKindServer)
	childSpan2.SetParentSpanID(rootSpan2.SpanID())
	childSpan2.Attributes().PutStr("http.url", "https://sqs.us-west-2.amazonaws.com/342994379019/NodeJSPerf-WithLayer")

	traces := convertToTraces(testTrace)
	assert.Equal(t, 2, len(traces))
	assert.Equal(t, 2, len(traces[0].segments))
	assert.Equal(t, 2, len(traces[1].segments))

	traceById := map[string]*trace{}
	for _, tr := range traces {
		traceById[tr.segments[0].getMainSpan().TraceID().String()] = tr
	}

	tr1 := traceById[rootSpan1.TraceID().String()]
	tr2 := traceById[rootSpan2.TraceID().String()]

	ts1 := tr1.segments[0]
	assert.Equal(t, rootSpan1.TraceID().String(), ts1.getMainSpan().TraceID().String())
	assert.Equal(t, "platform", ts1.namespace)
	assert.Equal(t, "api-server", ts1.service)
	assert.Equal(t, &rootSpan1, ts1.rootSpan)

	ts2 := tr1.segments[1]
	assert.Equal(t, rootSpan1.TraceID().String(), ts2.getMainSpan().TraceID().String())
	assert.Equal(t, "platform", ts2.namespace)
	assert.Equal(t, "model-builder", ts2.service)
	assert.Equal(t, 1, len(ts2.entrySpans))
	assert.Equal(t, &childSpan1, ts2.entrySpans[0])

	ts3 := tr2.segments[0]
	assert.Equal(t, rootSpan2.TraceID().String(), ts3.getMainSpan().TraceID().String())
	assert.Equal(t, "platform", ts3.namespace)
	assert.Equal(t, "api-server", ts3.service)
	assert.Equal(t, &rootSpan2, ts3.rootSpan)

	ts4 := tr2.segments[1]
	assert.Equal(t, rootSpan2.TraceID().String(), ts4.getMainSpan().TraceID().String())
	assert.Equal(t, "platform", ts4.namespace)
	assert.Equal(t, "model-builder", ts4.service)
	assert.Equal(t, 1, len(ts4.entrySpans))
	assert.Equal(t, &childSpan2, ts4.entrySpans[0])
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
