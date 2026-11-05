package shop

import (
	"encoding/json"
	"reflect"
)

var serverOwnedStoreFields = []string{"opened_total", "opened_by_pack", "champion_since_legendary"}

func KeepsServerOwnedFields(previous, next json.RawMessage) error {
	if len(previous) == 0 {
		return nil
	}
	before, err := parseDocument(previous)
	if err != nil {
		return nil
	}
	after, err := parseDocument(next)
	if err != nil {
		return refuse(BadProfile, "%v", err)
	}
	beforeStore, beforeOK := before["store"].(map[string]any)
	if !beforeOK {
		return nil
	}
	afterStore, _ := after["store"].(map[string]any)
	for _, field := range serverOwnedStoreFields {
		if !reflect.DeepEqual(beforeStore[field], afterStore[field]) {
			return refuse(BadProfile, "store.%s only changes through store purchases", field)
		}
	}
	return nil
}
