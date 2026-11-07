package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
	"github.com/jealous-sound/hoda-custom-backend/internal/ratelimit"
)

const (
	cloudflareEdge = "172.68.10.20"
	directVisitor  = "203.0.113.7"
)

func (h *harness) doFrom(method, path, token, body string, headers map[string]string) (*http.Response, string) {
	h.t.Helper()
	request, err := http.NewRequest(method, h.server.URL+path, strings.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		h.t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		h.t.Fatal(err)
	}
	return response, string(raw)
}

func viaCloudflare(visitor string) map[string]string {
	return map[string]string{forwardedForHeader: visitor + ", " + cloudflareEdge, cloudflareClientHeader: visitor}
}

func expectTooMany(t *testing.T, response *http.Response, body string) {
	t.Helper()
	expectStatus(t, response, body, http.StatusTooManyRequests)
	if response.Header.Get("Retry-After") == "" {
		t.Fatal("429 without Retry-After")
	}
}

func TestUnregisteredPlayerCannotOpenASession(t *testing.T) {
	h := newHarnessWith(t, Config{})
	response, body := h.do("POST", "/v1/sessions", "", "", `{"player_id":"local"}`)
	expectStatus(t, response, body, http.StatusNotFound)
	guest := h.createGuest()
	response, body = h.do("POST", "/v1/sessions", "", "",
		`{"player_id":"`+guest.PlayerID+`","secret":"`+guest.Secret+`"}`)
	expectStatus(t, response, body, http.StatusOK)
	response, body = h.do("POST", "/v1/sessions", "", "", `{"player_id":"`+guest.PlayerID+`","secret":"wrong"}`)
	expectStatus(t, response, body, http.StatusUnauthorized)
}

func TestAccountCreationIsLimitedPerVisitor(t *testing.T) {
	h := newHarnessWith(t, Config{Limits: Limits{AccountsPerAddress: ratelimit.Rule{Burst: 2, Every: time.Hour}}})
	for range 2 {
		response, body := h.doFrom("POST", "/v1/players", "", "", viaCloudflare("198.51.100.1"))
		expectStatus(t, response, body, http.StatusCreated)
	}
	response, body := h.doFrom("POST", "/v1/players", "", "", viaCloudflare("198.51.100.1"))
	expectTooMany(t, response, body)
	response, body = h.doFrom("POST", "/v1/players", "", "", viaCloudflare("198.51.100.2"))
	expectStatus(t, response, body, http.StatusCreated)
}

func TestSpoofedCloudflareHeaderDoesNotEscapeTheLimit(t *testing.T) {
	h := newHarnessWith(t, Config{Limits: Limits{AccountsPerAddress: ratelimit.Rule{Burst: 1, Every: time.Hour}}})
	spoofed := func(fake string) map[string]string {
		return map[string]string{forwardedForHeader: directVisitor, cloudflareClientHeader: fake}
	}
	response, body := h.doFrom("POST", "/v1/players", "", "", spoofed("198.51.100.1"))
	expectStatus(t, response, body, http.StatusCreated)
	response, body = h.doFrom("POST", "/v1/players", "", "", spoofed("198.51.100.2"))
	expectTooMany(t, response, body)
}

func TestAccountCreationHasAGlobalCeiling(t *testing.T) {
	h := newHarnessWith(t, Config{Limits: Limits{AccountsTotal: ratelimit.Rule{Burst: 1, Every: time.Hour}}})
	response, body := h.doFrom("POST", "/v1/players", "", "", viaCloudflare("198.51.100.1"))
	expectStatus(t, response, body, http.StatusCreated)
	response, body = h.doFrom("POST", "/v1/players", "", "", viaCloudflare("198.51.100.2"))
	expectTooMany(t, response, body)
}

func TestSessionsAreLimitedPerVisitor(t *testing.T) {
	h := newHarnessWith(t, Config{
		OpenUnregistered: true, Limits: Limits{SessionsPerAddress: ratelimit.Rule{Burst: 1, Every: time.Hour}},
	})
	response, body := h.doFrom("POST", "/v1/sessions", "", `{"player_id":"local"}`, viaCloudflare("198.51.100.1"))
	expectStatus(t, response, body, http.StatusOK)
	response, body = h.doFrom("POST", "/v1/sessions", "", `{"player_id":"local"}`, viaCloudflare("198.51.100.1"))
	expectTooMany(t, response, body)
}

func TestPlayerRequestsAreLimited(t *testing.T) {
	h := newHarnessWith(t, Config{
		OpenUnregistered: true, Limits: Limits{RequestsPerPlayer: ratelimit.Rule{Burst: 2, Every: time.Hour}},
	})
	token := h.open("local")
	other := h.open("other")
	for range 2 {
		response, body := h.do("GET", "/v1/players/local/profile", token, "", "")
		if response.StatusCode == http.StatusTooManyRequests {
			t.Fatalf("limited inside the burst: %s", body)
		}
	}
	response, body := h.do("GET", "/v1/players/local/profile", token, "", "")
	expectTooMany(t, response, body)
	response, body = h.do("GET", "/v1/players/other/profile", other, "", "")
	if response.StatusCode == http.StatusTooManyRequests {
		t.Fatalf("another player was limited by local's requests: %s", body)
	}
}

