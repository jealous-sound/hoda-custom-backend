package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

const (
	testWebOrigin   = "https://play.example.test"
	strangerOrigin  = "https://elsewhere.example.test"
	outdatedAgent   = "HeroesOfDragonAge/v0.3.7"
	allowedMinimum  = "v0.4.0"
	profileOfLocal  = "/v1/players/local/profile"
	requestedByPage = "authorization, content-type, if-match"
)

func newWebHarness(t *testing.T, config Config) *harness {
	config.OpenUnregistered = true
	config.WebOrigins = []string{testWebOrigin}
	return newHarnessWith(t, config)
}

func (h *harness) fromPage(method, path string, headers map[string]string) *http.Response {
	h.t.Helper()
	request, err := http.NewRequest(method, h.server.URL+path, strings.NewReader(""))
	if err != nil {
		h.t.Fatal(err)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		h.t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.ReadAll(response.Body)
	return response
}

func preflightFrom(origin, method string) map[string]string {
	return map[string]string{
		"Origin":                         origin,
		"Access-Control-Request-Method":  method,
		"Access-Control-Request-Headers": requestedByPage,
	}
}

func TestPageOriginReadsResponsesAndTheRevisionHeader(t *testing.T) {
	h := newWebHarness(t, Config{})
	token := h.open("local")
	response := h.fromPage("GET", profileOfLocal, map[string]string{
		"Origin": testWebOrigin, "Authorization": "Bearer " + token,
	})
	if got := response.Header.Get("Access-Control-Allow-Origin"); got != testWebOrigin {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
	if got := response.Header.Get("Access-Control-Expose-Headers"); got != RevisionHeader {
		t.Fatalf("Access-Control-Expose-Headers = %q", got)
	}
	if got := response.Header.Values("Vary"); len(got) == 0 || !strings.Contains(strings.Join(got, ","), "Origin") {
		t.Fatalf("Vary = %q", got)
	}
}

func TestOtherOriginsGetNoCrossOriginAnswer(t *testing.T) {
	h := newWebHarness(t, Config{})
	response := h.fromPage("GET", "/v1/time", map[string]string{"Origin": strangerOrigin})
	if response.StatusCode != http.StatusOK || response.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("status %d, Access-Control-Allow-Origin %q", response.StatusCode,
			response.Header.Get("Access-Control-Allow-Origin"))
	}
}

func TestPreflightAllowsTheClientsMethodsAndHeaders(t *testing.T) {
	h := newWebHarness(t, Config{})
	response := h.fromPage("OPTIONS", profileOfLocal, preflightFrom(testWebOrigin, "PUT"))
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status %d", response.StatusCode)
	}
	if got := response.Header.Get("Access-Control-Allow-Origin"); got != testWebOrigin {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
	if got := response.Header.Get("Access-Control-Allow-Methods"); !strings.Contains(got, "PUT") {
		t.Fatalf("Access-Control-Allow-Methods = %q", got)
	}
	allowed := strings.ToLower(response.Header.Get("Access-Control-Allow-Headers"))
	for _, name := range strings.Split(requestedByPage, ", ") {
		if !strings.Contains(allowed, name) {
			t.Fatalf("Access-Control-Allow-Headers = %q lacks %s", allowed, name)
		}
	}
	if got := response.Header.Get("Access-Control-Max-Age"); got != "7200" {
		t.Fatalf("Access-Control-Max-Age = %q", got)
	}
}

func TestPreflightFromAnotherOriginIsRefused(t *testing.T) {
	h := newWebHarness(t, Config{})
	response := h.fromPage("OPTIONS", profileOfLocal, preflightFrom(strangerOrigin, "PUT"))
	if response.StatusCode != http.StatusForbidden || response.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("status %d, Access-Control-Allow-Origin %q", response.StatusCode,
			response.Header.Get("Access-Control-Allow-Origin"))
	}
}

func TestPageCanReadAnUpgradeRequiredAnswer(t *testing.T) {
	h := newWebHarness(t, Config{ClientUpdates: ClientUpdates{Minimum: version(t, allowedMinimum)}})
	response := h.fromPage("GET", "/v1/players/local/guild", map[string]string{
		"Origin": testWebOrigin, "User-Agent": outdatedAgent,
	})
	if response.StatusCode != http.StatusUpgradeRequired {
		t.Fatalf("status %d", response.StatusCode)
	}
	if got := response.Header.Get("Access-Control-Allow-Origin"); got != testWebOrigin {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
}

func TestAdminAndHealthStayClosedToPages(t *testing.T) {
	h := newWebHarness(t, Config{AdminToken: testAdminToken})
	for _, path := range []string{"/healthz", adminOverviewPath} {
		response := h.fromPage("GET", path, map[string]string{
			"Origin": testWebOrigin, "Authorization": "Bearer " + testAdminToken,
		})
		if got := response.Header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("%s: Access-Control-Allow-Origin = %q", path, got)
		}
		preflight := h.fromPage("OPTIONS", path, preflightFrom(testWebOrigin, "GET"))
		if preflight.StatusCode == http.StatusNoContent {
			t.Fatalf("%s: preflight answered", path)
		}
	}
}

func TestTrafficCountsPreflightsApartFromUnknownPaths(t *testing.T) {
	h := newWebHarness(t, Config{AdminToken: testAdminToken})
	h.fromPage("OPTIONS", profileOfLocal, preflightFrom(testWebOrigin, "PUT"))
	h.fromPage("OPTIONS", profileOfLocal, preflightFrom(testWebOrigin, "GET"))

	var reply trafficReply
	h.adminRead(adminTrafficPath, &reply)
	routes := routesByName(reply)
	if routes[preflightRoute].Requests != 2 || routes[unmatchedRoute].Requests != 0 {
		t.Fatalf("routes = %+v", reply.Routes)
	}
}

func TestWebOriginsMustBeBareOrigins(t *testing.T) {
	origins, err := ParseWebOrigins(" https://hodareborn.com, http://localhost:8080 ,")
	if err != nil || len(origins) != 2 || origins[0] != "https://hodareborn.com" || origins[1] != "http://localhost:8080" {
		t.Fatalf("ParseWebOrigins = %q, %v", origins, err)
	}
	if origins, err := ParseWebOrigins(""); err != nil || len(origins) != 0 {
		t.Fatalf("empty list = %q, %v", origins, err)
	}
	for _, bad := range []string{"https://hodareborn.com/", "hodareborn.com", "ftp://hodareborn.com", "https://x/?a"} {
		if _, err := ParseWebOrigins(bad); err == nil {
			t.Fatalf("ParseWebOrigins(%q) accepted", bad)
		}
	}
}
