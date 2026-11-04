package profile

import "encoding/json"

type progressMarkers struct {
	Account struct {
		Name string `json:"name"`
	} `json:"account"`
	Tutorial *struct {
		Complete bool `json:"complete"`
	} `json:"tutorial"`
}

func ShowsProgress(profile json.RawMessage) bool {
	var markers progressMarkers
	if err := json.Unmarshal(profile, &markers); err != nil {
		return true
	}
	savedBeforeTutorialExisted := markers.Tutorial == nil
	return markers.Account.Name != "" || savedBeforeTutorialExisted || markers.Tutorial.Complete
}
