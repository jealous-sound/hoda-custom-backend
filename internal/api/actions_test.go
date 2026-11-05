package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const shopProfile = `{"version":9,"account":{"level":3,"experience":10,"energy":5,"energy_max":30,` +
	`"energy_regen_timer":12.5,"stamina":8,"stamina_max":8,"stamina_regen_timer":0,"gold":1000,"gems":7},` +
	`"store":{"rng_state":1,"opened_total":0,"opened_by_pack":[0,0,0,0],"champion_since_legendary":0,` +
	`"vouchers":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]},` +
	`"roster":{"starter_count":0,"next_instance_id":1,"starters":[],"additions":[]},"squads":[],` +
	`"runes":{"next_uuid":1,"seen":0,"items":[],"squads":[]}}`

const recruitPurchase = `{"request_id":"buy-recruit-1","pack":"recruit","currency":"gold","price":350}`

type shopResult struct {
	Revision int64 `json:"revision"`
	Profile  struct {
		Account struct{ Gold, Gems, Energy int }
		Store   struct {
			OpenedTotal int `json:"opened_total"`
		}
		Roster struct {
			Additions []struct{ ID string }
		}
	}
	Granted struct {
		Heroes []struct {
			ID         string
			InstanceID int `json:"instance_id"`
		}
	}
}

func (h *harness) shopPlayer() string {
	h.t.Helper()
	token := h.open("local")
	if _, err := h.profiles.Put("local", 0, json.RawMessage(shopProfile)); err != nil {
		h.t.Fatal(err)
	}
	return token
}

func decodeShop(t *testing.T, body string) shopResult {
	t.Helper()
	var result shopResult
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return result
}

func TestPurchaseChargesAndReturnsTheNewProfile(t *testing.T) {
	h := newHarness(t)
	token := h.shopPlayer()
	response, body := h.do("POST", "/v1/players/local/store/purchases", token, "1", recruitPurchase)
	expectStatus(t, response, body, http.StatusOK)
	result := decodeShop(t, body)
	if result.Revision != 2 || response.Header.Get(RevisionHeader) != "2" {
		t.Fatalf("revision = %d", result.Revision)
	}
	if result.Profile.Account.Gold != 650 || len(result.Granted.Heroes) != 1 ||
		len(result.Profile.Roster.Additions) != 1 || result.Profile.Store.OpenedTotal != 1 {
		t.Fatalf("result = %+v", result)
	}

	response, stored := h.do("GET", "/v1/players/local/profile", token, "", "")
	expectStatus(t, response, stored, http.StatusOK)
	if !strings.Contains(stored, `"gold":650`) {
		t.Fatalf("stored profile was not charged: %s", stored)
	}
}

func TestRetriedPurchaseIsNotChargedTwice(t *testing.T) {
	h := newHarness(t)
	token := h.shopPlayer()
	_, first := h.do("POST", "/v1/players/local/store/purchases", token, "1", recruitPurchase)
	response, second := h.do("POST", "/v1/players/local/store/purchases", token, "1", recruitPurchase)
	expectStatus(t, response, second, http.StatusOK)
	a, b := decodeShop(t, first), decodeShop(t, second)
	if b.Revision != 2 || b.Profile.Account.Gold != 650 || b.Granted.Heroes[0].ID != a.Granted.Heroes[0].ID {
		t.Fatalf("replay = %+v, first = %+v", b, a)
	}
}

func TestPurchaseNeedsTheCurrentRevision(t *testing.T) {
	h := newHarness(t)
	token := h.shopPlayer()
	response, body := h.do("POST", "/v1/players/local/store/purchases", token, "0", recruitPurchase)
	expectStatus(t, response, body, http.StatusConflict)
	response, body = h.do("POST", "/v1/players/local/store/purchases", token, "", recruitPurchase)
	expectStatus(t, response, body, http.StatusPreconditionRequired)
}

func TestRefusedPurchaseExplainsWhy(t *testing.T) {
	h := newHarness(t)
	token := h.shopPlayer()
	request := `{"request_id":"buy-champ-1","pack":"champion","currency":"gems","price":30}`
	response, body := h.do("POST", "/v1/players/local/store/purchases", token, "1", request)
	expectStatus(t, response, body, http.StatusUnprocessableEntity)
	if !strings.Contains(body, `"code":"cant_afford"`) || response.Header.Get(RevisionHeader) != "1" {
		t.Fatalf("body = %s", body)
	}
	response, body = h.do("POST", "/v1/players/local/store/purchases", token, "1", `{"pack":"recruit"}`)
	expectStatus(t, response, body, http.StatusBadRequest)
}

func TestUploadKeepsServerOwnedFields(t *testing.T) {
	h := newHarness(t)
	token := h.shopPlayer()
	forged := strings.Replace(shopProfile, `"opened_total":0,"opened_by_pack":[0,0,0,0]`,
		`"opened_total":3,"opened_by_pack":[3,0,0,0]`, 1)
	forged = strings.Replace(forged, `"gold":1000`, `"gold":99999`, 1)
	forged = strings.Replace(forged, `"rng_state":1`, `"rng_state":77`, 1)
	response, body := h.do("PUT", "/v1/players/local/profile", token, "1", forged)
	expectStatus(t, response, body, http.StatusOK)
	response, stored := h.do("GET", "/v1/players/local/profile", token, "", "")
	expectStatus(t, response, stored, http.StatusOK)
	for _, kept := range []string{`"gold":1000`, `"opened_total":0`, `"rng_state":77`} {
		if !strings.Contains(stored, kept) {
			t.Errorf("stored profile lacks %s: %s", kept, stored)
		}
	}
}

func TestRefillSpendsGems(t *testing.T) {
	h := newHarness(t)
	token := h.shopPlayer()
	request := `{"request_id":"refill-energy-1","kind":"energy"}`
	response, body := h.do("POST", "/v1/players/local/refills", token, "1", request)
	expectStatus(t, response, body, http.StatusOK)
	result := decodeShop(t, body)
	if result.Profile.Account.Energy != 30 || result.Profile.Account.Gems != 4 {
		t.Fatalf("account = %+v", result.Profile.Account)
	}
	response, body = h.do("POST", "/v1/players/local/refills", token, "2",
		`{"request_id":"refill-stamina-1","kind":"stamina"}`)
	expectStatus(t, response, body, http.StatusUnprocessableEntity)
}
