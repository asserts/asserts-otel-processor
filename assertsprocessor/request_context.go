package assertsprocessor

import (
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"reflect"
	"regexp"
)

type requestConfigCompiled struct {
	attrName    string
	spanKind    string
	regex       *regexp.Regexp
	replacement string
}

type requestContextBuilder interface {
	getRequest(span *ptrace.Span, serviceKey string) string
}

type requestContextBuilderImpl struct {
	logger         *zap.Logger
	requestConfigs map[string][]*requestConfigCompiled
}

func (rCB *requestContextBuilderImpl) compileRequestContextRegexps(config *Config) error {
	rCB.logger.Info("Compiling request context regexps")
	requestConfigs := make(map[string][]*requestConfigCompiled)
	if config.RequestContextExps != nil {
		for serviceKey, serviceRequestContextExps := range config.RequestContextExps {
			configs := make([]*requestConfigCompiled, 0)
			for _, matcher := range serviceRequestContextExps {
				compile, err := regexp.Compile(matcher.Regex)
				rCB.logger.Debug("Compiled request context regex",
					zap.String("Service", serviceKey),
					zap.String("AttrName", matcher.AttrName),
					zap.String("Regex", matcher.Regex),
					zap.String("Replacement", matcher.Replacement),
				)
				if err != nil {
					return err
				}
				replacement := matcher.Replacement
				if replacement == "" {
					replacement = "$1"
				}
				configs = append(configs, &requestConfigCompiled{
					attrName:    matcher.AttrName,
					spanKind:    matcher.SpanKind,
					regex:       compile,
					replacement: replacement,
				})
			}
			requestConfigs[serviceKey] = configs
		}
		rCB.requestConfigs = requestConfigs
	}
	rCB.logger.Debug("Compiled request context regexps successfully")
	return nil
}

func (rCB *requestContextBuilderImpl) getRequest(span *ptrace.Span, serviceKey string) string {
	if rCB.requestConfigs[serviceKey] != nil {
		request := getRequest(span, rCB.requestConfigs[serviceKey])
		if request != "" {
			return request
		}
	}

	if rCB.requestConfigs["default"] != nil {
		request := getRequest(span, rCB.requestConfigs["default"])
		if request != "" {
			return request
		}
	}

	return span.Name()
}

// configListener interface implementation
func (rCB *requestContextBuilderImpl) isUpdated(currConfig *Config, newConfig *Config) bool {
	updated := !reflect.DeepEqual(currConfig.RequestContextExps, newConfig.RequestContextExps)
	if updated {
		rCB.logger.Info("Change detected in config RequestContextExps",
			zap.Any("Current", currConfig.RequestContextExps),
			zap.Any("New", newConfig.RequestContextExps),
		)
	} else {
		rCB.logger.Debug("No change detected in config RequestContextExps")
	}
	return updated
}

func (rCB *requestContextBuilderImpl) onUpdate(newConfig *Config) error {
	err := rCB.compileRequestContextRegexps(newConfig)
	if err == nil {
		rCB.logger.Info("Updated config RequestContextExps",
			zap.Any("New", newConfig.RequestContextExps),
		)
	} else {
		rCB.logger.Error("Ignoring config RequestContextExps due to regex compilation error", zap.Error(err))
	}
	return err
}

func getRequest(span *ptrace.Span, serviceSpanAttrMatchers []*requestConfigCompiled) string {
	for _, matcher := range serviceSpanAttrMatchers {
		if matcher.spanKind == span.Kind().String() {
			value, found := span.Attributes().Get(matcher.attrName)
			if found {
				subMatch := matcher.regex.FindStringSubmatch(value.AsString())
				if len(subMatch) >= 1 {
					return matcher.regex.ReplaceAllString(value.AsString(), matcher.replacement)
				}
			}
		}
	}
	return ""
}
