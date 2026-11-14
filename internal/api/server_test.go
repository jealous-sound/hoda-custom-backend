package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/account"
	"github.com/jealous-sound/hoda-custom-backend/internal/guild"
	"github.com/jealous-sound/hoda-custom-backend/internal/maintenance"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
	"github.com/jealous-sound/hoda-custom-backend/internal/report"
	"github.com/jealous-sound/hoda-custom-backend/internal/session"
)

var fixedNow = time.Unix(1_800_000_000, 0)

const testPasswordIterations = 4

const validProfile = `{"version":1,"account":{"level":3,"experience":10,"energy":5,"energy_max":30,` +
	`"stamina":8,"stamina_max":8,"gold":100,"gems":7},"roster":{},"squads":[],"settings":{"fast_battles":true}}`

type harness struct {
	t        *testing.T
	now      *time.Time
	seed     *uint32
	server   *httptest.Server
	profiles *profile.Store
	dir      string
}

func newHarness(t *testing.T) *harness {
	return newHarnessWith(t, Config{OpenUnregistered: true})
}

func newHarnessWith(t *testing.T, config Config) *harness {
	dir := t.TempDir()
	current := fixedNow
	clock := func() time.Time { return current }
	store, err := profile.NewStore(dir, clock)
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := account.NewStore(dir, clock, account.WithPasswordIterations(testPasswordIterations))
	if err != nil {
		t.Fatal(err)
	}
	shopCatalog := unlevelledCatalog(t)
	guilds, err := guild.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	reports, err := report.NewLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	config.Maintenance, err = maintenance.Open(dir, clock)
	if err != nil {
		t.Fatal(err)
	}
	config.DataDir = dir
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	seed := uint32(1)
	config.BattleSeeds = func() (uint32, error) { return seed, nil }
	server := httptest.NewServer(
		NewServer(accounts, store, session.NewRegistry(clock, 0), shopCatalog, guilds, reports, clock, log, config).
			Handler(),
	)
	t.Cleanup(server.Close)
	return &harness{t: t, now: &current, seed: &seed, server: server, profiles: store, dir: dir}
}

func (h *harness) do(method, path, token, ifMatch, body string) (*http.Response, string) {
	h.t.Helper()
	request, err := http.NewRequest(method, h.server.URL+path, strings.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if ifMatch != "" {
		request.Header.Set("If-Match", ifMatch)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		h.t.Fatal(err)
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(response.Body)
	return response, string(payload)
}

func (h *harness) open(player string) string {
	h.t.Helper()
	response, body := h.do("POST", "/v1/sessions", "", "", `{"player_id":"`+player+`"}`)
	if response.StatusCode != http.StatusOK {
		h.t.Fatalf("open session: %d %s", response.StatusCode, body)
	}
	var opened sessionResponse
	if err := json.Unmarshal([]byte(body), &opened); err != nil {
		h.t.Fatal(err)
	}
	if opened.ServerTime != h.now.Unix() {
		h.t.Fatalf("server_time = %d, want %d", opened.ServerTime, h.now.Unix())
	}
	return opened.Token
}

func expectStatus(t *testing.T, response *http.Response, body string, want int) {
	t.Helper()
	if response.StatusCode != want {
		t.Fatalf("status = %d, want %d (%s)", response.StatusCode, want, body)
	}
}

func TestServerTimeReportsBackendClock(t *testing.T) {
	h := newHarness(t)
	response, body := h.do("GET", "/v1/time", "", "", "")
	expectStatus(t, response, body, http.StatusOK)
	if !strings.Contains(body, `"server_time":1800000000`) {
		t.Fatalf("body = %s", body)
	}
}

func TestProfileRoundTripAdvancesRevision(t *testing.T) {
	h := newHarness(t)
	token := h.open("local")
	path := "/v1/players/local/profile"

	response, body := h.do("GET", path, token, "", "")
	expectStatus(t, response, body, http.StatusNotFound)

	response, body = h.do("PUT", path, token, `"0"`, validProfile)
	expectStatus(t, response, body, http.StatusOK)
	if got := response.Header.Get(RevisionHeader); got != "1" {
		t.Fatalf("revision after create = %q", got)
	}

	response, body = h.do("GET", path, token, "", "")
	expectStatus(t, response, body, http.StatusOK)
	if !strings.Contains(body, `"fast_battles":true`) || response.Header.Get(RevisionHeader) != "1" {
		t.Fatalf("stored profile = %s rev %s", body, response.Header.Get(RevisionHeader))
	}
	if !strings.Contains(body, `"gold":1725`) || !strings.Contains(body, `"level":1`) {
		t.Fatalf("a new player's first upload set its own economy: %s", body)
	}

	response, body = h.do("PUT", path, token, "1", validProfile)
	expectStatus(t, response, body, http.StatusOK)
	if got := response.Header.Get(RevisionHeader); got != "2" {
		t.Fatalf("revision after update = %q", got)
	}
}

func TestStaleRevisionIsRejected(t *testing.T) {
	h := newHarness(t)
	token := h.open("local")
	path := "/v1/players/local/profile"
	h.do("PUT", path, token, "0", validProfile)

	response, body := h.do("PUT", path, token, "0", validProfile)
	expectStatus(t, response, body, http.StatusConflict)
	if got := response.Header.Get(RevisionHeader); got != "1" {
		t.Fatalf("conflict should report current revision, got %q", got)
	}
}

func TestProfileWriteRequiresIfMatch(t *testing.T) {
	h := newHarness(t)
	token := h.open("local")
	response, body := h.do("PUT", "/v1/players/local/profile", token, "", validProfile)
	expectStatus(t, response, body, http.StatusPreconditionRequired)
}

func TestProfileAccessRequiresOwnSession(t *testing.T) {
	h := newHarness(t)
	response, body := h.do("GET", "/v1/players/local/profile", "", "", "")
	expectStatus(t, response, body, http.StatusUnauthorized)

	other := h.open("other")
	response, body = h.do("GET", "/v1/players/local/profile", other, "", "")
	expectStatus(t, response, body, http.StatusForbidden)
}

func TestInvalidProfilesAreRejected(t *testing.T) {
	h := newHarness(t)
	token := h.open("local")
	cases := map[string]string{
		"not an object":    `[1,2,3]`,
		"missing roster":   `{"account":{"level":1,"experience":0,"energy":0,"energy_max":0,"stamina":0,"stamina_max":0,"gold":0,"gems":0},"squads":[]}`,
		"negative gems":    strings.Replace(validProfile, `"gems":7`, `"gems":-1`, 1),
		"string gold":      strings.Replace(validProfile, `"gold":100`, `"gold":"lots"`, 1),
		"missing currency": strings.Replace(validProfile, `,"gems":7`, ``, 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			response, payload := h.do("PUT", "/v1/players/local/profile", token, "0", body)
			expectStatus(t, response, payload, http.StatusUnprocessableEntity)
		})
	}
}

func TestSessionRejectsUnsafePlayerID(t *testing.T) {
	h := newHarness(t)
	response, body := h.do("POST", "/v1/sessions", "", "", `{"player_id":"../etc"}`)
	expectStatus(t, response, body, http.StatusBadRequest)
}

func (h *harness) createGuest() createPlayerResponse {
	h.t.Helper()
	response, body := h.do("POST", "/v1/players", "", "", "")
	expectStatus(h.t, response, body, http.StatusCreated)
	var created createPlayerResponse
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		h.t.Fatal(err)
	}
	return created
}

