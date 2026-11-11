package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
	"github.com/jealous-sound/hoda-custom-backend/internal/economy"
	"github.com/jealous-sound/hoda-custom-backend/internal/maintenance"
	"github.com/jealous-sound/hoda-custom-backend/internal/ratelimit"
)

const testAdminToken = "test-admin-token-0123456789abcdef"

func newMaintenanceHarness(t *testing.T) *harness {
	return newHarnessWith(t, Config{OpenUnregistered: true, AdminToken: testAdminToken})
}

func (h *harness) admin(method, body string) (*http.Response, maintenanceResponse) {
	h.t.Helper()
	response, reply := h.do(method, adminMaintenancePath, testAdminToken, "", body)
	var status maintenanceResponse
	if response.StatusCode == http.StatusOK {
		if err := json.Unmarshal([]byte(reply), &status); err != nil {
			h.t.Fatalf("decode %s: %v", reply, err)
		}
	}
	return response, status
}

func (h *harness) scheduleIn(wait time.Duration) time.Time {
	h.t.Helper()
	response, status := h.admin("PUT", fmt.Sprintf(`{"in":%q}`, wait))
	at := h.now.Add(wait)
	if response.StatusCode != http.StatusOK || status.MaintenanceAt != at.Unix() {
		h.t.Fatalf("schedule in %v: %d %+v", wait, response.StatusCode, status)
	}
	return at
}

func (h *harness) announced() (clientTooOldResponse, string) {
	h.t.Helper()
	response, body := h.do("GET", clientUpdatePath, "", "", "")
	expectStatus(h.t, response, body, http.StatusOK)
	return decodeUpdate(h.t, body), body
}

func TestAdminRoutesAreAbsentWithoutAToken(t *testing.T) {
	h := newHarness(t)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		response, body := h.do(method, adminMaintenancePath, testAdminToken, "", `{"in":"30m"}`)
		expectStatus(t, response, body, http.StatusNotFound)
	}
	if update, _ := h.announced(); update.MaintenanceAt != 0 {
		t.Fatalf("maintenance was announced: %+v", update)
	}
}

func TestAdminRoutesNeedTheToken(t *testing.T) {
	h := newMaintenanceHarness(t)
	for _, token := range []string{"", "wrong", testAdminToken + "x", testAdminToken[:len(testAdminToken)-1]} {
		response, body := h.do("PUT", adminMaintenancePath, token, "", `{"in":"30m"}`)
		expectStatus(t, response, body, http.StatusUnauthorized)
	}
	if update, _ := h.announced(); update.MaintenanceAt != 0 {
		t.Fatalf("maintenance was announced without the token: %+v", update)
	}
}

func TestAdminGuessesAreLimitedPerVisitor(t *testing.T) {
	h := newHarnessWith(t, Config{
		OpenUnregistered: true, AdminToken: testAdminToken,
		Limits: Limits{AdminGuessesPerAddress: ratelimit.Rule{Burst: 2, Every: time.Minute}},
	})
	for range 2 {
		response, body := h.do("GET", adminMaintenancePath, "wrong", "", "")
		expectStatus(t, response, body, http.StatusUnauthorized)
	}
	response, body := h.do("GET", adminMaintenancePath, testAdminToken, "", "")
	expectStatus(t, response, body, http.StatusTooManyRequests)
}

func TestClientEndpointCarriesTheServerClockAndNoMaintenanceByDefault(t *testing.T) {
	h := newMaintenanceHarness(t)
	update, body := h.announced()
	if update.ServerTime != fixedNow.Unix() || strings.Contains(body, "maintenance_at") {
		t.Fatalf("body = %s", body)
	}
}

func TestScheduledMaintenanceIsAnnouncedToClients(t *testing.T) {
	h := newMaintenanceHarness(t)
	at := h.scheduleIn(45 * time.Minute)
	if update, body := h.announced(); update.MaintenanceAt != at.Unix() || update.ServerTime != fixedNow.Unix() {
		t.Fatalf("body = %s", body)
	}
	response, status := h.admin("GET", "")
	if response.StatusCode != http.StatusOK || status.MaintenanceAt != at.Unix() {
		t.Fatalf("admin status = %d %+v", response.StatusCode, status)
	}
}

func TestMaintenanceCanBeScheduledForAClockTime(t *testing.T) {
	h := newMaintenanceHarness(t)
	at := fixedNow.Add(2 * time.Hour)
	response, status := h.admin("PUT", fmt.Sprintf(`{"at":%q}`, at.UTC().Format(time.RFC3339)))
	if response.StatusCode != http.StatusOK || status.MaintenanceAt != at.Unix() {
		t.Fatalf("schedule = %d %+v", response.StatusCode, status)
	}
}

