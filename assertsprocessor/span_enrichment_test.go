package assertsprocessor

import (
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"testing"
)

func TestBuildErrorProcessor(t *testing.T) {
	_logger, _ := zap.NewProduction()
	processor, err := buildEnrichmentProcessor(_logger, &Config{
		CustomAttributeConfigs: map[string]map[string][]*CustomAttributeConfig{
			"asserts.request.context": {
				"default": {&CustomAttributeConfig{
					SourceAttributes: []string{"http.url"},
					RegExp:           "https?://.+?((/[^/?]+){1,2}).*",
					Replacement:      "$1",
				}},
				"asserts#api-server": {&CustomAttributeConfig{
					SourceAttributes: []string{"http.url"},
					RegExp:           "https?://.+?((/[^/?]+){1,3}).*",
					Replacement:      "$1",
				}},
			},
			"asserts.error.type": {
				"default": {
					&CustomAttributeConfig{
						SourceAttributes: []string{"http.status_code"},
						RegExp:           "4..",
						Replacement:      "client_errors",
					},
					&CustomAttributeConfig{
						SourceAttributes: []string{"http.status_code"},
						RegExp:           "5..",
						Replacement:      "server_errors",
					}},
			},
		}})
	assert.Nil(t, err)
	assert.NotNil(t, processor)
}

func TestEnrichSpanRequestType(t *testing.T) {
	_logger, _ := zap.NewProduction()
	processor, err := buildEnrichmentProcessor(_logger, &Config{})
	assert.Nil(t, err)

	normalTrace := ptrace.NewTraces()
	resourceSpans := normalTrace.ResourceSpans().AppendEmpty()
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()
	span := scopeSpans.Spans().AppendEmpty()

	span.SetKind(ptrace.SpanKindClient)
	processor.enrichSpan("tsdb", "api-server", &span)

	typeAtt, _ := span.Attributes().Get(AssertsRequestTypeAttribute)
	assert.NotNil(t, typeAtt)
	assert.Equal(t, "outbound", typeAtt.Str())

	span.SetKind(ptrace.SpanKindServer)
	processor.enrichSpan("tsdb", "api-server", &span)
	typeAtt, _ = span.Attributes().Get(AssertsRequestTypeAttribute)
	assert.NotNil(t, typeAtt)
	assert.Equal(t, "inbound", typeAtt.Str())

	span.SetKind(ptrace.SpanKindInternal)
	processor.enrichSpan("asserts", "api-server", &span)
	typeAtt, _ = span.Attributes().Get(AssertsRequestTypeAttribute)
	assert.NotNil(t, typeAtt)
	assert.Equal(t, "internal", typeAtt.Str())
}

func TestEnrichSpanRequestContextErrorType(t *testing.T) {
	_logger, _ := zap.NewProduction()
	processor, err := buildEnrichmentProcessor(_logger, &Config{
		CustomAttributeConfigs: map[string]map[string][]*CustomAttributeConfig{
			"asserts.request.context": {
				"default": {&CustomAttributeConfig{
					SourceAttributes: []string{"http.url"},
					RegExp:           "https?://.+?((/[^/?]+){1,2}).*",
					Replacement:      "$1",
				}},
				"asserts#api-server": {&CustomAttributeConfig{
					SourceAttributes: []string{"http.url"},
					RegExp:           "https?://.+?((/[^/?]+){1,3}).*",
					Replacement:      "$1",
				}},
			},
			"asserts.error.type": {
				"default": {
					&CustomAttributeConfig{
						SourceAttributes: []string{"http.status_code"},
						RegExp:           "4..",
						Replacement:      "client_errors",
					},
					&CustomAttributeConfig{
						SourceAttributes: []string{"http.status_code"},
						RegExp:           "5..",
						Replacement:      "server_errors",
					}},
			},
		}})
	assert.Nil(t, err)

	normalTrace := ptrace.NewTraces()
	resourceSpans := normalTrace.ResourceSpans().AppendEmpty()
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()

	span := scopeSpans.Spans().AppendEmpty()
	span.Attributes().PutStr("http.url", "https://some.domain.com/foo/bar/baz?a=b")
	span.Attributes().PutInt("http.status_code", 404)
	span.SetKind(ptrace.SpanKindServer)

	processor.enrichSpan("asserts", "api-server", &span)
	contextAtt, _ := span.Attributes().Get(AssertsRequestContextAttribute)
	assert.NotNil(t, contextAtt)
	assert.Equal(t, "/foo/bar/baz", contextAtt.Str())

	clearAssertsAttributes(span)
	processor.enrichSpan("tsdb", "vminsert", &span)
	contextAtt, _ = span.Attributes().Get(AssertsRequestContextAttribute)
	assert.NotNil(t, contextAtt)
	assert.Equal(t, "/foo/bar", contextAtt.Str())

	att, _ := span.Attributes().Get(AssertsErrorTypeAttribute)
	assert.NotNil(t, att)
	assert.Equal(t, "client_errors", att.Str())

	clearAssertsAttributes(span)
	span.Attributes().PutStr("http.status_code", "504")
	processor.enrichSpan("asserts", "api-server", &span)
	att, _ = span.Attributes().Get(AssertsErrorTypeAttribute)
	assert.NotNil(t, att)
	assert.Equal(t, "server_errors", att.Str())

	clearAssertsAttributes(span)
	processor.enrichSpan("tsdb", "vminsert", &span)
	att, _ = span.Attributes().Get(AssertsErrorTypeAttribute)
	assert.NotNil(t, att)
	assert.Equal(t, "server_errors", att.Str())
}

