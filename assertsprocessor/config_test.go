package assertsprocessor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateCustomAttributeConfigsNoError(t *testing.T) {
	dto := Config{
		Env: "dev",
		CustomAttributeConfigs: map[string]map[string][]*CustomAttributeConfig{
			"asserts.request.context": {
				"default": {
					{
						SourceAttributes: []string{"attribute"},
						SpanKinds:        []string{"Client"},
						RegExp:           "(.+)",
						Replacement:      "$1",
					},
				},
			},
		},
	}
	err := dto.Validate()
	assert.Nil(t, err)
}

func TestValidateCustomAttributeConfigsError(t *testing.T) {
	dto := Config{
		Env: "dev",
		CustomAttributeConfigs: map[string]map[string][]*CustomAttributeConfig{
			"asserts.request.context": {
				"default": {
					{
						SourceAttributes: []string{"attribute"},
						SpanKinds:        []string{"Client"},
						RegExp:           "+",
						Replacement:      "$1",
					},
				},
			},
		},
	}
	err := dto.Validate()
	assert.NotNil(t, err)
}

func TestValidateSpanAttributesNoError(t *testing.T) {
	dto := Config{
		Env: "dev",
		SpanAttributes: []*SpanAttribute{
			{
				AttributeName: "asserts.request.context",
				AttributeConfigs: []*SpanAttributeConfig{
					{
						Rules: []*CustomAttributeConfig{
							{
								SourceAttributes: []string{"attribute"},
								SpanKinds:        []string{"Client"},
								RegExp:           "(.+)",
								Replacement:      "$1",
							},
						},
					},
				},
			},
		},
	}
	err := dto.Validate()
	assert.Nil(t, err)
}

func TestValidateSpanAttributesError(t *testing.T) {
	dto := Config{
		Env: "dev",
		SpanAttributes: []*SpanAttribute{
			{
				AttributeName: "asserts.request.context",
				AttributeConfigs: []*SpanAttributeConfig{
					{
						Rules: []*CustomAttributeConfig{
							{
								SourceAttributes: []string{"attribute"},
								SpanKinds:        []string{"Client"},
								RegExp:           "+",
								Replacement:      "$1",
							},
						},
					},
				},
			},
		},
	}
	err := dto.Validate()
	assert.NotNil(t, err)
}

func TestEnvMissing(t *testing.T) {
	dto := Config{
		CustomAttributeConfigs: map[string]map[string][]*CustomAttributeConfig{
			"asserts.request.context": {
				"default": {
					{
						SourceAttributes: []string{"attribute"},
						SpanKinds:        []string{"Client"},
						RegExp:           "(.+)",
						Replacement:      "$1",
					},
				},
			},
		},
	}
	err := dto.Validate()
	assert.NotNil(t, err)
	assert.Equal(t, "Env property is not set", err.Error())
}

func TestValidateLimits(t *testing.T) {
	dto := Config{
		Env: "dev",
		CustomAttributeConfigs: map[string]map[string][]*CustomAttributeConfig{
			"asserts.request.context": {
				"default": {
					{
						SourceAttributes: []string{"attribute"},
						SpanKinds:        []string{"Client"},
						RegExp:           "(.+)",
						Replacement:      "$1",
					},
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