func TestGuestPlayerOpensSessionWithItsSecret(t *testing.T) {
	h := newHarness(t)
	guest := h.createGuest()
	if !profile.ValidPlayerID(guest.PlayerID) || !strings.HasPrefix(guest.PlayerID, "guest-") {
		t.Fatalf("player_id = %q", guest.PlayerID)
	}
	if guest.Secret == "" || guest.ServerTime != fixedNow.Unix() {
		t.Fatalf("created = %+v", guest)
	}

	request := `{"player_id":"` + guest.PlayerID + `","secret":"` + guest.Secret + `"}`
	response, body := h.do("POST", "/v1/sessions", "", "", request)
	expectStatus(t, response, body, http.StatusOK)
	var opened sessionResponse
	if err := json.Unmarshal([]byte(body), &opened); err != nil {
		t.Fatal(err)
	}
	response, body = h.do("PUT", "/v1/players/"+guest.PlayerID+"/profile", opened.Token, "0", validProfile)
	expectStatus(t, response, body, http.StatusOK)
}

func TestGuestPlayerRejectsWrongOrMissingSecret(t *testing.T) {
	h := newHarness(t)
	guest := h.createGuest()
	for name, request := range map[string]string{
		"wrong secret":   `{"player_id":"` + guest.PlayerID + `","secret":"nope"}`,
		"missing secret": `{"player_id":"` + guest.PlayerID + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			response, body := h.do("POST", "/v1/sessions", "", "", request)
			expectStatus(t, response, body, http.StatusUnauthorized)
		})
	}
}

func TestEachGuestGetsItsOwnPlayer(t *testing.T) {
	h := newHarness(t)
	first, second := h.createGuest(), h.createGuest()
	if first.PlayerID == second.PlayerID || first.Secret == second.Secret {
		t.Fatalf("guests share credentials: %+v %+v", first, second)
	}
}

func TestPlayerNameFollowsOriginalRules(t *testing.T) {
	h := newHarness(t)
	token := h.open("local")
	named := func(name string) string {
		return strings.Replace(validProfile, `"gems":7`, `"gems":7,"name":`+name, 1)
	}
	response, body := h.do("PUT", "/v1/players/local/profile", token, "0", named(`"Knight-Commander Meredith"`))
	expectStatus(t, response, body, http.StatusOK)
	for label, name := range map[string]string{
		"underscore": `"Grey_Warden"`,
		"empty":      `""`,
		"too long":   `"` + strings.Repeat("a", profile.MaxPlayerNameLength+1) + `"`,
		"not text":   `7`,
	} {
		t.Run(label, func(t *testing.T) {
			response, payload := h.do("PUT", "/v1/players/local/profile", token, "1", named(name))
			expectStatus(t, response, payload, http.StatusUnprocessableEntity)
		})
	}
}
