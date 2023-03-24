package assertsprocessor

import (
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"regexp"
)

type spanAttrMatcher struct {
	attrName string
	regex    *regexp.Regexp
}

type spanMatcher struct {
	spanAttrMatchers []*spanAttrMatcher
}

func (sm *spanMatcher) compileRequestContextRegexps(logger *zap.Logger, config *Config) error {
	logger.Info("compiling request context regexps")
	sm.spanAttrMatchers = make([]*spanAttrMatcher, 0)
	if config.RequestContextExps != nil {
		for _, matcher := range *config.RequestContextExps {
			compile, err := regexp.Compile(matcher.Regex)
			if err != nil {
				return err
			}
			sm.spanAttrMatchers = append(sm.spanAttrMatchers, &spanAttrMatcher{
				attrName: matcher.AttrName,
				regex:    compile,
			})
		}
	}
	logger.Debug("compiled request context regexps successfully")
	return nil
}

func (sm *spanMatcher) getRequest(span *ptrace.Span) string {
	for _, matcher := range sm.spanAttrMatchers {
		value, found := span.Attributes().Get(matcher.attrName)
		if found {
			subMatch := matcher.regex.FindStringSubmatch(value.AsString())
			if len(subMatch) >= 1 {
				return subMatch[1]
			}
		}
	}
	return span.Name()
}
