package assertsprocessor

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"go.uber.org/zap"
	"io"
	"net/http"
	"time"
)

const (
	latencyThresholdsApi = "/v1/latency-thresholds"
)

type assertsClient struct {
	config *Config
	logger *zap.Logger
}

func (ac *assertsClient) invoke(method string, api string, payload any) ([]byte, error) {
	client := &http.Client{
		Timeout: time.Second * 5,
	}
	var requestBody = make([]byte, 0)
	if http.MethodPost == method || http.MethodPut == method {
		// Encode request payload
		buf := &bytes.Buffer{}
		err := json.NewEncoder(buf).Encode(payload)
		if err != nil {
			ac.logger.Error("Request payload JSON encoding error", zap.Error(err))
			return nil, err
		}
		requestBody = buf.Bytes()
	}

	// Build request
	assertsServer := *ac.config.AssertsServer
	url := assertsServer["endpoint"] + api
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(requestBody))
	if err != nil {
		ac.logger.Error("Error creating new http request", zap.Error(err))
		return nil, err
	}

	ac.logger.Debug("Invoking", zap.String("Api", api))

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Accept", "application/json")
	// Add authentication headers
	if assertsServer["user"] != "" && assertsServer["password"] != "" {
		req.Header.Add("Authorization", "Basic "+basicAuth(assertsServer["user"], assertsServer["password"]))
	}

	// Make the call
	response, err := client.Do(req)
	var responseBody []byte = nil

	// Handle response
	if err != nil {
		ac.logger.Error("Failed to invoke",
			zap.String("Api", api),
			zap.Error(err),
		)
	} else if response.StatusCode == 200 {
		responseBody, err = io.ReadAll(response.Body)
		if err != nil {
			ac.logger.Debug("Got Response",
				zap.String("Api", api),
				zap.String("Body", string(responseBody)),
			)
		}
	} else {
		if responseBody, err = io.ReadAll(response.Body); err == nil {
			ac.logger.Error("Un-expected response",
				zap.String("Api", api),
				zap.Int("Status code", response.StatusCode),
				zap.String("Response", string(responseBody)),
				zap.Error(err))
		}
	}
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}

	return responseBody, err
}

func basicAuth(username string, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}
