package assertsprocessor

import (
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"regexp"
)

const (
	AssertsErrorTypeAttribute      = "asserts.error.type"
	AssertsRequestTypeAttribute    = "asserts.request.type"
	AssertsRequestContextAttribute = "asserts.request.context"
	AssertsRequestTypeInbound      = "inbound"
	AssertsRequestTypeOutbound     = "outbound"
	AssertsRequestTypeInternal     = "internal"
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

type spanEnrichmentProcessor interface {
	enrichSpan(namespace string, service string, span *ptrace.Span)
}

type spanEnrichmentProcessorImpl struct {
	logger           *zap.Logger
	errorTypeConfigs map[string][]*errorTypeCompiledConfig
	requestBuilder   requestContextBuilder
}

func buildEnrichmentProcessor(logger *zap.Logger, config *Config, requestBuilder requestContextBuilder) *spanEnrichmentProcessorImpl {
	processor := spanEnrichmentProcessorImpl{
		logger:           logger,
		errorTypeConfigs: map[string][]*errorTypeCompiledConfig{},
		requestBuilder:   requestBuilder,
	}
	for attrName, errorConfigs := range config.ErrorTypeConfigs {
		logger.Debug("Compiling error type configs for", zap.String("attribute", attrName))
		processor.errorTypeConfigs[attrName] = make([]*errorTypeCompiledConfig, 0)
		for _, errorConfig := range errorConfigs {
			compile, _ := regexp.Compile(errorConfig.ValueExpr)
			processor.errorTypeConfigs[attrName] = append(processor.errorTypeConfigs[attrName],
				&errorTypeCompiledConfig{
					errorType:    errorConfig.ErrorType,
					valueMatcher: compile,
				})
			logger.Debug("Compiled and added error type config",
				zap.String("error type", errorConfig.ErrorType),
				zap.String("attr value regexp", errorConfig.ValueExpr))
		}
	}
	return &processor
}

func (ep *spanEnrichmentProcessorImpl) enrichSpan(namespace string, service string, span *ptrace.Span) {
	ep.addRequestType(span)
	ep.addRequestContext(namespace, service, span)
	ep.addErrorType(span)
}

func (ep *spanEnrichmentProcessorImpl) addRequestType(span *ptrace.Span) {
	// Add request type
	kind := span.Kind()
	if kind == ptrace.SpanKindClient || kind == ptrace.SpanKindProducer {
		span.Attributes().PutStr(AssertsRequestTypeAttribute, AssertsRequestTypeOutbound)
	} else if kind == ptrace.SpanKindServer || kind == ptrace.SpanKindConsumer {
		span.Attributes().PutStr(AssertsRequestTypeAttribute, AssertsRequestTypeInbound)
	} else if kind == ptrace.SpanKindInternal {
		span.Attributes().PutStr(AssertsRequestTypeAttribute, AssertsRequestTypeInternal)
	}
}

func (ep *spanEnrichmentProcessorImpl) addRequestContext(namespace string, service string, span *ptrace.Span) {
	request := ep.requestBuilder.getRequest(span, namespace+"#"+service)
	span.Attributes().PutStr(AssertsRequestContextAttribute, request)
}

func (ep *spanEnrichmentProcessorImpl) addErrorType(span *ptrace.Span) {
	ep.logger.Debug("Adding error type", zap.String("span id", span.SpanID().String()))
	for attrName, errorConfigs := range ep.errorTypeConfigs {
		value, present := span.Attributes().Get(attrName)
		if present {
			stringValue := value.AsString()
			ep.logger.Debug("Matching error type config for",
				zap.String("span id", span.SpanID().String()),
				zap.String(attrName, stringValue))
			for _, errorConfig := range errorConfigs {
				if errorConfig.valueMatcher.MatchString(stringValue) {
					ep.logger.Debug("Matched",
						zap.String("span id", span.SpanID().String()),
						zap.String("expr", errorConfig.valueMatcher.String()),
						zap.String("value", stringValue))
					span.Attributes().PutStr(AssertsErrorTypeAttribute, errorConfig.errorType)
					ep.logger.Debug("Added error type",
						zap.String("span id", span.SpanID().String()),
						zap.String(attrName, stringValue),
						zap.String("error type", errorConfig.errorType))
					return
				} else {
					ep.logger.Debug("Did not match",
						zap.String("span id", span.SpanID().String()),
						zap.String("expr", errorConfig.valueMatcher.String()),
						zap.String("value", stringValue))
				}
			}
		} else {
			ep.logger.Debug("Attribute missing. Won't add error type",
				zap.String("span id", span.SpanID().String()),
				zap.String("attribute", attrName))
		}
	}
}
