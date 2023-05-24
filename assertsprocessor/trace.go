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

func (t *traceSegment) getSpans() []*ptrace.Span {
	spans := make([]*ptrace.Span, 0, len(t.entrySpans)+len(t.exitSpans))
	if t.rootSpan != nil {
		spans = append(spans, t.rootSpan)
	}
	spans = append(spans, t.entrySpans...)
	spans = append(spans, t.exitSpans...)
	return spans
}

func (t *traceSegment) getMainSpan() *ptrace.Span {
	// A distributed trace will have only one root span. Trace fragments that come from a downstream service
	// will not have a root span. In such a scenario, use the first entry or exit span as the main span
	for _, span := range t.getSpans() {
		return span
	}
	return nil
}

func (t *traceSegment) hasError() bool {
	for _, span := range t.getSpans() {
		if spanHasError(span) {
			return true
		}
	}
	return false
}

func newTrace(ts *traceSegment) *trace {
	return &trace{
		segments: []*traceSegment{
			ts,
		},
	}
}
