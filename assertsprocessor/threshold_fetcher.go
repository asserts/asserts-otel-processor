package assertsprocessor

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"go.uber.org/zap"
	"io"
	"net/http"
	"sync"
	"time"
)

func (p *assertsProcessorImpl) fetchThresholds() {
	for {
		select {
		case <-p.done:
			return
		case <-p.thresholdSyncTicker.C:
			p.logger.Info("Fetching thresholds")
			p.latencyBounds.Range(p.updateThresholdsAsync)
		}
	}
}

func (p *assertsProcessorImpl) updateThresholdsAsync(_ any, value any) bool {
	var entityKey, _ = value.(EntityKeyDto)
	p.logger.Info("sync.Map.Range(...) called for",
		zap.String("Entity Key", entityKey.AsString()))
	go func() {
		thresholds, err := p.getThresholds(entityKey)
		if err == nil {
			var latestThresholds sync.Map
			for _, threshold := range thresholds {
				latestThresholds.Store(threshold.ResourceURIPattern, threshold)
			}
			p.latencyBounds.Store(entityKey.AsString(), &latestThresholds)
		}
	}()
	return true
}

func (p *assertsProcessorImpl) getThresholds(entityKey EntityKeyDto) ([]EntityThresholdDto, error) {
	var thresholds []EntityThresholdDto
	client := &http.Client{
		Timeout: time.Second * 10,
	}
	// Add all metrics in request body
	buf := &bytes.Buffer{}
	err := json.NewEncoder(buf).Encode(entityKey)
	if err != nil {
		p.logger.Error("Request payload JSON encoding error", zap.Error(err))
	}

	// Build request
	url := p.config.AssertsServer + "/v1/latency-thresholds"
	req, err := http.NewRequest("POST", url, bytes.NewReader(buf.Bytes()))
	if err != nil {
		p.logger.Error("Got error", zap.Error(err))
	}

	p.logger.Info("Fetching thresholds",
		zap.String("URL", url),
		zap.String("Entity Key", entityKey.AsString()),
	)

	// Add authentication headers
	// req.Header.Add("Authorization", "Basic ")

	// Make the call
	response, err := client.Do(req)

	// Handle response
	if err != nil {
		p.logger.Error("Failed to fetch thresholds",
			zap.String("Entity Key", entityKey.AsString()), zap.Error(err))
	} else if response.StatusCode == 200 {
		body, err := io.ReadAll(response.Body)
		if err == nil {
			err = json.Unmarshal(body, &thresholds)
		}
	} else {
		if b, err := io.ReadAll(response.Body); err == nil {
			p.logger.Error("Un-expected response",
				zap.String("Entity Key", entityKey.AsString()),
				zap.Int("status_code", response.StatusCode),
				zap.String("Response", string(b)),
				zap.Error(err))
		}
	}
	err = response.Body.Close()
	if err != nil {
		p.logger.Error("Failed to close response body", zap.Error(err))
	}
	return thresholds, err
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}
