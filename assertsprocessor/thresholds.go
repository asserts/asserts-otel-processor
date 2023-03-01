package assertsprocessor

import (
	"bytes"
	"encoding/json"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/tilinna/clock"
	"go.uber.org/zap"
	"io"
	"net/http"
	"time"
)

type ThresholdDto struct {
	ResourceURIPattern string  `json:"request_context"`
	LatencyUpperBound  float64 `json:"upper_threshold"`
}

type thresholdHelper struct {
	config              *Config
	logger              *zap.Logger
	thresholds          cmap.ConcurrentMap[string, cmap.ConcurrentMap[string, ThresholdDto]]
	thresholdSyncTicker *clock.Ticker
	entityKeys          cmap.ConcurrentMap[string, EntityKeyDto]
	stop                chan bool
}

func (th *thresholdHelper) getThreshold(ns string, service string, request string) float64 {
	var entityKey = buildEntityKey(th.config, ns, service)
	_, found := th.entityKeys.Get(entityKey.AsString())
	if !found {
		th.entityKeys.Set(entityKey.AsString(), entityKey)
	}
	thresholds, found := th.thresholds.Get(entityKey.AsString())
	if !found {
		return th.config.DefaultLatencyThreshold
	} else {
		threshold, found := thresholds.Get(request)
		if !found {
			return th.config.DefaultLatencyThreshold
		} else {
			return threshold.LatencyUpperBound
		}
	}
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
			for _, val := range th.entityKeys.Items() {
				th.updateThresholdsAsync(val)
			}
		}
	}
}

func (th *thresholdHelper) updateThresholdsAsync(entityKey EntityKeyDto) bool {
	th.logger.Info("updateThresholdsAsync(...) called for",
		zap.String("Entity Key", entityKey.AsString()))
	go func() {
		thresholds, err := th.getThresholds(entityKey)
		if err == nil {
			var latestThresholds = cmap.New[ThresholdDto]()
			for _, threshold := range thresholds {
				latestThresholds.Set(threshold.ResourceURIPattern, threshold)
			}
			th.thresholds.Set(entityKey.AsString(), latestThresholds)
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
