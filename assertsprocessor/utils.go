package assertsprocessor

import (
	"context"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
	"go.uber.org/zap"
	"regexp"
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

func compileRequestContextRegexps(logger *zap.Logger, config *Config) (*map[string]*regexp.Regexp, error) {
	logger.Info("consumer.Start compiling request context regexps")
	var exps = map[string]*regexp.Regexp{}
	if config.RequestContextExps != nil {
		for attName, matchExpString := range *config.RequestContextExps {
			compile, err := regexp.Compile(matchExpString)
			if err != nil {
				return nil, err
			}
			exps[attName] = compile
		}
	}
	logger.Debug("consumer.Start compiled request context regexps successfully")
	return &exps, nil
}

func getRequest(exps *map[string]*regexp.Regexp, span *ptrace.Span) string {
	for attName, regExp := range *exps {
		value, found := span.Attributes().Get(attName)
		if found {
			subMatch := regExp.FindStringSubmatch(value.AsString())
			if len(subMatch) >= 1 {
				return subMatch[1]
			}
		}
	}
	return span.Name()
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
	resourceSpan  *ptrace.ResourceSpans
	requestKey    *RequestKey
	latency       float64
	isSlow        bool
	rootSpan      *ptrace.Span
	internalSpans []*ptrace.Span
	exitSpans     []*ptrace.Span
}

func (t *traceStruct) hasError() bool {
	if spanHasError(t.rootSpan) {
		return true
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
		resourceSpans := traces.ResourceSpans().At(i)
		resourceAttributes := resourceSpans.Resource().Attributes()

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
		ilsSlice := resourceSpans.ScopeSpans()
		for j := 0; j < ilsSlice.Len(); j++ {
			ils := ilsSlice.At(j)
			spans := ils.Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				traceID := span.TraceID().String()
				t := (*tracesInResource.traceById)[traceID]
				if t == nil {
					t = &traceStruct{
						resourceSpan: &resourceSpans,
					}
					(*tracesInResource.traceById)[traceID] = t
				}
				if isRootSpan(&span) {
					t.rootSpan = &span
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

	rootSpan := ils.Spans().AppendEmpty()
	trace.rootSpan.CopyTo(rootSpan)

	for _, span := range trace.internalSpans {
		sp := ils.Spans().AppendEmpty()
		span.CopyTo(sp)
	}

	for _, span := range trace.exitSpans {
		sp := ils.Spans().AppendEmpty()
		span.CopyTo(sp)
	}

	return &newTrace
}

func isExitSpan(span *ptrace.Span) bool {
	return span.Kind() == ptrace.SpanKindClient || span.Kind() == ptrace.SpanKindProducer
}

func isRootSpan(span *ptrace.Span) bool {
	return span.ParentSpanID().IsEmpty()
}
