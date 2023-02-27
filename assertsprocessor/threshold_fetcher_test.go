package assertsprocessor

import (
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"regexp"
	"testing"
	"time"
)

var dto = EntityThresholdDto{
	ResourceURIPattern: "/api", LatencyUpperBound: 2,
}

type mockProcessor struct {
	assertsProcessorImpl
}

func (m *mockProcessor) getThresholds(entityKey EntityKeyDto) ([]EntityThresholdDto, error) {
	return []EntityThresholdDto{dto}, nil
}

func TestUpdateThresholdsAsync(t *testing.T) {
	systemPattern, _ := regexp.Compile("aws-api")
	servicePattern, _ := regexp.Compile("(Sqs)|(DynamoDb)")
	logger, _ := zap.NewProduction()

	m := mockProcessor{
		assertsProcessorImpl{
			logger: logger,
			attributeValueRegExps: &map[string]regexp.Regexp{
				"rpc.system":  *systemPattern,
				"rpc.service": *servicePattern,
			},
			config: Config{
				AssertsServer:           "http://localhost:8030",
				DefaultLatencyThreshold: 0.5,
			},
			latencyBounds: cmap.New[cmap.ConcurrentMap[string, LatencyBound]](),
			entityKeys:    cmap.New[EntityKeyDto](),
		},
	}

	dto := EntityKeyDto{
		EntityType: "Service", Name: "s", Scope: map[string]string{
			"asserts_env": "dev", "asserts_site": "us-west-2", "namespace": "n",
		},
	}
	m.updateThresholdsAsync(dto)
	time.Sleep(time.Second * 2)
	thresholds, err := m.getThresholds(dto)
	assert.Nil(t, err)
	assert.NotNil(t, thresholds)
	assert.Equal(t, 1, len(thresholds))
	assert.Equal(t, EntityThresholdDto{
		ResourceURIPattern: "/api", LatencyUpperBound: 2,
	}, thresholds[0])
}
