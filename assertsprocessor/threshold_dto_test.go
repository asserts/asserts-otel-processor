package assertsprocessor

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestAsString(t *testing.T) {

	dto := EntityKeyDto{
		EntityType: "Service", Name: "api-server", Scope: map[string]string{
			"asserts_env": "dev", "asserts_site": "us-west-2", "namespace": "ride-service",
		},
	}
	assert.Equal(t, "{, asserts_env=dev, asserts_site=us-west-2, namespace=ride-service}/Service/api-server", dto.AsString())
}
