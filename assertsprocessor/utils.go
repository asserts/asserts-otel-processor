package assertsprocessor

import (
	"context"
	"go.opentelemetry.io/collector/pdata/pcommon"
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

func getExp(exps *map[string]regexp.Regexp, span ptrace.Span) string {
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

func hasError(ctx context.Context, traces ptrace.Traces) bool {
	var slice []int
	callback := func(ns string, service string, ctx context.Context, traces ptrace.Traces, span ptrace.Span) error {
		attributes := span.Attributes()
		attributes.Range(func(k string, v pcommon.Value) bool {
			if k == "error" && v.Bool() {
				slice = append(slice, 1)
				return false
			} else {
				return true
			}
		})
		return nil
	}
	_ = spanIterator(ctx, traces, callback)
	return len(slice) > 0
}

func spanIterator(ctx context.Context, traces ptrace.Traces,
	callback func(string, string, context.Context, ptrace.Traces, ptrace.Span) error) error {
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
		ilsSlice := resourceSpans.ScopeSpans()
		for j := 0; j < ilsSlice.Len(); j++ {
			ils := ilsSlice.At(j)
			spans := ils.Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				err := callback(namespace, serviceName, ctx, traces, span)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}
