package assertsprocessor

import (
	"encoding/json"
	"github.com/puzpuzpuz/xsync/v2"
	"github.com/tilinna/clock"
	"go.uber.org/zap"
	"net/http"
	"sync"
)

type ThresholdDto struct {
	RequestType       string  `json:"requestType"`
	RequestContext    string  `json:"requestContext"`
	LatencyUpperBound float64 `json:"upperThreshold"`
}

type ThresholdsDto struct {
	EntityKey         EntityKeyDto   `json:"entityKey"`
	LatencyThresholds []ThresholdDto `json:"latencyThresholds"`
}

type thresholdHelper struct {
	config              *Config
	logger              *zap.Logger
	thresholds          *xsync.MapOf[string, map[string]*ThresholdDto]
	thresholdSyncTicker *clock.Ticker
	entityKeys          *xsync.MapOf[string, EntityKeyDto]
	stop                chan bool
	rc                  restClient
	rwMutex             *sync.RWMutex // guard access to config.DefaultLatencyThreshold
}

func (th *thresholdHelper) getThreshold(ns string, service string, request string) float64 {
	var entityKey = buildEntityKey(th.config, ns, service)
	th.entityKeys.LoadOrStore(entityKey.AsString(), entityKey)
	var thresholds, _ = th.thresholds.Load(entityKey.AsString())

	thresholdFound := th.getDefaultThreshold()
	if thresholds != nil {
		if thresholds[request] != nil {
			thresholdFound = (*thresholds[request]).LatencyUpperBound
		} else if thresholds[""] != nil {
			thresholdFound = (*thresholds[""]).LatencyUpperBound
		}
	}
	return thresholdFound
}

func (th *thresholdHelper) getDefaultThreshold() float64 {
	th.rwMutex.RLock()
	defer th.rwMutex.RUnlock()

	return th.config.DefaultLatencyThreshold
}

func (th *thresholdHelper) stopUpdates() {
	go func() { th.stop <- true }()
}

func (th *thresholdHelper) startUpdates() {
	endPoint := (*(*th.config).AssertsServer)["endpoint"]
	if endPoint != "" {
		go func() {
			for {
				select {
				case <-th.stop:
					th.logger.Info("Stopping threshold updates")
					return
				case <-th.thresholdSyncTicker.C:
					entityKeys := make([]EntityKeyDto, 0)
					th.entityKeys.Range(func(key string, entityKey EntityKeyDto) bool {
						entityKeys = append(entityKeys, entityKey)
						return true
					})
					if len(entityKeys) > 0 {
						th.logger.Info("Fetching thresholds for",
							zap.Any("Services", entityKeys),
						)
						th.updateThresholdsAsync(entityKeys)
					} else {
						th.logger.Info("Skip fetching thresholds as no service has reported a Trace")
					}
				}
			}
		}()
	}
}

func (th *thresholdHelper) updateThresholdsAsync(entityKeys []EntityKeyDto) bool {
	go func() {
		thresholdsDtos, err := th.getThresholds(entityKeys)
		if err == nil {
			for _, thresholdsDto := range thresholdsDtos {
				var entityKey = thresholdsDto.EntityKey.AsString()
				var thresholds = map[string]*ThresholdDto{}
				for _, threshold := range thresholdsDto.LatencyThresholds {
					thresholds[threshold.RequestContext] = &threshold
				}
				th.thresholds.Store(entityKey, thresholds)
			}
		}
	}()
	return true
}

func (th *thresholdHelper) getThresholds(entityKeys []EntityKeyDto) ([]ThresholdsDto, error) {
	var thresholds []ThresholdsDto
	body, err := th.rc.invoke(http.MethodPost, latencyThresholdsApi, entityKeys)
	if err == nil {
		err = json.Unmarshal(body, &thresholds)
		if err == nil {
			th.logger.Debug("",
				zap.Any("Got thresholds", thresholds),
			)
		} else {
			th.logger.Error("Error unmarshalling thresholds", zap.Error(err))
		}
	}

	return thresholds, err
}

// configListener interface implementation
func (th *thresholdHelper) isUpdated(currConfig *Config, newConfig *Config) bool {
	th.rwMutex.RLock()
	defer th.rwMutex.RUnlock()

	updated := currConfig.DefaultLatencyThreshold != newConfig.DefaultLatencyThreshold
	if updated {
		th.logger.Info("Change detected in config DefaultLatencyThreshold",
			zap.Any("Current", currConfig.DefaultLatencyThreshold),
			zap.Any("New", newConfig.DefaultLatencyThreshold),
		)
	} else {
		th.logger.Debug("No change detected in config DefaultLatencyThreshold")
	}
	return updated
}

func (th *thresholdHelper) onUpdate(newConfig *Config) error {
	th.rwMutex.Lock()
	defer th.rwMutex.Unlock()

	th.config.DefaultLatencyThreshold = newConfig.DefaultLatencyThreshold
	th.logger.Info("Updated config DefaultLatencyThreshold",
		zap.Float64("New", th.config.DefaultLatencyThreshold),
	)
	return nil
}
