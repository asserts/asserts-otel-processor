package assertsprocessor

import (
	"sync"
	"time"
)

type periodicSamplingState struct {
	lastSampleTime int64
	rwMutex        *sync.RWMutex
}

func (state *periodicSamplingState) sample(everyNMinutes int) bool {
	state.rwMutex.RLock()
	currentTime := time.Now().Unix()
	elapsedTime := currentTime - state.lastSampleTime
	state.rwMutex.RUnlock()
	interval := int64(time.Duration(everyNMinutes) * 60)
	sampleNow := elapsedTime > interval
	if sampleNow {
		state.rwMutex.Lock()
		elapsedTime = currentTime - state.lastSampleTime
		if elapsedTime > interval {
			state.lastSampleTime = currentTime
			sampleNow = true
		}
		state.rwMutex.Unlock()
		return sampleNow
	}
	return false
}
