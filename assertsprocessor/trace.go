package assertsprocessor

import "go.opentelemetry.io/collector/pdata/ptrace"

type trace struct {
	segments []*traceSegment
}

type traceSegment struct {
	resourceSpans *ptrace.ResourceSpans
	namespace     string
	service       string
	requestKey    *RequestKey
	latency       float64
	rootSpan      *ptrace.Span
	internalSpans []*ptrace.Span
	entrySpans    []*ptrace.Span
	exitSpans     []*ptrace.Span
}

func (ts *traceSegment) getSpans() []*ptrace.Span {
	spans := make([]*ptrace.Span, 0, len(ts.entrySpans)+len(ts.exitSpans))
	if ts.rootSpan != nil {
		spans = append(spans, ts.rootSpan)
	}
	spans = append(spans, ts.entrySpans...)
	spans = append(spans, ts.exitSpans...)
	return spans
}

func (ts *traceSegment) getMainSpan() *ptrace.Span {
	// A distributed trace will have only one root span. Trace fragments that come from a downstream service
	// will not have a root span. In such a scenario, use the first entry or exit span as the main span
	for _, span := range ts.getSpans() {
		return span
	}
	return nil
}

func newTrace(traceSegments ...*traceSegment) *trace {
	return &trace{
		segments: traceSegments,
	}
}
