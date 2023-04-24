package assertsprocessor

import (
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"reflect"
	"regexp"
)

type spanAttrMatcher struct {
	attrName    string
	regex       *regexp.Regexp
	replacement string
}

type spanMatcher struct {
	logger           *zap.Logger
	spanAttrMatchers map[string][]*spanAttrMatcher
}

func (sm *spanMatcher) compileRequestContextRegexps(config *Config) error {
	sm.logger.Info("Compiling request context regexps")
	spanAttrMatchers := make(map[string][]*spanAttrMatcher)
	if config.RequestContextExps != nil {
		for serviceKey, serviceRequestContextExps := range config.RequestContextExps {
			serviceSpanAttrMatchers := make([]*spanAttrMatcher, 0)
			for _, matcher := range serviceRequestContextExps {
				compile, err := regexp.Compile(matcher.Regex)
				sm.logger.Debug("Compiled request context regex",
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
				serviceSpanAttrMatchers = append(serviceSpanAttrMatchers, &spanAttrMatcher{
					attrName:    matcher.AttrName,
					regex:       compile,
					replacement: replacement,
				})
			}
			spanAttrMatchers[serviceKey] = serviceSpanAttrMatchers
		}
		sm.spanAttrMatchers = spanAttrMatchers
	}
	sm.logger.Debug("Compiled request context regexps successfully")
	return nil
}

func (sm *spanMatcher) getRequest(span *ptrace.Span, serviceKey string) string {
	if sm.spanAttrMatchers[serviceKey] != nil {
		request := getRequest(span, sm.spanAttrMatchers[serviceKey])
		if request != "" {
			return request
		}
	}

	if sm.spanAttrMatchers["default"] != nil {
		request := getRequest(span, sm.spanAttrMatchers["default"])
		if request != "" {
			return request
		}
	}

	return span.Name()
}

// configListener interface implementation
func (sm *spanMatcher) isUpdated(currConfig *Config, newConfig *Config) bool {
	updated := !reflect.DeepEqual(currConfig.RequestContextExps, newConfig.RequestContextExps)
	if updated {
		sm.logger.Info("Change detected in config RequestContextExps",
			zap.Any("Current", currConfig.RequestContextExps),
			zap.Any("New", newConfig.RequestContextExps),
		)
	} else {
		sm.logger.Debug("No change detected in config RequestContextExps")
	}
	return updated
}

func (sm *spanMatcher) onUpdate(newConfig *Config) error {
	err := sm.compileRequestContextRegexps(newConfig)
	if err == nil {
		sm.logger.Info("Updated config RequestContextExps",
			zap.Any("New", newConfig.RequestContextExps),
		)
	} else {
		sm.logger.Error("Ignoring config RequestContextExps due to regex compilation error", zap.Error(err))
	}
	return err
}

func getRequest(span *ptrace.Span, serviceSpanAttrMatchers []*spanAttrMatcher) string {
	for _, matcher := range serviceSpanAttrMatchers {
		value, found := span.Attributes().Get(matcher.attrName)
		if found {
			subMatch := matcher.regex.FindStringSubmatch(value.AsString())
			if len(subMatch) >= 1 {
				return matcher.regex.ReplaceAllString(value.AsString(), matcher.replacement)
			}
		}
	}
	return ""
}
