package assertsprocessor

import (
	"context"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	semconv "go.opentelemetry.io/collector/semconv/v1.16.0"
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

func computeLatency(span ptrace.Span) float64 {
	return float64(span.EndTimestamp()-span.StartTimestamp()) / 1e9
}

func compileRequestContextRegexps(logger *zap.Logger, config *Config) (*map[string]regexp.Regexp, error) {
	logger.Info("consumer.Start compiling request context regexps")
	var exps = map[string]regexp.Regexp{}
	if config.RequestContextExps != nil {
		for attName, matchExpString := range *config.RequestContextExps {
			compile, err := regexp.Compile(matchExpString)
			if err != nil {
				return nil, err
			}
			exps[attName] = *compile
		}
	}
	logger.Debug("consumer.Start compiled request context regexps successfully")
	return &exps, nil
}

func getRequest(exps *map[string]regexp.Regexp, span ptrace.Span) string {
	for attName, regExp := range *exps {
		value, found := span.Attributes().Get(attName)
		if found {
			subMatch := regExp.FindStringSubmatch(value.AsString())
			if len(subMatch) >= 1 {
				return subMatch[1]
			}
		}
	}
	return ""
}

func spanHasError(span *ptrace.Span, logger *zap.Logger) bool {
	var slice = make([]int, 0)
	var attributeNames = ""
	span.Attributes().Range(func(k string, v pcommon.Value) bool {
		attributeNames = attributeNames + ", " + k
		if k == "error" {
			logger.Info("Error Flag",
				zap.String("spanId", span.SpanID().String()),
				zap.String("error", v.AsString()))
			if v.AsString() == "true" {
				slice = append(slice, 1)
				return false
			}
		} else if k == semconv.AttributeOtelStatusCode {
			logger.Info("Error Flag",
				zap.String("spanId", span.SpanID().String()),
				zap.String("error", v.AsString()))
			if v.Str() == semconv.AttributeOtelStatusCodeError {
				slice = append(slice, 1)
				return false
			}
		}
		return true
	})
	logger.Info("Span attributes",
		zap.String("spanId", span.SpanID().String()),
		zap.String("attributes", attributeNames))
	return len(slice) > 0
}

type resourceSpanGroup struct {
	resourceAttributes *pcommon.Map
	rootSpans          []*ptrace.Span
	nestedSpans        []*ptrace.Span
	namespace          string
	service            string
}

func (ss *resourceSpanGroup) hasError(logger *zap.Logger) bool {
	for _, span := range ss.rootSpans {
		if spanHasError(span, logger) {
			return true
		}
	}

	for _, span := range ss.nestedSpans {
		if spanHasError(span, logger) {
			return true
		}
	}
	return false
}

func spanIterator(logger *zap.Logger, ctx context.Context, traces ptrace.Traces,
	callback func(context.Context, ptrace.Traces, string, *resourceSpanGroup) error) error {
	var spanSet = &resourceSpanGroup{}
	traceID := ""
	for i := 0; i < traces.ResourceSpans().Len(); i++ {
		resourceSpans := traces.ResourceSpans().At(i)
		resourceAttributes := resourceSpans.Resource().Attributes()

		var namespace string
		namespaceAttr, found := resourceAttributes.Get(conventions.AttributeServiceNamespace)
		if found {
			namespace = namespaceAttr.Str()
		}

		serviceAttr, found := resourceAttributes.Get(conventions.AttributeServiceName)
		if !found {
			continue
		}
		serviceName := serviceAttr.Str()
		spanSet.namespace = namespace
		spanSet.service = serviceName
		ilsSlice := resourceSpans.ScopeSpans()
		for j := 0; j < ilsSlice.Len(); j++ {
			ils := ilsSlice.At(j)
			spans := ils.Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				traceID = span.TraceID().String()
				if span.ParentSpanID().IsEmpty() {
					spanSet.rootSpans = append(spanSet.rootSpans, &span)
				} else {
					spanSet.nestedSpans = append(spanSet.nestedSpans, &span)
				}
			}
		}
	}
	logger.Info("Span Group",
		zap.String("Trace Id", traceID),
		zap.Int("Root Spans", len(spanSet.rootSpans)),
		zap.Int("Nested Spans", len(spanSet.nestedSpans)))
	return callback(ctx, traces, traceID, spanSet)
}
