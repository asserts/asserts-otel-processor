package assertsprocessor

import "sort"

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
	for _, key := range sortedKeys {
		if len(scopeString) > 0 {
			scopeString = scopeString + ", "
		}
		scopeString = scopeString + key + "=" + ek.Scope[key]
	}
	scopeString = scopeString + "}"
	return scopeString + "/" + ek.Type + "/" + ek.Name
}
