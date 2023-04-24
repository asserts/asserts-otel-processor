package assertsprocessor

import (
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"regexp"
	"testing"
)

type spanTestRequestContextBuilder struct {
}

var requestContextValue = "/mock-request-context"

func (rcb *spanTestRequestContextBuilder) getRequest(span *ptrace.Span, serviceKey string) string {
	return requestContextValue
}

func TestCompileValidExpression(t *testing.T) {
	errorTypeConfig := ErrorTypeConfig{
		ErrorType: "client_errors",
		ValueExpr: "4..",
	}
	compiled, err := errorTypeConfig.compile()
	assert.Nil(t, err)
	assert.NotNil(t, compiled)
	assert.Equal(t, "client_errors", compiled.errorType)
	assert.NotNil(t, compiled.valueMatcher)
	assert.True(t, compiled.valueMatcher.MatchString("404"))
	assert.False(t, compiled.valueMatcher.MatchString("504"))
}

func TestCompileInValidExpression(t *testing.T) {
	errorTypeConfig := ErrorTypeConfig{
		ErrorType: "client_errors",
		ValueExpr: "+",
	}
	compiled, err := errorTypeConfig.compile()
	assert.NotNil(t, err)
	assert.Nil(t, compiled)
}

func TestBuildErrorProcessor(t *testing.T) {
	clientErrorConfig := ErrorTypeConfig{
		ErrorType: "client_errors",
		ValueExpr: "4..",
	}

	serverErrorConfig := ErrorTypeConfig{
		ErrorType: "server_errors",
		ValueExpr: "5..",
	}

	processor := buildEnrichmentProcessor(&Config{
		ErrorTypeConfigs: map[string][]*ErrorTypeConfig{
			"http.status_code": {
				&clientErrorConfig, &serverErrorConfig,
			},
		},
	})
	assert.NotNil(t, processor)
	assert.NotNil(t, processor.errorTypeConfigs)
	assert.NotNil(t, processor.errorTypeConfigs["http.status_code"])
	assert.Equal(t, 2, len(processor.errorTypeConfigs["http.status_code"]))
	assert.Equal(t, clientErrorConfig.ErrorType, processor.errorTypeConfigs["http.status_code"][0].errorType)
	assert.True(t, processor.errorTypeConfigs["http.status_code"][0].valueMatcher.MatchString("404"))
	assert.Equal(t, serverErrorConfig.ErrorType, processor.errorTypeConfigs["http.status_code"][1].errorType)
	assert.True(t, processor.errorTypeConfigs["http.status_code"][1].valueMatcher.MatchString("504"))
}

func TestEnrichSpan(t *testing.T) {
	clientMatcher, _ := regexp.Compile("4..")
	serverMatcher, _ := regexp.Compile("5..")
	processor := spanEnrichmentProcessorImpl{
		errorTypeConfigs: map[string][]*errorTypeCompiledConfig{
			"http.status_code": {
				&errorTypeCompiledConfig{
					errorType: "client_errors", valueMatcher: clientMatcher,
				},
				&errorTypeCompiledConfig{
					errorType: "server_errors", valueMatcher: serverMatcher,
				},
			},
		},
		requestBuilder: &spanTestRequestContextBuilder{},
	}
	normalTrace := ptrace.NewTraces()
	resourceSpans := normalTrace.ResourceSpans().AppendEmpty()
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()
	span := scopeSpans.Spans().AppendEmpty()

	span.Attributes().PutStr("http.status_code", "404")
	span.SetKind(ptrace.SpanKindClient)
	processor.enrichSpan("asserts", "api-server", &span)

	att, _ := span.Attributes().Get(AssertsErrorTypeAttribute)
	assert.NotNil(t, att)
	assert.Equal(t, "client_errors", att.Str())

	typeAtt, _ := span.Attributes().Get(AssertsRequestTypeAttribute)
	assert.NotNil(t, typeAtt)
	assert.Equal(t, "outbound", typeAtt.Str())

	span.Attributes().PutStr("http.status_code", "504")
	span.SetKind(ptrace.SpanKindServer)
	processor.enrichSpan("asserts", "api-server", &span)

	att, _ = span.Attributes().Get(AssertsErrorTypeAttribute)
	assert.NotNil(t, att)
	assert.Equal(t, "server_errors", att.Str())

	typeAtt, _ = span.Attributes().Get(AssertsRequestTypeAttribute)
	assert.NotNil(t, typeAtt)
	assert.Equal(t, "inbound", typeAtt.Str())

	contextAtt, _ := span.Attributes().Get(AssertsRequestContextAttribute)
	assert.NotNil(t, contextAtt)
	assert.Equal(t, "/mock-request-context", contextAtt.Str())
}
