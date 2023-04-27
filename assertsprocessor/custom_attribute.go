package assertsprocessor

import (
	"fmt"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"regexp"
	"strings"
)

type CustomAttributeConfig struct {
	SpanKinds        []string `mapstructure:"span_kinds" json:"span_kinds"`
	SourceAttributes []string `mapstructure:"source_attributes" json:"source_attributes"`
	RegExp           string   `mapstructure:"regexp" json:"regexp"`
	Replacement      string   `mapstructure:"replacement" json:"replacement"`
}

func (cAC *CustomAttributeConfig) validate(targetAtt string, serviceKey string) error {
	if cAC.RegExp == "" {
		return ValidationError{
			message: fmt.Sprintf("Invalid custom attribute config for target attribute: %s. and service key: %s,"+
				"regexp not specified for source_attributes: [%s], replacement: %s", targetAtt, serviceKey,
				strings.Join(cAC.SourceAttributes, "\", \""), cAC.Replacement),
		}
	} else if cAC.Replacement == "" {
		return ValidationError{
			message: fmt.Sprintf("Invalid custom attribute config for target attribute: %s. and service key: %s,"+
				"replacement not specified for source_attributes: [%s], regexp: %s", targetAtt, serviceKey,
				strings.Join(cAC.SourceAttributes, "\", \""), cAC.RegExp),
		}
	} else if len(cAC.SourceAttributes) == 0 {
		return ValidationError{
			message: fmt.Sprintf("Invalid custom attribute config for target attribute: %s. and service key: %s,"+
				"source_attributes not specified for regexp: %s, replacement: %s", targetAtt, serviceKey,
				cAC.RegExp, cAC.Replacement),
		}
	} else {
		for _, value := range cAC.SourceAttributes {
			if value == "" {
				return ValidationError{
					message: fmt.Sprintf("Invalid custom attribute config for target attribute: %s. and service key: %s,"+
						"empty attribute in source_attributes: [%s], regexp: %s, replacement: %s", targetAtt, serviceKey,
						strings.Join(cAC.SourceAttributes, "\", \""), cAC.RegExp, cAC.Replacement),
				}
			}
		}
	}
	_, err := regexp.Compile(cAC.RegExp)
	if err != nil {
		return ValidationError{
			message: fmt.Sprintf("Invalid custom attribute config for target attribute: %s. and service key: %s, "+
				"Invalid regexp in source_attributes: [%s] regexp: %s, replacement: %s: ", targetAtt, serviceKey,
				strings.Join(cAC.SourceAttributes, "\", \""), cAC.RegExp, cAC.Replacement),
			error: err,
		}
	}
	return nil
}

func (cAC *CustomAttributeConfig) compile() *customAttributeConfigCompiled {
	compiled, _ := regexp.Compile(cAC.RegExp)
	_spanKind := cAC.SpanKinds
	if _spanKind == nil || len(_spanKind) == 0 {
		_spanKind = append(_spanKind, "Server")
	}
	return &customAttributeConfigCompiled{
		spanKinds:        _spanKind,
		sourceAttributes: cAC.SourceAttributes,
		regExp:           compiled,
		replacement:      cAC.Replacement,
	}
}

type customAttributeConfigCompiled struct {
	spanKinds        []string
	sourceAttributes []string
	regExp           *regexp.Regexp
	replacement      string
}

func (cACC *customAttributeConfigCompiled) addCustomAttribute(targetAtt string, span *ptrace.Span) {
	for _, _spanKind := range cACC.spanKinds {
		if _spanKind == span.Kind().String() {
			values := make([]string, 0)
			for _, sourceAtt := range cACC.sourceAttributes {
				att, present := span.Attributes().Get(sourceAtt)
				if present {
					values = append(values, att.AsString())
				} else {
					return
				}
			}
			joined := strings.Join(values, ";")
			subMatch := cACC.regExp.FindStringSubmatch(joined)
			if len(subMatch) >= 1 {
				span.Attributes().PutStr(targetAtt, cACC.regExp.ReplaceAllString(joined, cACC.replacement))
			}
		}
	}
}
