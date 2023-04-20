package assertsprocessor

import (
	"encoding/json"
	"github.com/tilinna/clock"
	"go.uber.org/zap"
	"net/http"
)

type configListener interface {
	isUpdated(prevConfig *Config, currentConfig *Config) bool
	onUpdate(currentConfig *Config) error
}

type configRefresh struct {
	config           *Config
	logger           *zap.Logger
	configSyncTicker *clock.Ticker
	stop             chan bool
	restClient       restClient
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
					cr.fetchAndUpdateConfig(cr.restClient)
				}
			}
		}()
	}
}

func (cr *configRefresh) fetchAndUpdateConfig(rc restClient) {
	latestConfig, err := cr.fetchConfig(rc)
	if err == nil {
		cr.updateConfig(latestConfig)
	}
}

func (cr *configRefresh) fetchConfig(rc restClient) (*Config, error) {
	var config = &Config{}
	body, err := rc.invoke(http.MethodGet, configApi, nil)
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
		// The config updates are accepted only when all the listeners consume the config updates without
		// error. This could potentially leave different components with different versions of the config
		cr.config = latestConfig
	}
}

func (cr *configRefresh) logConfig(config *Config) {
	cr.logger.Debug("Got config", zap.Any("config", config))
}
