package assertsprocessor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateNoError(t *testing.T) {
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
	assert.Nil(t, err)
}

func TestValidateLimits(t *testing.T) {
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
		LimitPerService:           1,
		LimitPerRequestPerService: 2,
	}
	err := dto.Validate()
	assert.NotNil(t, err)
	assert.Equal(t, "LimitPerService: 1 < LimitPerRequestPerService: 2", err.Error())
}
