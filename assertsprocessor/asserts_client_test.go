package assertsprocessor

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"io"
	"net/http"
	"testing"
)

type (
	mockRestClient struct {
		expectedMethod  string
		expectedApi     string
		expectedPayload any
		expectedData    []byte
		expectedErr     error
	}
)

func (rc *mockRestClient) invoke(method string, api string, payload any) ([]byte, error) {
	rc.expectedMethod = method
	rc.expectedApi = api
	rc.expectedPayload = payload
	return rc.expectedData, rc.expectedErr
}

type (
	mockReadCloser struct {
		expectedData []byte
		expectedErr  error
	}
)

func (mrc *mockReadCloser) Read(p []byte) (n int, err error) {
	if mrc.expectedErr != io.EOF {
		return 0, mrc.expectedErr
	}
	if len(p) == 512 {
		copy(p, mrc.expectedData)
		return len(mrc.expectedData), nil
	} else {
		return 0, mrc.expectedErr
	}
}

func (mrc *mockReadCloser) Close() error { return nil }

func TestInvoke(t *testing.T) {
	logger, _ := zap.NewProduction()
	ac := assertsClient{
		logger: logger,
		config: &Config{
			Env:  "dev",
			Site: "us-west-2",
			AssertsServer: &map[string]string{
				"endpoint": "http://localhost:8031",
				"user":     "asserts",
				"password": "asserts",
			},
			AssertsTenant:           "bootstrap",
			DefaultLatencyThreshold: 0.5,
		},
	}

	invoke, err := ac.invoke(http.MethodPost, latencyThresholdsApi, "junit")
	assert.NotNil(t, err)
	assert.Nil(t, invoke)
}

func TestReadResponseBodySuccess(t *testing.T) {
	logger, _ := zap.NewProduction()
	ac := assertsClient{
		logger: logger,
	}

	mrc := &mockReadCloser{
		expectedData: []byte(`hello`),
		expectedErr:  io.EOF,
	}

	body, err := ac.readResponseBody(configApi, 200, mrc)

	assert.Nil(t, err)
	assert.Equal(t, mrc.expectedData, body)
}

func TestReadResponseBodyErrorStatusCode(t *testing.T) {
	logger, _ := zap.NewProduction()
	ac := assertsClient{
		logger: logger,
	}

	mrc := &mockReadCloser{
		expectedData: []byte(`Unauthorized access`),
		expectedErr:  io.EOF,
	}

	body, err := ac.readResponseBody(configApi, 401, mrc)

	assert.NotNil(t, err)
	assert.Equal(t, mrc.expectedData, body)
}

func TestReadResponseBodyError(t *testing.T) {
	logger, _ := zap.NewProduction()
	ac := assertsClient{
		logger: logger,
	}

	mrc := &mockReadCloser{
		expectedData: []byte(`Service Unavailable`),
		expectedErr:  fmt.Errorf("failed reading"),
	}

	body, err := ac.readResponseBody(configApi, 503, mrc)

	assert.NotNil(t, err)
	assert.Equal(t, 0, len(body))
}
