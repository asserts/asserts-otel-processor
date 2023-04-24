package assertsprocessor

import (
	"go.opentelemetry.io/collector/pdata/ptrace"
	"regexp"
)

const (
	AssertsErrorTypeAttribute   = "asserts.error.type"
	AssertsRequestTypeAttribute = "asserts.request.type"
	AssertsRequestTypeInbound   = "inbound"
	AssertsRequestTypeOutbound  = "outbound"
)

type ErrorTypeConfig struct {
	ValueExpr string `mapstructure:"value_match_regex"`
	ErrorType string `mapstructure:"error_type"`
}

func (eTC *ErrorTypeConfig) compile() (*errorTypeCompiledConfig, error) {
	compile, err := regexp.Compile(eTC.ValueExpr)
	if err != nil {
		return nil, err
	} else {
		e := &errorTypeCompiledConfig{
			errorType:    eTC.ErrorType,
			valueMatcher: compile,
		}
		return e, nil
	}
}

type errorTypeCompiledConfig struct {
	valueMatcher *regexp.Regexp
	errorType    string
}

type spanEnrichmentProcessor struct {
	errorTypeConfigs map[string][]*errorTypeCompiledConfig
}

func buildEnrichmentProcessor(config *Config) *spanEnrichmentProcessor {
	processor := spanEnrichmentProcessor{
		errorTypeConfigs: map[string][]*errorTypeCompiledConfig{},
	}
	for attrName, errorConfigs := range config.ErrorTypeConfigs {
		processor.errorTypeConfigs[attrName] = make([]*errorTypeCompiledConfig, 0)
		for _, errorConfig := range errorConfigs {
			compile, _ := regexp.Compile(errorConfig.ValueExpr)
			processor.errorTypeConfigs[attrName] = append(processor.errorTypeConfigs[attrName],
				&errorTypeCompiledConfig{
					errorType:    errorConfig.ErrorType,
					valueMatcher: compile,
				})
		}
	}
	return &processor
}

func (ep *spanEnrichmentProcessor) enrichSpan(span *ptrace.Span) {
	ep.addErrorType(span)

	// Add request type
	kind := span.Kind()
	if kind == ptrace.SpanKindClient || kind == ptrace.SpanKindProducer {
		span.Attributes().PutStr(AssertsRequestTypeAttribute, AssertsRequestTypeOutbound)
	} else if kind == ptrace.SpanKindServer || kind == ptrace.SpanKindConsumer {
		span.Attributes().PutStr(AssertsRequestTypeAttribute, AssertsRequestTypeInbound)
	}
}

func (ep *spanEnrichmentProcessor) addErrorType(span *ptrace.Span) {
	for attrName, errorConfigs := range ep.errorTypeConfigs {
		value, present := span.Attributes().Get(attrName)
		if present {
			for _, errorConfig := range errorConfigs {
				if errorConfig.valueMatcher.MatchString(value.Str()) {
					span.Attributes().PutStr(AssertsErrorTypeAttribute, errorConfig.errorType)
					return
				}
			}
		}
	}
}