func TestMaintenanceRefusesUnclearOrPastTimes(t *testing.T) {
	h := newMaintenanceHarness(t)
	past := fixedNow.Add(-time.Minute).UTC().Format(time.RFC3339)
	for body, want := range map[string]int{
		``:                                 http.StatusBadRequest,
		`{}`:                               http.StatusBadRequest,
		`{"in":"soon"}`:                    http.StatusBadRequest,
		`{"at":"tomorrow"}`:                http.StatusBadRequest,
		`{"in":"30m","at":"` + past + `"}`: http.StatusBadRequest,
		`{"in":"0s"}`:                      http.StatusUnprocessableEntity,
		`{"in":"-5m"}`:                     http.StatusUnprocessableEntity,
		`{"at":"` + past + `"}`:            http.StatusUnprocessableEntity,
	} {
		response, reply := h.do("PUT", adminMaintenancePath, testAdminToken, "", body)
		expectStatus(t, response, reply, want)
	}
	if update, _ := h.announced(); update.MaintenanceAt != 0 {
		t.Fatalf("a refused time was announced: %+v", update)
	}
}

func TestCancelledMaintenanceIsNoLongerAnnounced(t *testing.T) {
	h := newMaintenanceHarness(t)
	h.scheduleIn(30 * time.Minute)
	response, status := h.admin("DELETE", "")
	if response.StatusCode != http.StatusOK || status.MaintenanceAt != 0 {
		t.Fatalf("cancel = %d %+v", response.StatusCode, status)
	}
	if _, body := h.announced(); strings.Contains(body, "maintenance_at") {
		t.Fatalf("body = %s", body)
	}
}

func TestMaintenanceStopsBeingAnnouncedOnceItOverruns(t *testing.T) {
	h := newMaintenanceHarness(t)
	at := h.scheduleIn(30 * time.Minute)
	*h.now = at.Add(maintenance.Overrun - time.Second)
	if update, body := h.announced(); update.MaintenanceAt != at.Unix() {
		t.Fatalf("body during the overrun = %s", body)
	}
	*h.now = at.Add(maintenance.Overrun)
	if _, body := h.announced(); strings.Contains(body, "maintenance_at") {
		t.Fatalf("body after the overrun = %s", body)
	}
}

func battleStartBody(t *testing.T, id string, request economy.BattleStartRequest) string {
	t.Helper()
	fields := map[string]any{}
	raw, _ := json.Marshal(request)
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["request_id"], fields["action"] = id, economy.ActionBattleStart
	raw, _ = json.Marshal(fields)
	return string(raw)
}

func (h *harness) expectHeld(player, token string, revision int64, body string) {
	h.t.Helper()
	response, reply := h.do("POST", "/v1/players/"+player+"/actions", token, fmt.Sprint(revision), body)
	expectStatus(h.t, response, reply, http.StatusUnprocessableEntity)
	var refused struct {
		Code     string `json:"code"`
		Revision int64  `json:"revision"`
	}
	if err := json.Unmarshal([]byte(reply), &refused); err != nil {
		h.t.Fatal(err)
	}
	if refused.Code != string(economy.Maintenance) || refused.Revision != revision {
		h.t.Fatalf("refusal = %s", reply)
	}
}

func TestBattlesInProgressFinishButNoneStartAroundTheRestart(t *testing.T) {
	h := newMaintenanceHarness(t)
	token := h.guildPlayer("local", "Hawke", 9, 10)
	request, revision := h.matchedStart("local", token, 1, "held", false, 12)
	request.Squad = h.storedSquad("local")
	seed, outcome := h.winningSeed(request)
	*h.seed = seed
	started := h.act("local", token, revision, battleStartBody(t, "held-start", request))

	at := h.scheduleIn(time.Hour)
	*h.now = at.Add(-maintenance.BattleLead)
	rematch := fmt.Sprintf(`{"request_id":"held-rematch","action":"%s","event":false}`, economy.ActionPvpMatch)
	h.expectHeld("local", token, started.Revision, rematch)

	finish := fmt.Sprintf(`{"request_id":"held-finish","action":"battle","battle_id":1,"won":true,"crit_gold":%d}`,
		battle.CritGoldFor(outcome.Crits))
	finished := h.act("local", token, started.Revision, finish)
	h.expectHeld("local", token, finished.Revision, battleStartBody(t, "held-again", request))

	*h.now = at.Add(maintenance.Overrun)
	h.matchedStart("local", token, finished.Revision, "resumed", false, 12)
}

func TestBattlesStartFreelyBeforeTheLead(t *testing.T) {
	h := newMaintenanceHarness(t)
	token := h.guildPlayer("local", "Hawke", 9, 10)
	at := h.scheduleIn(time.Hour)
	*h.now = at.Add(-maintenance.BattleLead - time.Second)
	h.matchedStart("local", token, 1, "early", false, 12)
}
