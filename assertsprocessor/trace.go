package assertsprocessor

import "go.opentelemetry.io/collector/pdata/ptrace"

type trace struct {
	segments []*traceSegment
}

func (tr *trace) getSpanCount() int {
	var count int
	for _, ts := range tr.segments {
		count += len(ts.entrySpans) + len(ts.exitSpans) + len(ts.internalSpans)
		if ts.rootSpan != nil {
			count += 1
		}
	}
	return count
}

type traceSegment struct {
	resourceSpans    *ptrace.ResourceSpans
	namespace        string
	service          string
	requestKey       *RequestKey
	latency          float64
	rootSpan         *ptrace.Span
	internalSpans    []*ptrace.Span
	entrySpans       []*ptrace.Span
	exitSpans        []*ptrace.Span
	nonInternalSpans []*ptrace.Span
}

func (ts *traceSegment) getNonInternalSpans() []*ptrace.Span {
	if ts.nonInternalSpans != nil {
		return ts.nonInternalSpans
	}

	ts.nonInternalSpans = make([]*ptrace.Span, 0, len(ts.entrySpans)+len(ts.exitSpans))
	if ts.rootSpan != nil {
		ts.nonInternalSpans = append(ts.nonInternalSpans, ts.rootSpan)
	}
	ts.nonInternalSpans = append(ts.nonInternalSpans, ts.entrySpans...)
	ts.nonInternalSpans = append(ts.nonInternalSpans, ts.exitSpans...)

	return ts.nonInternalSpans
}

func (ts *traceSegment) getMainSpan() *ptrace.Span {
	// A distributed trace will have only one root span. Trace fragments that come from a downstream service
	// will not have a root span. In such a scenario, use the first entry or exit span as the main span
	for _, span := range ts.getNonInternalSpans() {
		return span
	}
	return nil
}

func newTrace(traceSegments ...*traceSegment) *trace {
	return &trace{
		segments: traceSegments,
	}
}
