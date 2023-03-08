package assertsprocessor

import (
	"github.com/stretchr/testify/assert"
	"sync"
	"testing"
	"time"
)

func TestSample(t *testing.T) {
	state := periodicSamplingState{
		lastSampleTime: 0,
		rwMutex:        &sync.RWMutex{},
	}
	assert.True(t, state.sample(1))

	state.lastSampleTime = time.Now().Unix()
	assert.False(t, state.sample(1))

	state.lastSampleTime = time.Now().Unix() - 65
	assert.True(t, state.sample(1))
}
