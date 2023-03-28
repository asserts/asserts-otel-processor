package assertsprocessor

import (
	"context"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
)

func buildEntityKey(config *Config, namespace string, service string) EntityKeyDto {
	return EntityKeyDto{
		Type: "Service",
		Name: service,
		Scope: map[string]string{
			"env": config.Env, "site": config.Site, "namespace": namespace,
		},
	}
}

func computeLatency(span *ptrace.Span) float64 {
	return float64(span.EndTimestamp()-span.StartTimestamp()) / 1e9
}

func spanHasError(span *ptrace.Span) bool {
	return span.Status().Code() == ptrace.StatusCodeError
}

type resourceTraces struct {
	traceById *map[string]*traceStruct
	namespace string
	service   string
}

type traceStruct struct {
	resourceSpan     *ptrace.ResourceSpans
	requestKey       *RequestKey
	latency          float64
	isSlow           bool
	latencyThreshold float64
	rootSpan         *ptrace.Span
	internalSpans    []*ptrace.Span
	entrySpans       []*ptrace.Span
	exitSpans        []*ptrace.Span
}

func (t *traceStruct) getMainSpan() *ptrace.Span {
	// A distributed trace will have only one root span. Trace fragments that come from a downstream service
	// will not have a root span. In such a scenario, use the first entry or exit span as the main span
	if t.rootSpan != nil {
		return t.rootSpan
	} else if len(t.entrySpans) > 0 {
		return t.entrySpans[0]
	} else if len(t.exitSpans) > 0 {
		return t.exitSpans[0]
	}
	return nil
}

func (t *traceStruct) hasError() bool {
	if t.rootSpan != nil && spanHasError(t.rootSpan) {
		return true
	}

	for _, span := range t.entrySpans {
		if spanHasError(span) {
			return true
		}
	}

	for _, span := range t.exitSpans {
		if spanHasError(span) {
			return true
		}
	}
	return false
}

func spanIterator(ctx context.Context, traces ptrace.Traces,
	callback func(context.Context, *resourceTraces) error) error {
	for i := 0; i < traces.ResourceSpans().Len(); i++ {
		resources := traces.ResourceSpans().At(i)
		resourceAttributes := resources.Resource().Attributes()

		// service is a required attribute
		serviceAttr, found := resourceAttributes.Get(conventions.AttributeServiceName)
		if !found {
			continue
		}

		// namespace is an optional attribute
		var namespace string
		namespaceAttr, found := resourceAttributes.Get(conventions.AttributeServiceNamespace)
		if found {
			namespace = namespaceAttr.Str()
		}
		serviceName := serviceAttr.Str()

		var tracesInResource = resourceTraces{}
		tracesInResource.traceById = &map[string]*traceStruct{}
		tracesInResource.namespace = namespace
		tracesInResource.service = serviceName
		scopes := resources.ScopeSpans()
		for j := 0; j < scopes.Len(); j++ {
			scope := scopes.At(j)
			spans := scope.Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				traceID := span.TraceID().String()
				t := (*tracesInResource.traceById)[traceID]
				if t == nil {
					t = &traceStruct{
						resourceSpan: &resources,
					}
					(*tracesInResource.traceById)[traceID] = t
				}
				if isRootSpan(&span) {
					t.rootSpan = &span
				} else if isEntrySpan(&span) {
					t.entrySpans = append(t.entrySpans, &span)
				} else if isExitSpan(&span) {
					t.exitSpans = append(t.exitSpans, &span)
				} else {
					t.internalSpans = append(t.internalSpans, &span)
				}
			}
		}

		if err := callback(ctx, &tracesInResource); err != nil {
			return err
		}
	}
	return nil
}

func buildTrace(trace *traceStruct) *ptrace.Traces {
	newTrace := ptrace.NewTraces()
	rs := newTrace.ResourceSpans().AppendEmpty()
	trace.resourceSpan.Resource().CopyTo(rs.Resource())
	ils := rs.ScopeSpans().AppendEmpty()

	if trace.rootSpan != nil {
		rootSpan := ils.Spans().AppendEmpty()
		trace.rootSpan.CopyTo(rootSpan)
	}

	for _, span := range trace.internalSpans {
		sp := ils.Spans().AppendEmpty()
		span.CopyTo(sp)
	}

	for _, span := range trace.entrySpans {
		sp := ils.Spans().AppendEmpty()
		span.CopyTo(sp)
	}

	for _, span := range trace.exitSpans {
		sp := ils.Spans().AppendEmpty()
		span.CopyTo(sp)
	}

	return &newTrace
}

func isEntrySpan(span *ptrace.Span) bool {
	return span.Kind() == ptrace.SpanKindServer || span.Kind() == ptrace.SpanKindConsumer
}

func isExitSpan(span *ptrace.Span) bool {
	return span.Kind() == ptrace.SpanKindClient || span.Kind() == ptrace.SpanKindProducer
}

func isRootSpan(span *ptrace.Span) bool {
	return span.ParentSpanID().IsEmpty()
}
