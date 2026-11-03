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

	"github.com/danbestan/hoda-custom-backend/internal/profile"
	"github.com/danbestan/hoda-custom-backend/internal/session"
)

var fixedNow = time.Unix(1_800_000_000, 0)

const validProfile = `{"version":1,"account":{"level":3,"experience":10,"energy":5,"energy_max":30,` +
	`"stamina":8,"stamina_max":8,"gold":100,"gems":7},"roster":{},"squads":[]}`

type harness struct {
	t      *testing.T
	server *httptest.Server
}

func newHarness(t *testing.T) *harness {
	store, err := profile.NewStore(t.TempDir(), func() time.Time { return fixedNow })
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(NewServer(store, session.NewRegistry(), func() time.Time { return fixedNow }, log).Handler())
	t.Cleanup(server.Close)
	return &harness{t: t, server: server}
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
	if opened.ServerTime != fixedNow.Unix() {
		h.t.Fatalf("server_time = %d, want %d", opened.ServerTime, fixedNow.Unix())
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
	if body != validProfile || response.Header.Get(RevisionHeader) != "1" {
		t.Fatalf("stored profile = %s rev %s", body, response.Header.Get(RevisionHeader))
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
