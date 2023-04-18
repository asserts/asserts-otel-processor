package assertsprocessor

import (
	"encoding/json"
	"github.com/tilinna/clock"
	"go.uber.org/zap"
	"net/http"
)

type configListener interface {
	isUpdated(currConfig *Config, latestConfig *Config) bool
	onUpdate(config *Config) error
}

type configRefresh struct {
	config           *Config
	logger           *zap.Logger
	configSyncTicker *clock.Ticker
	stop             chan bool
	assertsClient    *assertsClient
	spanMatcher      *spanMatcher
	configListeners  []configListener
}

func (cr *configRefresh) stopUpdates() {
	go func() { cr.stop <- true }()
}

func (cr *configRefresh) startUpdates() {
	endPoint := (*(*cr.config).AssertsServer)["endpoint"]
	if endPoint != "" {
		go func() {
			for {
				select {
				case <-cr.stop:
					cr.logger.Info("Stopping collector config updates")
					return
				case <-cr.configSyncTicker.C:
					cr.logger.Info("Fetching collector config")
					cr.fetchAndUpdateConfig()
				}
			}
		}()
	}
}

func (cr *configRefresh) fetchAndUpdateConfig() {
	latestConfig, err := cr.fetchConfig()
	if err == nil {
		cr.updateConfig(latestConfig)
	}
}

func (cr *configRefresh) fetchConfig() (*Config, error) {
	var config *Config
	body, err := cr.assertsClient.invoke(http.MethodGet, configApi, nil)
	if err == nil {
		err = json.Unmarshal(body, config)
		if err == nil {
			cr.logConfig(config)
		} else {
			cr.logger.Error("Error unmarshalling config", zap.Error(err))
		}
	}

	return config, err
}

func (cr *configRefresh) updateConfig(latestConfig *Config) {
	err := error(nil)
	for _, listener := range cr.configListeners {
		if listener.isUpdated(cr.config, latestConfig) {
			err = listener.onUpdate(latestConfig)
		}
	}
	if err == nil {
		cr.config = latestConfig
	}
}

func (cr *configRefresh) logConfig(config *Config) {
	cr.logger.Debug("Got config", zap.Any("config", config))
}
