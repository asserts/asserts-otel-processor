package assertsprocessor

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestValidateNoError(t *testing.T) {
	dto := Config{
		AttributeExps: &map[string]string{
			"attribute": ".+",
		},
		RequestContextExps: &map[string]string{
			"attribute": ".+",
		},
	}
	err := dto.Validate()
	assert.Nil(t, err)
}

func TestValidateAttributeExpError(t *testing.T) {
	dto := Config{
		AttributeExps: &map[string]string{
			"attribute": "+",
		},
		RequestContextExps: &map[string]string{
			"attribute": ".+",
		},
	}
	err := dto.Validate()
	assert.NotNil(t, err)
}

func TestValidateRequestExpError(t *testing.T) {
	dto := Config{
		AttributeExps: &map[string]string{
			"attribute": ".+",
		},
		RequestContextExps: &map[string]string{
			"attribute": "+",
		},
	}
	err := dto.Validate()
	assert.NotNil(t, err)
}
