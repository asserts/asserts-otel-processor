package assertsprocessor

import (
	"math"
	"sync"
)

type serviceQueues struct {
	config        *Config
	requestStates *sync.Map
	requestCount  int
	rwMutex       *sync.RWMutex
}

func NewServiceQueues(config *Config) *serviceQueues {
	return &serviceQueues{
		config:        config,
		requestStates: &sync.Map{},
		rwMutex:       &sync.RWMutex{},
	}
}

func (sq *serviceQueues) getRequestState(request string) *traceSampler {
	entry, found := sq.requestStates.Load(request)
	if found {
		return entry.(*traceSampler)
	}

	// We create an entry for a request only when there is room
	var result *traceSampler

	// Check if there is room
	if sq.hasRoom() {
		sq.rwMutex.Lock()
		defer sq.rwMutex.Unlock()
		entry, found = sq.requestStates.Load(request)
		if found {
			return entry.(*traceSampler)
		}
		currentSize := sq.requestCount
		if currentSize < sq.config.LimitPerService {
			perRequestLimit := int(math.Min(5, float64(sq.config.LimitPerRequestPerService)))
			result = &traceSampler{
				slowQueue:  NewTraceQueue(perRequestLimit),
				errorQueue: NewTraceQueue(perRequestLimit),
				samplingState: &periodicSamplingState{
					lastSampleTime: 0,
					rwMutex:        &sync.RWMutex{},
				},
			}
			sq.requestStates.Store(request, result)
			sq.requestCount = sq.requestCount + 1
		}
	}
	return result
}

func (sq *serviceQueues) hasRoom() bool {
	sq.rwMutex.RLock()
	currentSize := sq.requestCount
	sq.rwMutex.RUnlock()
	return currentSize < sq.config.LimitPerService
}
