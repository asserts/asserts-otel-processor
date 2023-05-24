package assertsprocessor

import (
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
)

func getServiceKey(namespace string, service string) string {
	if namespace != "" {
		return namespace + "#" + service
	} else {
		return service
	}
}

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

func convertToTraces(traces ptrace.Traces) []*trace {
	var traceById = map[string]map[string]*traceSegment{}
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

		serviceKey := getServiceKey(namespace, serviceName)
		scopes := resources.ScopeSpans()
		for j := 0; j < scopes.Len(); j++ {
			scope := scopes.At(j)
			spans := scope.Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				traceID := span.TraceID().String()

				tr, exists := traceById[traceID]
				if !exists {
					tr = map[string]*traceSegment{}
					traceById[traceID] = tr
				}

				ts, exists := tr[serviceKey]
				if !exists {
					ts = &traceSegment{
						resourceSpans: &resources,
						namespace:     namespace,
						service:       serviceName,
					}
					tr[serviceKey] = ts
				}

				if isRootSpan(&span) {
					ts.rootSpan = &span
				} else if isEntrySpan(&span) {
					ts.entrySpans = append(ts.entrySpans, &span)
				} else if isExitSpan(&span) {
					ts.exitSpans = append(ts.exitSpans, &span)
				} else {
					ts.internalSpans = append(ts.internalSpans, &span)
				}
			}
		}
	}

	allTraces := make([]*trace, 0)
	for _, tr := range traceById {
		aTrace := &trace{}
		for _, ts := range tr {
			aTrace.segments = append(aTrace.segments, ts)
		}
		allTraces = append(allTraces, aTrace)
	}

	return allTraces
}

func buildTrace(tr *trace) *ptrace.Traces {
	newTrace := ptrace.NewTraces()
	for _, ts := range tr.segments {
		rs := newTrace.ResourceSpans().AppendEmpty()
		ts.resourceSpans.Resource().CopyTo(rs.Resource())
		ils := rs.ScopeSpans().AppendEmpty()

		spans := ts.getSpans()
		spans = append(spans, ts.internalSpans...)

		for _, span := range spans {
			sp := ils.Spans().AppendEmpty()
			span.CopyTo(sp)
		}
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