func TestChatPostsAreLimitedButPollingIsNot(t *testing.T) {
	h := newHarnessWith(t, Config{
		OpenUnregistered: true, Limits: Limits{ChatPerPlayer: ratelimit.Rule{Burst: 1, Every: time.Hour}},
	})
	token := h.open("local")
	response, body := h.do("POST", "/v1/players/local/guild/messages", token, "", `{"text":"hello"}`)
	if response.StatusCode == http.StatusTooManyRequests {
		t.Fatalf("first chat line was limited: %s", body)
	}
	response, body = h.do("POST", "/v1/players/local/guild/messages", token, "", `{"text":"again"}`)
	expectTooMany(t, response, body)
	response, body = h.do("POST", "/v1/players/local/guild/messages", token, "", `{"after":0}`)
	if response.StatusCode == http.StatusTooManyRequests {
		t.Fatalf("polling chat was limited: %s", body)
	}
}

func TestAbuseReportsAreLimited(t *testing.T) {
	h := newHarnessWith(t, Config{
		OpenUnregistered: true, Limits: Limits{ReportsPerPlayer: ratelimit.Rule{Burst: 1, Every: time.Hour}},
	})
	token := h.open("local")
	report := `{"kind":"guild","abuse_type":"OTHER","guild_id":"missing"}`
	response, body := h.do("POST", "/v1/players/local/reports", token, "", report)
	if response.StatusCode == http.StatusTooManyRequests {
		t.Fatalf("first report was limited: %s", body)
	}
	response, body = h.do("POST", "/v1/players/local/reports", token, "", report)
	expectTooMany(t, response, body)
}

func TestOversizedProfileIsRefused(t *testing.T) {
	h := newHarness(t)
	token := h.open("local")
	padding := strings.Repeat("x", profile.MaxProfileBytes)
	oversized := `{"account":{},"roster":{},"squads":[],"padding":"` + padding + `"}`
	response, body := h.do("PUT", "/v1/players/local/profile", token, `"0"`, oversized)
	expectStatus(t, response, body, http.StatusUnprocessableEntity)
}

func TestClientAddressTrustsCloudflareOnlyFromItsEdge(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		remote  string
		want    string
	}{
		{"through cloudflare", viaCloudflare("198.51.100.9"), "10.0.0.1:5000", "198.51.100.9"},
		{"spoofed header", map[string]string{forwardedForHeader: directVisitor, cloudflareClientHeader: "1.1.1.1"},
			"10.0.0.1:5000", directVisitor},
		{"forged hops before the edge", map[string]string{forwardedForHeader: "1.1.1.1, " + directVisitor},
			"10.0.0.1:5000", directVisitor},
		{"real ip header", map[string]string{realIPHeader: directVisitor}, "10.0.0.1:5000", directVisitor},
		{"direct connection", nil, directVisitor + ":5000", directVisitor},
		{"cloudflare ipv6 edge", map[string]string{forwardedForHeader: "2606:4700::1", cloudflareClientHeader: "2001:db8::5"},
			"10.0.0.1:5000", "2001:db8::5"},
	}
	for _, c := range cases {
		request := httptest.NewRequest("GET", "/", nil)
		request.RemoteAddr = c.remote
		for name, value := range c.headers {
			request.Header.Set(name, value)
		}
		if got := clientAddress(request, false); got != c.want {
			t.Errorf("%s: client address %q, want %q", c.name, got, c.want)
		}
	}
}

func TestTrustedCloudflareHeaderWinsBehindAnotherEdge(t *testing.T) {
	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set(forwardedForHeader, "198.51.100.9, "+cloudflareEdge+", 152.233.43.34")
	request.Header.Set(cloudflareClientHeader, "198.51.100.9")
	if got := clientAddress(request, false); got != "152.233.43.34" {
		t.Fatalf("untrusted: client address %q, want the edge hop", got)
	}
	if got := clientAddress(request, true); got != "198.51.100.9" {
		t.Fatalf("trusted: client address %q, want the Cloudflare visitor", got)
	}
	request.Header.Del(cloudflareClientHeader)
	if got := clientAddress(request, true); got != "152.233.43.34" {
		t.Fatalf("trusted without the header: client address %q, want the edge hop", got)
	}
}

func TestTrustedCloudflareHeaderSeparatesVisitorsBehindOneEdge(t *testing.T) {
	h := newHarnessWith(t, Config{
		TrustCloudflareHeader: true,
		Limits:                Limits{AccountsPerAddress: ratelimit.Rule{Burst: 1, Every: time.Hour}},
	})
	behindRailway := func(visitor string) map[string]string {
		return map[string]string{
			forwardedForHeader:     visitor + ", " + cloudflareEdge + ", 152.233.43.34",
			cloudflareClientHeader: visitor,
		}
	}
	response, body := h.doFrom("POST", "/v1/players", "", "", behindRailway("198.51.100.1"))
	expectStatus(t, response, body, http.StatusCreated)
	response, body = h.doFrom("POST", "/v1/players", "", "", behindRailway("198.51.100.2"))
	expectStatus(t, response, body, http.StatusCreated)
	response, body = h.doFrom("POST", "/v1/players", "", "", behindRailway("198.51.100.1"))
	expectTooMany(t, response, body)
}
