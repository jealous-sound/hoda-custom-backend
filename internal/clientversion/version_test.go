package clientversion

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mustParse(t *testing.T, text string) Version {
	t.Helper()
	version, ok := Parse(text)
	if !ok {
		t.Fatalf("Parse(%q) failed", text)
	}
	return version
}

func TestParseAcceptsTagsAndDescribeOutput(t *testing.T) {
	for _, text := range []string{
		"v0.4.0", "0.4.0", "v1.12.3", "v0.5.0-rc.1", "v0.4.0-3-gabc1234", "v0.5.0-rc.1-2-g0f3a9c1",
		"v0.4.0-dirty", "v0.4.0-3-gabc1234-dirty",
	} {
		if _, ok := Parse(text); !ok {
			t.Errorf("Parse(%q) failed", text)
		}
	}
}

func TestParseRejectsBuildsWithoutAVersionTag(t *testing.T) {
	for _, text := range []string{"", "dev", "abc1234", "abc1234-dirty", "v0.4", "v0.4.0.1", "version"} {
		if _, ok := Parse(text); ok {
			t.Errorf("Parse(%q) succeeded", text)
		}
	}
}

func TestCompareOrdersReleasesCandidatesAndLaterCommits(t *testing.T) {
	ordered := []string{
		"v0.3.9",
		"v0.4.0-rc.1",
		"v0.4.0-rc.1-4-gabc1234",
		"v0.4.0-rc.2",
		"v0.4.0-rc.10",
		"v0.4.0",
		"v0.4.0-1-gabc1234",
		"v0.4.0-12-gabc1234",
		"v0.4.1",
		"v0.10.0",
		"v1.0.0",
	}
	for i := 0; i+1 < len(ordered); i++ {
		lower, higher := mustParse(t, ordered[i]), mustParse(t, ordered[i+1])
		if !lower.Older(higher) || higher.Older(lower) {
			t.Errorf("%s should be older than %s", ordered[i], ordered[i+1])
		}
	}
}

func TestDirtyBuildCountsAsItsCommit(t *testing.T) {
	if order := mustParse(t, "v0.4.0-dirty").Compare(mustParse(t, "v0.4.0")); order != 0 {
		t.Fatalf("dirty v0.4.0 compares %d to v0.4.0", order)
	}
}

func TestFromUserAgentReadsTheClientProduct(t *testing.T) {
	version, ok := FromUserAgent("HeroesOfDragonAge/v0.4.0-2-gabc1234")
	if !ok || version.String() != "v0.4.0-2-gabc1234" {
		t.Fatalf("got %q, %v", version, ok)
	}
	if _, ok := FromUserAgent("HeroesOfDragonAge/dev"); ok {
		t.Fatal("dev build parsed as a version")
	}
	if _, ok := FromUserAgent("curl/8.7.1"); ok {
		t.Fatal("curl parsed as the client")
	}
}

func TestNewestPrefersTheKnownHigherVersion(t *testing.T) {
	low, high := mustParse(t, "v0.4.0"), mustParse(t, "v0.5.0")
	if got := Newest(low, high).String(); got != "v0.5.0" {
		t.Fatalf("Newest = %s", got)
	}
	if got := Newest(Version{}, low).String(); got != "v0.4.0" {
		t.Fatalf("Newest with unknown = %s", got)
	}
	if got := Newest(high, Version{}).String(); got != "v0.5.0" {
		t.Fatalf("Newest with unknown second = %s", got)
	}
}

func TestReleaseFeedKeepsTheLastTagAcrossUnchangedAndFailedFetches(t *testing.T) {
	status := http.StatusOK
	var conditional string
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conditional = r.Header.Get("If-None-Match")
		if r.Header.Get("User-Agent") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("ETag", `"r1"`)
		w.Write([]byte(`{"tag_name":"v0.5.0","name":"Heroes of Dragon Age v0.5.0"}`))
	}))
	defer feed.Close()
	releases := NewReleaseFeed(feed.URL, feed.Client())

	if releases.Latest().Known() {
		t.Fatal("latest known before the first fetch")
	}
	if err := releases.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := releases.Latest().String(); got != "v0.5.0" {
		t.Fatalf("latest = %q", got)
	}

	status = http.StatusNotModified
	if err := releases.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if conditional != `"r1"` {
		t.Fatalf("If-None-Match = %q", conditional)
	}

	status = http.StatusInternalServerError
	if err := releases.Refresh(context.Background()); err == nil {
		t.Fatal("failed fetch reported no error")
	}
	if got := releases.Latest().String(); got != "v0.5.0" {
		t.Fatalf("latest after failures = %q", got)
	}
}
