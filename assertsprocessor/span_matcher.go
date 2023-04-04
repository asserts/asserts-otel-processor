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
	spanAttrMatchers []*spanAttrMatcher
}

func (sm *spanMatcher) compileRequestContextRegexps(logger *zap.Logger, config *Config) error {
	logger.Info("Compiling request context regexps")
	sm.spanAttrMatchers = make([]*spanAttrMatcher, 0)
	if config.RequestContextExps != nil {
		for _, matcher := range *config.RequestContextExps {
			compile, err := regexp.Compile(matcher.Regex)
			logger.Debug("Compiled request context regex",
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
			sm.spanAttrMatchers = append(sm.spanAttrMatchers, &spanAttrMatcher{
				attrName:    matcher.AttrName,
				regex:       compile,
				replacement: replacement,
			})
		}
	}
	logger.Debug("Compiled request context regexps successfully")
	return nil
}

func (sm *spanMatcher) getRequest(span *ptrace.Span) string {
	for _, matcher := range sm.spanAttrMatchers {
		value, found := span.Attributes().Get(matcher.attrName)
		if found {
			subMatch := matcher.regex.FindStringSubmatch(value.AsString())
			if len(subMatch) >= 1 {
				return matcher.regex.ReplaceAllString(value.AsString(), matcher.replacement)
			}
		}
	}
	return span.Name()
}
