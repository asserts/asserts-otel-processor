package assertsprocessor

import "sort"

type LatencyBound struct {
	Lower float64
	Upper float64
}

type thresholdRequest struct {
	entityKey       EntityKeyDto
	responseChannel chan LatencyBound
}

type EntityKeyDto struct {
	EntityType string            `json:"type"`
	Name       string            `json:"name"`
	Scope      map[string]string `json:"scope"`
}

func (ek *EntityKeyDto) AsString() string {
	var sortedKeys []string
	for key, _ := range ek.Scope {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)
	var scopeString = "{"
	for _, key := range sortedKeys {
		if len(scopeString) > 0 {
			scopeString = scopeString + ", "
		}
		scopeString = scopeString + key + "=" + ek.Scope[key]
	}
	scopeString = scopeString + "}"
	return scopeString + "/" + ek.EntityType + "/" + ek.Name
}

type EntityThresholdDto struct {
	ResourceURIPattern string `json:"request_context"`
	LatencyUpperBound  string `json:"upper_threshold"`
}
