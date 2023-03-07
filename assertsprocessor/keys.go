package assertsprocessor

import (
	"sort"
)

type EntityKeyDto struct {
	Type  string            `json:"type"`
	Name  string            `json:"name"`
	Scope map[string]string `json:"scope"`
}

func (ek *EntityKeyDto) AsString() string {
	var sortedKeys []string
	for key := range ek.Scope {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)
	var scopeString = "{"
	var i = 0
	for _, key := range sortedKeys {
		if i > 0 {
			scopeString = scopeString + ", "
		}
		scopeString = scopeString + key + "=" + ek.Scope[key]
		i = i + 1
	}
	scopeString = scopeString + "}"
	return scopeString + "#" + ek.Type + "#" + ek.Name
}

type RequestKey struct {
	entityKey EntityKeyDto
	request   string
}

func (rq *RequestKey) AsString() string {
	return rq.entityKey.AsString() + "#" + rq.request
}
