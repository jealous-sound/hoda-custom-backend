package ratelimit

import (
	"testing"
	"time"
)

type clock struct{ current time.Time }

func (c *clock) now() time.Time { return c.current }

func (c *clock) advance(by time.Duration) { c.current = c.current.Add(by) }

func newClock() *clock {
	return &clock{current: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)}
}

func TestBurstIsAllowedThenRefused(t *testing.T) {
	c := newClock()
	limiter := New(Rule{Burst: 3, Every: 10 * time.Second}, c.now)
	for i := range 3 {
		if ok, _ := limiter.Allow("a"); !ok {
			t.Fatalf("request %d refused inside the burst", i+1)
		}
	}
	ok, retry := limiter.Allow("a")
	if ok {
		t.Fatal("request past the burst was allowed")
	}
	if retry != 10*time.Second {
		t.Fatalf("retry after %v, want 10s", retry)
	}
}

func TestTokensRefillOverTime(t *testing.T) {
	c := newClock()
	limiter := New(Rule{Burst: 2, Every: time.Minute}, c.now)
	limiter.Allow("a")
	limiter.Allow("a")
	c.advance(30 * time.Second)
	if ok, retry := limiter.Allow("a"); ok || retry != 30*time.Second {
		t.Fatalf("half a refill: allowed=%v retry=%v, want refused with 30s left", ok, retry)
	}
	c.advance(30 * time.Second)
	if ok, _ := limiter.Allow("a"); !ok {
		t.Fatal("a full refill period did not restore a token")
	}
	c.advance(time.Hour)
	for i := range 2 {
		if ok, _ := limiter.Allow("a"); !ok {
			t.Fatalf("request %d refused after a long idle", i+1)
		}
	}
	if ok, _ := limiter.Allow("a"); ok {
		t.Fatal("idle time refilled past the burst")
	}
}

func TestKeysHaveSeparateBuckets(t *testing.T) {
	c := newClock()
	limiter := New(Rule{Burst: 1, Every: time.Hour}, c.now)
	if ok, _ := limiter.Allow("a"); !ok {
		t.Fatal("first request for a refused")
	}
	if ok, _ := limiter.Allow("b"); !ok {
		t.Fatal("b was limited by a's request")
	}
	if ok, _ := limiter.Allow("a"); ok {
		t.Fatal("a's second request was allowed")
	}
}

func TestFullBucketsAreForgotten(t *testing.T) {
	c := newClock()
	limiter := New(Rule{Burst: 2, Every: time.Second}, c.now)
	limiter.Allow("idle")
	c.advance(sweepEvery)
	limiter.Allow("busy")
	if got := limiter.tracked(); got != 1 {
		t.Fatalf("tracking %d keys after the sweep, want only the busy one", got)
	}
}

func TestZeroRuleAllowsEverything(t *testing.T) {
	limiter := New(Rule{}, newClock().now)
	for range 100 {
		if ok, _ := limiter.Allow("a"); !ok {
			t.Fatal("a zero rule refused a request")
		}
	}
	var missing *Limiter
	if ok, _ := missing.Allow("a"); !ok {
		t.Fatal("a nil limiter refused a request")
	}
}
