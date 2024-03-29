package assertsprocessor

import (
	"github.com/jellydator/ttlcache/v3"
	"math"
	"sync"
	"time"
)

type serviceQueues struct {
	config                 *Config
	requestStates          *sync.Map
	periodicSamplingStates *ttlcache.Cache[string, *periodicSamplingState] // limit cardinality of request contexts for which traces are captured
	requestCount           int
	rwMutex                *sync.RWMutex
}

func newServiceQueues(config *Config) *serviceQueues {
	return &serviceQueues{
		config:        config,
		requestStates: &sync.Map{},
		periodicSamplingStates: ttlcache.New[string, *periodicSamplingState](
			ttlcache.WithTTL[string, *periodicSamplingState](time.Minute*time.Duration(config.RequestContextCacheTTL)),
			ttlcache.WithCapacity[string, *periodicSamplingState](uint64(config.LimitPerService)),
		),
		rwMutex: &sync.RWMutex{},
	}
}

func (sq *serviceQueues) clearRequestStates() *sync.Map {
	sq.rwMutex.Lock()
	defer sq.rwMutex.Unlock()
	previousRequestStates := sq.requestStates
	sq.requestStates = &sync.Map{}
	sq.requestCount = 0
	return previousRequestStates
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