func TestIsUpdated(t *testing.T) {
	config1 := &Config{
		CustomAttributeConfigs: map[string]map[string][]*CustomAttributeConfig{
			"asserts.request.context": {
				"default": {&CustomAttributeConfig{
					SourceAttributes: []string{"http.url"},
					RegExp:           "https?://.+?((/[^/?]+){1,2}).*",
					Replacement:      "$1",
				}},
				"asserts#api-server": {&CustomAttributeConfig{
					SourceAttributes: []string{"http.url"},
					RegExp:           "https?://.+?((/[^/?]+){1,3}).*",
					Replacement:      "$1",
				}},
			},
		}}
	config2 := &Config{
		CustomAttributeConfigs: map[string]map[string][]*CustomAttributeConfig{
			"asserts.request.context": {
				"default": {&CustomAttributeConfig{
					SourceAttributes: []string{"http.url"},
					RegExp:           "https?://.+?((/[^/?]+){1,2}).*",
					Replacement:      "$1",
				}},
				"asserts#api-server": {&CustomAttributeConfig{
					SourceAttributes: []string{"http.url"},
					RegExp:           "https?://.+?((/[^/?]+){1,3}).*",
					Replacement:      "$2",
				}},
			},
		}}
	_logger, _ := zap.NewProduction()
	processor, err := buildEnrichmentProcessor(_logger, config1)
	assert.Nil(t, err)
	assert.NotNil(t, processor)
	assert.False(t, processor.isUpdated(config1, config1))
	assert.True(t, processor.isUpdated(config1, config2))
}

func TestOnUpdate(t *testing.T) {
	config1 := &Config{
		CustomAttributeConfigs: map[string]map[string][]*CustomAttributeConfig{
			"asserts.request.context": {
				"default": {&CustomAttributeConfig{
					SourceAttributes: []string{"http.url"},
					RegExp:           "https?://.+?((/[^/?]+){1,2}).*",
					Replacement:      "$1",
				}},
				"asserts#api-server": {&CustomAttributeConfig{
					SourceAttributes: []string{"http.url"},
					RegExp:           "https?://.+?((/[^/?]+){1,3}).*",
					Replacement:      "$1",
				}},
			},
		}}
	config2 := &Config{
		CustomAttributeConfigs: map[string]map[string][]*CustomAttributeConfig{
			"asserts.request.context": {
				"default": {&CustomAttributeConfig{
					SourceAttributes: []string{"http.url"},
					RegExp:           "https?://.+?((/[^/?]+){1,2}).*",
					Replacement:      "$1",
				}},
				"asserts#api-server": {&CustomAttributeConfig{
					SourceAttributes: []string{"http.url"},
					RegExp:           "https?://.+?((/[^/?]+){1,3}).*",
					Replacement:      "$2",
				}},
			},
		}}
	_logger, _ := zap.NewProduction()
	processor, err := buildEnrichmentProcessor(_logger, config1)
	assert.Nil(t, err)
	assert.NotNil(t, processor)
	assert.Nil(t, processor.onUpdate(config2))
	assert.NotNil(t, processor.customAttributes)
	assert.NotNil(t, processor.customAttributes["asserts.request.context"])
	assert.NotNil(t, processor.customAttributes["asserts.request.context"]["default"])
	assert.Equal(t, 1, len(processor.customAttributes["asserts.request.context"]["default"]))
	assert.NotNil(t, processor.customAttributes["asserts.request.context"]["asserts#api-server"])
	assert.Equal(t, 1, len(processor.customAttributes["asserts.request.context"]["asserts#api-server"]))
	assert.Equal(t, "$2", processor.customAttributes["asserts.request.context"]["asserts#api-server"][0].replacement)
}

func clearAssertsAttributes(span ptrace.Span) {
	span.Attributes().Remove(AssertsRequestContextAttribute)
	span.Attributes().Remove(AssertsRequestTypeAttribute)
	span.Attributes().Remove(AssertsErrorTypeAttribute)
}
