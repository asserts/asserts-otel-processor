package assertsprocessor

import (
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"reflect"
	"sync"
)

const (
	AssertsErrorTypeAttribute      = "asserts.error.type"
	AssertsRequestTypeAttribute    = "asserts.request.type"
	AssertsRequestContextAttribute = "asserts.request.context"
	AssertsRequestTypeInbound      = "inbound"
	AssertsRequestTypeOutbound     = "outbound"
	AssertsRequestTypeInternal     = "internal"
)

type spanEnrichmentProcessor interface {
	enrichSpan(namespace string, service string, span *ptrace.Span)
}

type spanEnrichmentProcessorImpl struct {
	logger           *zap.Logger
	customAttributes map[string]map[string][]*customAttributeConfigCompiled
	configRWMutex    sync.RWMutex
}

func buildEnrichmentProcessor(logger *zap.Logger, config *Config) (*spanEnrichmentProcessorImpl, error) {
	compiledAttributes, err := buildCompiledConfig(config)
	if err == nil {
		processor := spanEnrichmentProcessorImpl{
			logger:           logger,
			customAttributes: compiledAttributes,
		}
		return &processor, err
	} else {
		return nil, err
	}
}

func buildCompiledConfig(config *Config) (map[string]map[string][]*customAttributeConfigCompiled, error) {
	compiledAttributes := map[string]map[string][]*customAttributeConfigCompiled{}
	for targetAtt, attrConfigsByServiceKey := range config.CustomAttributeConfigs {
		compiledAttributes[targetAtt] = map[string][]*customAttributeConfigCompiled{}
		for serviceKey, attrConfigs := range attrConfigsByServiceKey {
			if compiledAttributes[targetAtt][serviceKey] == nil {
				compiledAttributes[targetAtt][serviceKey] = make([]*customAttributeConfigCompiled, 0)
			}
			for _, attrConfig := range attrConfigs {
				err := attrConfig.validate(targetAtt, serviceKey)
				if err == nil {
					compiledAttributes[targetAtt][serviceKey] = append(compiledAttributes[targetAtt][serviceKey], attrConfig.compile())
				} else {
					return nil, err
				}
			}
		}
	}
	return compiledAttributes, nil
}

// configListener interface implementation
func (ep *spanEnrichmentProcessorImpl) isUpdated(currConfig *Config, newConfig *Config) bool {
	updated := !reflect.DeepEqual(currConfig.CustomAttributeConfigs, newConfig.CustomAttributeConfigs)
	if updated {
		ep.logger.Info("Change detected in config CustomAttributeConfigs",
			zap.Any("Current", currConfig.CustomAttributeConfigs),
			zap.Any("New", newConfig.CustomAttributeConfigs),
		)
	} else {
		ep.logger.Debug("No change detected in config CustomAttributeConfigs")
	}
	return updated
}

func (ep *spanEnrichmentProcessorImpl) onUpdate(newConfig *Config) error {
	newAttributes, err := buildCompiledConfig(newConfig)
	if err == nil {
		ep.logger.Info("Updated config RequestContextExps",
			zap.Any("New", newConfig.CustomAttributeConfigs),
		)
		ep.configRWMutex.Lock()
		ep.customAttributes = newAttributes
		ep.configRWMutex.Unlock()
	} else {
		ep.logger.Error("Ignoring config RequestContextExps due to regex compilation error", zap.Error(err))
	}
	return err
}

func (ep *spanEnrichmentProcessorImpl) enrichSpan(namespace string, service string, span *ptrace.Span) {
	ep.addRequestType(span)
	ep.configRWMutex.RLock()
	currentConfig := ep.customAttributes
	ep.configRWMutex.RUnlock()
	for targetAtt, configByServiceKey := range currentConfig {
		if configByServiceKey[namespace+"#"+service] != nil {
			for _, config := range configByServiceKey[namespace+"#"+service] {
				config.addCustomAttribute(targetAtt, span)
			}
		} else {
			for _, config := range configByServiceKey["default"] {
				config.addCustomAttribute(targetAtt, span)
			}
		}
	}
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
