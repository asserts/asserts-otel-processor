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
	assertsClient       *assertsClient
}

func (th *thresholdHelper) getThreshold(ns string, service string, request string) float64 {
	var entityKey = buildEntityKey(th.config, ns, service)
	th.entityKeys.LoadOrStore(entityKey.AsString(), entityKey)
	var thresholds, _ = th.thresholds.Load(entityKey.AsString())

	thresholdFound := th.config.DefaultLatencyThreshold
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
	body, err := th.assertsClient.invoke(http.MethodPost, latencyThresholdsApi, entityKey)
	if err == nil {
		err = json.Unmarshal(body, &thresholds)
	}

	th.logThresholds(entityKey, thresholds)
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
