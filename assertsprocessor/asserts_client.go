package assertsprocessor

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"go.uber.org/zap"
	"io"
	"net/http"
	"time"
)

const (
	configApi            = "/v1/config/otel-collector"
	latencyThresholdsApi = "/v1/latency-thresholds"
)

type restClient interface {
	invoke(method string, api string, payload any) ([]byte, error)
}

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
	req, err := http.NewRequest(method, url, bytes.NewReader(requestBody))
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
	} else {
		responseBody, err = ac.readResponseBody(api, response.StatusCode, response.Body)
	}

	return responseBody, err
}

func (ac *assertsClient) readResponseBody(api string, statusCode int, body io.ReadCloser) ([]byte, error) {
	responseBody, err := io.ReadAll(body)
	if err == nil {
		if statusCode == 200 {
			ac.logger.Debug("Got Response",
				zap.String("Api", api),
				zap.String("Body", string(responseBody)),
			)
		} else {
			bodyString := string(responseBody)
			err = errors.New(bodyString)
			ac.logger.Info("Un-expected response",
				zap.String("Api", api),
				zap.Int("Status code", statusCode),
				zap.String("Response", bodyString),
			)
		}
	} else {
		ac.logger.Error("Error reading response body",
			zap.String("Api", api),
			zap.Int("Status code", statusCode),
			zap.Error(err),
		)
	}
	_ = body.Close()
	return responseBody, err
}

func basicAuth(username string, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

var (
	restClientFactory = createRestClient
)

func createRestClient(logger *zap.Logger, pConfig *Config) restClient {
	return &assertsClient{
		config: pConfig,
		logger: logger,
	}
}
