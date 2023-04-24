package assertsprocessor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateNoError(t *testing.T) {
	dto := Config{
		RequestContextExps: map[string][]*MatcherDto{
			"default": {
				{
					AttrName: "attribute",
					SpanKind: "Client",
					Regex:    ".+",
				},
			},
		},
		ErrorTypeConfigs: map[string][]*ErrorTypeConfig{
			"http.status_code": {
				&ErrorTypeConfig{
					ValueExpr: "4..",
					ErrorType: "client_errors",
				},
			},
		},
	}
	err := dto.Validate()
	assert.Nil(t, err)
}

func TestValidateRequestExpError(t *testing.T) {
	dto := Config{
		RequestContextExps: map[string][]*MatcherDto{
			"default": {
				{
					AttrName: "attribute",
					SpanKind: "Client",
					Regex:    "+",
				},
			},
		},
		ErrorTypeConfigs: map[string][]*ErrorTypeConfig{
			"http.status_code": {
				&ErrorTypeConfig{
					ValueExpr: "4..",
					ErrorType: "client_errors",
				},
			},
		},
	}
	err := dto.Validate()
	assert.NotNil(t, err)
}

func TestValidateErrorTypeConfigError(t *testing.T) {
	dto := Config{
		RequestContextExps: map[string][]*MatcherDto{
			"default": {
				{
					AttrName: "attribute",
					SpanKind: "Client",
					Regex:    ".+",
				},
			},
		},
		ErrorTypeConfigs: map[string][]*ErrorTypeConfig{
			"http.status_code": {
				&ErrorTypeConfig{
					ValueExpr: "+",
					ErrorType: "client_errors",
				},
			},
		},
	}
	err := dto.Validate()
	assert.NotNil(t, err)
}

func TestValidateLimits(t *testing.T) {
	dto := Config{
		RequestContextExps: map[string][]*MatcherDto{
			"default": {
				{
					AttrName: "attribute",
					SpanKind: "Client",
					Regex:    ".+",
				},
			},
		},
		LimitPerService:           1,
		LimitPerRequestPerService: 2,
	}
	err := dto.Validate()
	assert.NotNil(t, err)
	assert.Equal(t, "LimitPerService: 1 < LimitPerRequestPerService: 2", err.Error())
}
