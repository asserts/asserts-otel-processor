package assertsprocessor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateNoError(t *testing.T) {
	dto := Config{
		AttributeExps: &map[string]string{
			"attribute": ".+",
		},
		RequestContextExps: &[]*MatcherDto{{
			attrName: "attribute",
			regex:    ".+",
		}},
	}
	err := dto.Validate()
	assert.Nil(t, err)
}

func TestValidateAttributeExpError(t *testing.T) {
	dto := Config{
		AttributeExps: &map[string]string{
			"attribute": "+",
		},
		RequestContextExps: &[]*MatcherDto{{
			attrName: "attribute",
			regex:    ".+",
		}},
	}
	err := dto.Validate()
	assert.NotNil(t, err)
}

func TestValidateRequestExpError(t *testing.T) {
	dto := Config{
		AttributeExps: &map[string]string{
			"attribute": ".+",
		},
		RequestContextExps: &[]*MatcherDto{{
			attrName: "attribute",
			regex:    "+",
		}},
	}
	err := dto.Validate()
	assert.NotNil(t, err)
}

func TestValidateLimits(t *testing.T) {
	dto := Config{
		AttributeExps: &map[string]string{
			"attribute": ".+",
		},
		RequestContextExps: &[]*MatcherDto{{
			attrName: "attribute",
			regex:    ".+",
		}},
		LimitPerService:           1,
		LimitPerRequestPerService: 2,
	}
	err := dto.Validate()
	assert.NotNil(t, err)
	assert.Equal(t, "LimitPerService: 1 < LimitPerRequestPerService: 2", err.Error())
}
