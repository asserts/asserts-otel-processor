package assertsprocessor

import (
	"bytes"
	"encoding/json"
	"github.com/tilinna/clock"
	"go.uber.org/zap"
	"io"
	"net/http"
	"sync"
	"time"
)

type ThresholdDto struct {
	ResourceURIPattern string  `json:"request_context"`
	LatencyUpperBound  float64 `json:"upper_threshold"`
}

type thresholdHelper struct {
	config              *Config
	logger              *zap.Logger
	thresholds          *sync.Map
	thresholdSyncTicker *clock.Ticker
	entityKeys          *sync.Map
	stop                chan bool
}

func (th *thresholdHelper) getThreshold(ns string, service string, request string) float64 {
	var entityKey = buildEntityKey(th.config, ns, service)
	th.entityKeys.LoadOrStore(entityKey.AsString(), entityKey)
	var thresholds, _ = th.thresholds.Load(entityKey.AsString())
	if thresholds != nil {
		thresholdMap := thresholds.(map[string]*ThresholdDto)
		if thresholdMap[request] != nil {
			return (*thresholdMap[request]).LatencyUpperBound
		} else if thresholdMap[""] != nil {
			return (*thresholdMap[""]).LatencyUpperBound
		}
	}
	return th.config.DefaultLatencyThreshold
}

func (th *thresholdHelper) stopUpdates() {
	go func() { th.stop <- true }()
}

func (th *thresholdHelper) updateThresholds() {
	for {
		select {
		case <-th.stop:
			th.logger.Info("Stopping threshold updates")
			return
		case <-th.thresholdSyncTicker.C:
			th.logger.Info("Fetching thresholds")
			th.entityKeys.Range(func(key any, value any) bool {
				entityKey := value.(EntityKeyDto)
				th.updateThresholdsAsync(entityKey)
				return true
			})
		}
	}
}

func (th *thresholdHelper) updateThresholdsAsync(entityKey EntityKeyDto) bool {
	th.logger.Info("updateThresholdsAsync(...) called for",
		zap.String("Entity Key", entityKey.AsString()))
	go func() {
		thresholds, err := th.getThresholds(entityKey)
		if err == nil {
			var latestThresholds = map[string]*ThresholdDto{}
			for _, threshold := range thresholds {
				latestThresholds[threshold.ResourceURIPattern] = &threshold
			}
			th.thresholds.Store(entityKey.AsString(), latestThresholds)
		}
	}()
	return true
}

func (th *thresholdHelper) getThresholds(entityKey EntityKeyDto) ([]ThresholdDto, error) {
	var thresholds []ThresholdDto
	client := &http.Client{
		Timeout: time.Second * 5,
	}
	// Add all metrics in request body
	buf := &bytes.Buffer{}
	err := json.NewEncoder(buf).Encode(entityKey)
	if err != nil {
		th.logger.Error("Request payload JSON encoding error", zap.Error(err))
	}

	// Build request
	url := th.config.AssertsServer + "/v1/latency-thresholds"
	req, err := http.NewRequest("POST", url, bytes.NewReader(buf.Bytes()))
	if err != nil {
		th.logger.Error("Got error", zap.Error(err))
	}

	th.logger.Info("Fetching thresholds",
		zap.String("URL", url),
		zap.String("Entity Key", entityKey.AsString()),
	)

	// Add authentication headers
	// req.Header.Add("Authorization", "Basic ")

	// Make the call
	response, err := client.Do(req)

	// Handle response
	if err != nil {
		th.logger.Error("Failed to fetch thresholds",
			zap.String("Entity Key", entityKey.AsString()), zap.Error(err))
	} else if response.StatusCode == 200 {
		body, err := io.ReadAll(response.Body)
		if err == nil {
			err = json.Unmarshal(body, &thresholds)
		}
	} else {
		if body, err := io.ReadAll(response.Body); err == nil {
			th.logger.Error("Un-expected response",
				zap.String("Entity Key", entityKey.AsString()),
				zap.Int("status_code", response.StatusCode),
				zap.String("Response", string(body)),
				zap.Error(err))
		}
	}
	if response != nil && response.Body != nil {
		err = response.Body.Close()
	}
	return thresholds, err
}
