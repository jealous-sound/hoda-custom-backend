package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jealous-sound/hoda-custom-backend/internal/clientversion"
)

const testDownloadURL = "https://hodareborn.com/#download"

func version(t *testing.T, text string) clientversion.Version {
	t.Helper()
	parsed, ok := clientversion.Parse(text)
	if !ok {
		t.Fatalf("Parse(%q) failed", text)
	}
	return parsed
}

func newUpdateHarness(t *testing.T, minimum, latest string) *harness {
	updates := ClientUpdates{DownloadURL: testDownloadURL}
	if minimum != "" {
		updates.Minimum = version(t, minimum)
	}
	if latest != "" {
		newest := version(t, latest)
		updates.Latest = func() clientversion.Version { return newest }
	}
	return newHarnessWith(t, Config{OpenUnregistered: true, ClientUpdates: updates})
}

func (h *harness) doAs(userAgent, method, path, body string) (*http.Response, string) {
	h.t.Helper()
	request, err := http.NewRequest(method, h.server.URL+path, strings.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	request.Header.Set("User-Agent", userAgent)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		h.t.Fatal(err)
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(response.Body)
	return response, string(payload)
}

func decodeUpdate(t *testing.T, body string) clientTooOldResponse {
	t.Helper()
	var update clientTooOldResponse
	if err := json.Unmarshal([]byte(body), &update); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return update
}

func TestClientEndpointReportsLatestMinimumAndDownload(t *testing.T) {
	h := newUpdateHarness(t, "v0.4.0", "v0.5.1")
	response, body := h.doAs("HeroesOfDragonAge/v0.3.0", "GET", clientUpdatePath, "")
	expectStatus(t, response, body, http.StatusOK)
	update := decodeUpdate(t, body)
	if update.Latest != "v0.5.1" || update.Minimum != "v0.4.0" || update.DownloadURL != testDownloadURL {
		t.Fatalf("update = %+v", update)
	}
}

func TestClientEndpointNeverReportsLatestBelowMinimum(t *testing.T) {
	h := newUpdateHarness(t, "v0.6.0", "v0.5.1")
	_, body := h.doAs("", "GET", clientUpdatePath, "")
	if update := decodeUpdate(t, body); update.Latest != "v0.6.0" {
		t.Fatalf("latest = %q, want the minimum", update.Latest)
	}
}

func TestClientEndpointWithoutReleaseInfoReportsEmptyVersions(t *testing.T) {
	h := newUpdateHarness(t, "", "")
	_, body := h.doAs("", "GET", clientUpdatePath, "")
	if update := decodeUpdate(t, body); update.Latest != "" || update.Minimum != "" {
		t.Fatalf("update = %+v", update)
	}
}

func TestClientOlderThanMinimumGetsUpgradeRequired(t *testing.T) {
	h := newUpdateHarness(t, "v0.4.0", "v0.5.1")
	for _, path := range []string{"/v1/sessions", "/v1/players", "/v1/players/local/profile", "/v1/time"} {
		response, body := h.doAs("HeroesOfDragonAge/v0.3.7", "POST", path, `{"player_id":"local"}`)
		expectStatus(t, response, body, http.StatusUpgradeRequired)
		update := decodeUpdate(t, body)
		if update.Minimum != "v0.4.0" || update.Latest != "v0.5.1" || update.DownloadURL != testDownloadURL ||
			update.Error == "" {
			t.Fatalf("%s: update = %+v", path, update)
		}
	}
}

func TestReleaseCandidateOfTheMinimumIsTooOld(t *testing.T) {
	h := newUpdateHarness(t, "v0.4.0", "")
	response, body := h.doAs("HeroesOfDragonAge/v0.4.0-rc.2", "POST", "/v1/sessions", `{"player_id":"local"}`)
	expectStatus(t, response, body, http.StatusUpgradeRequired)
}

func TestCurrentAndUnversionedClientsPassTheMinimum(t *testing.T) {
	h := newUpdateHarness(t, "v0.4.0", "v0.5.1")
	for _, userAgent := range []string{
		"HeroesOfDragonAge/v0.4.0", "HeroesOfDragonAge/v0.4.0-3-gabc1234", "HeroesOfDragonAge/v0.5.1",
		"HeroesOfDragonAge/dev", "HeroesOfDragonAge/abc1234-dirty", "Go-http-client/1.1", "",
	} {
		response, body := h.doAs(userAgent, "POST", "/v1/sessions", `{"player_id":"local"}`)
		expectStatus(t, response, body, http.StatusOK)
	}
}

func TestTooOldClientStillReachesTheClientEndpointAndHealth(t *testing.T) {
	h := newUpdateHarness(t, "v0.4.0", "")
	for _, path := range []string{clientUpdatePath, "/healthz"} {
		response, body := h.doAs("HeroesOfDragonAge/v0.1.0", "GET", path, "")
		expectStatus(t, response, body, http.StatusOK)
	}
}

func TestWithoutMinimumEveryClientPasses(t *testing.T) {
	h := newUpdateHarness(t, "", "v0.5.1")
	response, body := h.doAs("HeroesOfDragonAge/v0.0.1", "POST", "/v1/sessions", `{"player_id":"local"}`)
	expectStatus(t, response, body, http.StatusOK)
}
