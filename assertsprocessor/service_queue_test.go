package assertsprocessor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewServiceQueues(t *testing.T) {
	var testConfig = &Config{}
	var sq = newServiceQueues(testConfig)

	assert.NotNil(t, sq.requestStates)
	assert.NotNil(t, sq.rwMutex)
	assert.Equal(t, 0, sq.requestCount)
	assert.Equal(t, testConfig, sq.config)
}

func TestNewServiceQueues_Limit_Zero(t *testing.T) {
	var testConfig = &Config{
		LimitPerService:           0,
		LimitPerRequestPerService: 5,
	}
	var sq = newServiceQueues(testConfig)

	assert.Nil(t, sq.getRequestState("/request1"))
	assert.Equal(t, 0, sq.requestCount)
}

func TestNewServiceQueues_Limit_One(t *testing.T) {
	var testConfig = &Config{
		LimitPerService:           1,
		LimitPerRequestPerService: 5,
	}
	var sq = newServiceQueues(testConfig)

	assert.Equal(t, 0, sq.requestCount)
	queue := sq.getRequestState("/request1")
	assert.NotNil(t, queue)
	assert.Equal(t, 1, sq.requestCount)

	assert.NotNil(t, queue.errorQueue)
	assert.NotNil(t, queue.errorQueue.priorityQueue)
	assert.NotNil(t, queue.errorQueue.mutex)
	assert.Equal(t, 5, queue.errorQueue.maxSize)

	assert.NotNil(t, queue.slowQueue)
	assert.NotNil(t, queue.slowQueue.priorityQueue)
	assert.NotNil(t, queue.slowQueue.mutex)
	assert.Equal(t, 5, queue.slowQueue.maxSize)

	queue = sq.getRequestState("/request2")
	assert.Equal(t, 1, sq.requestCount)
	assert.Nil(t, queue)
}

func TestNewServiceQueues_Limit_Two(t *testing.T) {
	var testConfig = &Config{
		LimitPerService:           2,
		LimitPerRequestPerService: 5,
	}
	var sq = newServiceQueues(testConfig)

	assert.Equal(t, 0, sq.requestCount)
	queue := sq.getRequestState("/request1")
	assert.NotNil(t, queue)
	assert.Equal(t, 1, sq.requestCount)

	assert.NotNil(t, queue.errorQueue)
	assert.NotNil(t, queue.errorQueue.priorityQueue)
	assert.NotNil(t, queue.errorQueue.mutex)
	assert.Equal(t, 5, queue.errorQueue.maxSize)

	assert.NotNil(t, queue.slowQueue)
	assert.NotNil(t, queue.slowQueue.priorityQueue)
	assert.NotNil(t, queue.slowQueue.mutex)
	assert.Equal(t, 5, queue.slowQueue.maxSize)

	queue = sq.getRequestState("/request2")
	assert.NotNil(t, queue)
	assert.Equal(t, 2, sq.requestCount)
}
