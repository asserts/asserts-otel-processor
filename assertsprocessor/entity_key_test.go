package assertsprocessor

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestEntityKeyAsString(t *testing.T) {
	dto := EntityKeyDto{
		Type: "Service", Name: "api-server", Scope: map[string]string{
			"env": "dev", "site": "us-west-2", "namespace": "ride-service",
		},
	}
	assert.Equal(t, "{, env=dev, namespace=ride-service, site=us-west-2}/Service/api-server", dto.AsString())
}
