package assertsprocessor

import (
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"reflect"
	"strings"
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
	compiledAttributes, err := buildCompiledConfig(logger, config)
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

func buildCompiledConfig(logger *zap.Logger, config *Config) (map[string]map[string][]*customAttributeConfigCompiled, error) {
	if len(config.SpanAttributes) > 0 {
		return compileSpanAttributes(logger, config)
	} else {
		return compileCustomAttributes(logger, config)
	}
}

func compileCustomAttributes(logger *zap.Logger, config *Config) (map[string]map[string][]*customAttributeConfigCompiled, error) {
	compiled := map[string]map[string][]*customAttributeConfigCompiled{}
	for targetAtt, attrConfigsByServiceKey := range config.CustomAttributeConfigs {
		compiled[targetAtt] = map[string][]*customAttributeConfigCompiled{}
		for serviceKey, attrConfigs := range attrConfigsByServiceKey {
			compiled[targetAtt][serviceKey] = make([]*customAttributeConfigCompiled, 0)
			// Make one pass to ensure all configurations are valid
			for _, attrConfig := range attrConfigs {
				err := attrConfig.validate(targetAtt, serviceKey)
				if err != nil {
					return nil, err
				}
			}
			for _, attrConfig := range attrConfigs {
				logger.Debug("Added custom attribute for ",
					zap.String("TargetAttribute", targetAtt),
					zap.String("Service Key", serviceKey),
					zap.String("Source Attributes", "["+strings.Join(attrConfig.SourceAttributes, ", ")+"]"),
					zap.String("Regex", attrConfig.RegExp),
					zap.String("Replacement", attrConfig.Replacement))
				compiled[targetAtt][serviceKey] = append(compiled[targetAtt][serviceKey], attrConfig.compile())
			}
		}
	}
	return compiled, nil
}

func compileSpanAttributes(logger *zap.Logger, config *Config) (map[string]map[string][]*customAttributeConfigCompiled, error) {
	compiled := map[string]map[string][]*customAttributeConfigCompiled{}
	for _, spanAttribute := range config.SpanAttributes {
		attrName := spanAttribute.AttributeName
		compiled[attrName] = map[string][]*customAttributeConfigCompiled{}
		for _, attrConfig := range spanAttribute.AttributeConfigs {
			serviceKey := getKey(attrConfig)
			compiled[attrName][serviceKey] = make([]*customAttributeConfigCompiled, 0)
			// Make one pass to ensure all configurations are valid
			for _, rule := range attrConfig.Rules {
				err := rule.validate(attrName, serviceKey)
				if err != nil {
					return nil, err
				}
			}
			for _, rule := range attrConfig.Rules {
				logger.Debug("Added custom attribute for ",
					zap.String("TargetAttribute", attrName),
					zap.String("Service Key", serviceKey),
					zap.String("Source Attributes", "["+strings.Join(rule.SourceAttributes, ", ")+"]"),
					zap.String("Regex", rule.RegExp),
					zap.String("Replacement", rule.Replacement))
				compiled[attrName][serviceKey] = append(compiled[attrName][serviceKey], rule.compile())
			}
		}
	}
	return compiled, nil
}

func getKey(attrConfig *SpanAttributeConfig) string {
	var parts []string
	for _, part := range []string{attrConfig.Namespace, attrConfig.Service} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	serviceKey := strings.Join(parts, "#")
	if serviceKey == "" {
		serviceKey = "default"
	}
	return serviceKey
}

// configListener interface implementation
func (ep *spanEnrichmentProcessorImpl) isUpdated(currConfig *Config, newConfig *Config) bool {
	if len(newConfig.SpanAttributes) > 0 {
		return ep.isSpanAttributesUpdated(currConfig, newConfig)
	} else {
		return ep.isCustomAttributeConfigsUpdated(currConfig, newConfig)
	}
}

func (ep *spanEnrichmentProcessorImpl) isSpanAttributesUpdated(currConfig *Config, newConfig *Config) bool {
	updated := !reflect.DeepEqual(currConfig.SpanAttributes, newConfig.SpanAttributes)
	if updated {
		ep.logger.Info("Change detected in config SpanAttributes",
			zap.Any("Current", currConfig.SpanAttributes),
			zap.Any("New", newConfig.SpanAttributes),
		)
	} else {
		ep.logger.Debug("No change detected in config SpanAttributes")
	}
	return updated
}

func (ep *spanEnrichmentProcessorImpl) isCustomAttributeConfigsUpdated(currConfig *Config, newConfig *Config) bool {
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
	if len(newConfig.SpanAttributes) > 0 {
		return ep.onSpanAttributesUpdate(newConfig)
	} else {
		return ep.onCustomAttributeConfigsUpdate(newConfig)
	}
}

func (ep *spanEnrichmentProcessorImpl) onSpanAttributesUpdate(newConfig *Config) error {
	newAttributes, err := compileSpanAttributes(ep.logger, newConfig)
	if err == nil {
		ep.logger.Info("Updated config SpanAttributes",
			zap.Any("New", newConfig.SpanAttributes),
		)
		ep.configRWMutex.Lock()
		ep.customAttributes = newAttributes
		ep.configRWMutex.Unlock()
	} else {
		ep.logger.Error("Ignoring config RequestContextExps due to regex compilation error", zap.Error(err))
	}
	return err
}

func (ep *spanEnrichmentProcessorImpl) onCustomAttributeConfigsUpdate(newConfig *Config) error {
	newAttributes, err := compileCustomAttributes(ep.logger, newConfig)
	if err == nil {
		ep.logger.Info("Updated config CustomAttributeConfigs",
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
		customAttValue := ""
		serviceConfig := getAttrConfigs(configByServiceKey, namespace, service)
		if serviceConfig != nil {
			for _, config := range serviceConfig {
				customAttValue = config.getCustomAttribute(span)
				if customAttValue != "" {
					break
				}
			}
		}

		if customAttValue == "" && configByServiceKey["default"] != nil {
			for _, config := range configByServiceKey["default"] {
				customAttValue = config.getCustomAttribute(span)
				if customAttValue != "" {
					break
				}
			}
		}

		if customAttValue != "" {
			span.Attributes().PutStr(targetAtt, customAttValue)
		}
	}
	// If request context is not added set the span name as request context
	_, present := span.Attributes().Get(AssertsRequestContextAttribute)
	if !present {
		span.Attributes().PutStr(AssertsRequestContextAttribute, span.Name())
	}
}

func getAttrConfigs(cfg map[string][]*customAttributeConfigCompiled, ns string, svc string) []*customAttributeConfigCompiled {
	key := getServiceKey(ns, svc)
	attrConfigs := cfg[key]
	if attrConfigs == nil {
		attrConfigs = cfg[ns]
	}
	if attrConfigs == nil {
		attrConfigs = cfg[svc]
	}
	return attrConfigs
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
