package assertsprocessor

import (
	"encoding/json"
	"github.com/mitchellh/mapstructure"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"gopkg.in/yaml.v3"
	"log"
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

func TestUnmarshalFromJSON(t *testing.T) {
	attrConfig := &CustomAttributeConfig{}
	err := json.Unmarshal([]byte(`{"value_expr": "server_errors",
            "regex": "5..",
            "source_attributes": ["http.status_code"],
            "span_kinds": ["Client", "Server"]}`), attrConfig)
	assert.Nil(t, err)
	assert.Equal(t, "server_errors", attrConfig.Replacement)
	assert.Equal(t, "5..", attrConfig.RegExp)
	assert.Equal(t, 1, len(attrConfig.SourceAttributes))
	assert.Equal(t, 2, len(attrConfig.SpanKinds))
	assert.Nil(t, attrConfig.validate("target", "service-key"))
}

func TestUnmarshalFromYAMLSimple(t *testing.T) {
	var attrConfig CustomAttributeConfig
	var raw interface{}

	var bytes = []byte(`source_attributes: ["http.url"]
span_kinds: ["Server"]
regex: "http://user:8080(/check)/anonymous-.*"
value_expr: "$1"`)
	// Unmarshal our input YAML file into empty interface
	if err := yaml.Unmarshal(bytes, &raw); err != nil {
		log.Fatal(err)
	}

	// Use mapstructure to convert our interface{} to CustomAttributeConfig (var attrConfig)
	decoder, _ := mapstructure.NewDecoder(&mapstructure.DecoderConfig{WeaklyTypedInput: true, Result: &attrConfig})
	if err := decoder.Decode(raw); err != nil {
		log.Fatal(err)
	}

	// Print out the new struct
	assert.NotNil(t, attrConfig)
	assert.Equal(t, "$1", attrConfig.Replacement)
	assert.Equal(t, "http://user:8080(/check)/anonymous-.*", attrConfig.RegExp)
	assert.Equal(t, 1, len(attrConfig.SpanKinds))
	assert.Equal(t, 1, len(attrConfig.SourceAttributes))
	assert.Nil(t, attrConfig.validate("target", "service-key"))
}

func TestUnmarshalFromYAMLComplex(t *testing.T) {
	var configs map[string]map[string][]*CustomAttributeConfig
	var raw interface{}
	var bytes = []byte(`"asserts.error.type":
  "default":
    - value_expr: client_errors
      regex: 4..
      source_attributes: ["http.status_code"]
      span_kinds: ["Client", "Server"]
    - value_expr: server_errors
      regex: 5..
      source_attributes: ["http.status_code"]
      span_kinds: ["Client", "Server"]
"asserts.request.context":
  robot-shop#payment:
    - source_attributes: ["http.url"]
      span_kinds: ["Server"]
      regex: "http://cart:8080(/cart)/anonymous-.*"
      value_expr: "$1"
    - source_attributes: ["http.url"]
      span_kinds: ["Server"]
      regex: "http://user:8080(/check)/anonymous-.*"
      value_expr: "$1"
  robot-shop#shipping:
    - source_attributes: ["http.url"]
      span_kinds: ["Server"]
      regex: "http://cart:8080(/shipping)/anonymous-.*"
      value_expr: "$1"
  default:
    - source_attributes: ["http.route"]
      regex: "(.+)"
      span_kinds: ["Server"]
      value_expr: "$1"
    - source_attributes: ["http.url"]
      span_kinds: ["Server"]
      regex: "https?://.+?((/[^/?]+){1,2}).*"
      value_expr: "$1"`)

	// Unmarshal our input YAML file into empty interface
	if err := yaml.Unmarshal(bytes, &raw); err != nil {
		log.Fatal(err)
	}

	// Use mapstructure to convert our interface{} to CustomAttributeConfig (var configs)
	decoder, _ := mapstructure.NewDecoder(&mapstructure.DecoderConfig{WeaklyTypedInput: true, Result: &configs})
	if err := decoder.Decode(raw); err != nil {
		log.Fatal(err)
	}

	// Print out the new struct
	assert.NotNil(t, configs)
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
