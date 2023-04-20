package assertsprocessor

import (
	"encoding/json"
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

type thresholdHelper struct {
	config              *Config
	logger              *zap.Logger
	thresholds          *sync.Map
	thresholdSyncTicker *clock.Ticker
	entityKeys          *sync.Map
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
		thresholdMap := thresholds.(map[string]*ThresholdDto)

		if thresholdMap[request] != nil {
			thresholdFound = (*thresholdMap[request]).LatencyUpperBound
		} else if thresholdMap[""] != nil {
			thresholdFound = (*thresholdMap[""]).LatencyUpperBound
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
					keys := make([]string, 0)
					th.entityKeys.Range(func(key any, value any) bool {
						entityKey := value.(EntityKeyDto)
						keys = append(keys, entityKey.AsString())
						th.updateThresholdsAsync(entityKey)
						return true
					})
					if len(keys) > 0 {
						th.logger.Info("Fetching thresholds for",
							zap.Strings("Services", keys),
						)
					} else {
						th.logger.Info("Skip fetching thresholds as no service has reported a Trace")
					}
				}
			}
		}()
	}
}

func (th *thresholdHelper) updateThresholdsAsync(entityKey EntityKeyDto) bool {
	th.logger.Debug("updateThresholdsAsync(...) called for",
		zap.String("Entity Key", entityKey.AsString()))
	go func() {
		thresholds, err := th.getThresholds(entityKey)
		if err == nil {
			var latestThresholds = map[string]*ThresholdDto{}
			for i, threshold := range thresholds {
				latestThresholds[threshold.RequestContext] = &thresholds[i]
			}
			th.thresholds.Store(entityKey.AsString(), latestThresholds)
		}
	}()
	return true
}

func (th *thresholdHelper) getThresholds(entityKey EntityKeyDto) ([]ThresholdDto, error) {
	var thresholds []ThresholdDto
	body, err := th.rc.invoke(http.MethodPost, latencyThresholdsApi, entityKey)
	if err == nil {
		err = json.Unmarshal(body, &thresholds)
		if err == nil {
			th.logThresholds(entityKey, thresholds)
		} else {
			th.logger.Error("Error unmarshalling thresholds", zap.Error(err))
		}
	}

	return thresholds, err
}

func (th *thresholdHelper) logThresholds(entityKey EntityKeyDto, thresholds []ThresholdDto) {
	var fields = make([]zap.Field, 0)
	fields = append(fields, zap.String("Entity", entityKey.AsString()))
	for i := range thresholds {
		fields = append(fields, zap.Float64(thresholds[i].RequestContext, thresholds[i].LatencyUpperBound))
	}
	th.logger.Debug("Got thresholds ", fields...)
}

// configListener interface implementation
func (th *thresholdHelper) isUpdated(prevConfig *Config, currentConfig *Config) bool {
	th.rwMutex.RLock()
	defer th.rwMutex.RUnlock()

	updated := prevConfig.DefaultLatencyThreshold != currentConfig.DefaultLatencyThreshold
	if updated {
		th.logger.Info("Change detected in config DefaultLatencyThreshold",
			zap.Any("Previous", prevConfig.DefaultLatencyThreshold),
			zap.Any("Current", currentConfig.DefaultLatencyThreshold),
		)
	} else {
		th.logger.Debug("No change detected in config DefaultLatencyThreshold")
	}
	return updated
}

func (th *thresholdHelper) onUpdate(currentConfig *Config) error {
	th.rwMutex.Lock()
	defer th.rwMutex.Unlock()

	th.config.DefaultLatencyThreshold = currentConfig.DefaultLatencyThreshold
	th.logger.Info("Updated config DefaultLatencyThreshold",
		zap.Float64("Current", th.config.DefaultLatencyThreshold),
	)
	return nil
}
