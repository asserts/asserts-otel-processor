package assertsprocessor

import (
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"regexp"
	"testing"
)

func TestCheckMatch(t *testing.T) {
	logger, _ := zap.NewProduction()
	compiledExp, _ := regexp.Compile("(Sqs)|(DynamoDb)")
	assert.True(t, checkMatch(true, *compiledExp, "DynamoDb", &assertsProcessorImpl{
		logger: logger,
	}, "traceId", "spanId", "attName"))

	assert.True(t, checkMatch(true, *compiledExp, "Sqs", &assertsProcessorImpl{
		logger: logger,
	}, "traceId", "spanId", "attName"))

	assert.False(t, checkMatch(true, *compiledExp, "Ecs", &assertsProcessorImpl{
		logger: logger,
	}, "traceId", "spanId", "attName"))
}
