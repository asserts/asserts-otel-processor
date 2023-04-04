package assertsprocessor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateNoError(t *testing.T) {
	dto := Config{
		RequestContextExps: &[]*MatcherDto{{
			AttrName: "attribute",
			Regex:    ".+",
		}},
	}
	err := dto.Validate()
	assert.Nil(t, err)
}

func TestValidateRequestExpError(t *testing.T) {
	dto := Config{
		RequestContextExps: &[]*MatcherDto{{
			AttrName: "attribute",
			Regex:    "+",
		}},
	}
	err := dto.Validate()
	assert.NotNil(t, err)
}

func TestValidateLimits(t *testing.T) {
	dto := Config{
		RequestContextExps: &[]*MatcherDto{{
			AttrName: "attribute",
			Regex:    ".+",
		}},
		LimitPerService:           1,
		LimitPerRequestPerService: 2,
	}
	err := dto.Validate()
	assert.NotNil(t, err)
	assert.Equal(t, "LimitPerService: 1 < LimitPerRequestPerService: 2", err.Error())
}
