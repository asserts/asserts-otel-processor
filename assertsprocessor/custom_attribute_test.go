package assertsprocessor

import (
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"regexp"
	"testing"
)

func TestValidateValidConfig(t *testing.T) {
	attrConfig := CustomAttributeConfig{
		SourceAttributes: []string{"attr1", "attr2"},
		RegExp:           "(.+);(.+)",
		Replacement:      "$1:$2",
	}
	assert.Nil(t, attrConfig.validate("target", "namespace#service"))
}

func TestValidateInvalidConfig_Regexp_Missing(t *testing.T) {
	attrConfig := CustomAttributeConfig{
		SourceAttributes: []string{"attr1", "attr2"},
		Replacement:      "$1:$2",
	}
	err := attrConfig.validate("target", "namespace#service")
	assert.NotNil(t, err)
}

func TestValidateInvalidConfig_Invalid_Regexp(t *testing.T) {
	attrConfig := CustomAttributeConfig{
		SourceAttributes: []string{"attr1", "attr2"},
		Replacement:      "$1:$2",
		RegExp:           "+",
	}
	err := attrConfig.validate("target", "namespace#service")
	assert.NotNil(t, err)
}

func TestValidateInvalidConfig_Replacement_Missing(t *testing.T) {
	attrConfig := CustomAttributeConfig{
		SourceAttributes: []string{"attr1", "attr2"},
		RegExp:           "(.+);(.+)",
	}
	err := attrConfig.validate("target", "namespace#service")
	assert.NotNil(t, err)
}

func TestValidateInvalidConfig_Source_Missing(t *testing.T) {
	attrConfig := CustomAttributeConfig{
		RegExp:      "(.+);(.+)",
		Replacement: "$1:$2",
	}
	err := attrConfig.validate("target", "namespace#service")
	assert.NotNil(t, err)
}

func TestValidateInvalidConfig_InvalidSourceAttribute(t *testing.T) {
	attrConfig := CustomAttributeConfig{
		SourceAttributes: []string{"attr1", ""},
		RegExp:           "(.+);(.+)",
		Replacement:      "$1:$2",
	}
	err := attrConfig.validate("target", "namespace#service")
	assert.NotNil(t, err)
}

func TestAddCustomAttribute_SpanDoesNotMatch(t *testing.T) {
	compiled, _ := regexp.Compile("(.+);(.+)")
	attrConfig := customAttributeConfigCompiled{
		sourceAttributes: []string{"attr1", "attr2"},
		regExp:           compiled,
		replacement:      "$1:$2",
		spanKinds:        []string{"Server"},
	}

	normalTrace := ptrace.NewTraces()
	resourceSpans := normalTrace.ResourceSpans().AppendEmpty()
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()
	span := scopeSpans.Spans().AppendEmpty()
	span.SetKind(ptrace.SpanKindClient)
	span.Attributes().PutStr("attr1", "foo")
	span.Attributes().PutStr("attr2", "bar")

	attrConfig.addCustomAttribute("target", &span)
	_, found := span.Attributes().Get("target")
	assert.False(t, found)
}

func TestAddCustomAttribute_SpanMatches_RegexpDoesNotMatch(t *testing.T) {
	compiled, _ := regexp.Compile("(foo);(.+)")
	attrConfig := customAttributeConfigCompiled{
		sourceAttributes: []string{"attr1", "attr2"},
		regExp:           compiled,
		replacement:      "$1:$2",
		spanKinds:        []string{"Server"},
	}

	normalTrace := ptrace.NewTraces()
	resourceSpans := normalTrace.ResourceSpans().AppendEmpty()
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()
	span := scopeSpans.Spans().AppendEmpty()
	span.SetKind(ptrace.SpanKindServer)
	span.Attributes().PutStr("attr1", "foo1")
	span.Attributes().PutStr("attr2", "bar")

	attrConfig.addCustomAttribute("target", &span)
	_, found := span.Attributes().Get("target")
	assert.False(t, found)
}

func TestAddCustomAttribute_SpanMatches_RegexpMatch(t *testing.T) {
	compiled, _ := regexp.Compile("(foo);(.+)")
	attrConfig := customAttributeConfigCompiled{
		sourceAttributes: []string{"attr1", "attr2"},
		regExp:           compiled,
		replacement:      "$1:$2",
		spanKinds:        []string{"Server"},
	}

	normalTrace := ptrace.NewTraces()
	resourceSpans := normalTrace.ResourceSpans().AppendEmpty()
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()
	span := scopeSpans.Spans().AppendEmpty()
	span.SetKind(ptrace.SpanKindServer)
	span.Attributes().PutStr("attr1", "foo")
	span.Attributes().PutStr("attr2", "bar")

	attrConfig.addCustomAttribute("target", &span)
	att, found := span.Attributes().Get("target")
	assert.True(t, found)
	assert.Equal(t, "foo:bar", att.AsString())
}
