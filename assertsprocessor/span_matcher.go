package assertsprocessor

import (
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
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
	sm.spanAttrMatchers = make(map[string][]*spanAttrMatcher)
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
			sm.spanAttrMatchers[serviceKey] = serviceSpanAttrMatchers
		}
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
