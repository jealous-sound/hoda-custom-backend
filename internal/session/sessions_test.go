package session

import (
	"testing"
	"time"
)

type clock struct{ current time.Time }

func (c *clock) now() time.Time { return c.current }

func newClock() *clock {
	return &clock{current: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)}
}

func open(t *testing.T, r *Registry, player string) string {
	t.Helper()
	token, err := r.Open(player)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestSessionExpiresAfterIdle(t *testing.T) {
	c := newClock()
	r := NewRegistry(c.now, time.Hour)
	token := open(t, r, "p")
	c.current = c.current.Add(59 * time.Minute)
	if player, ok := r.Player(token); !ok || player != "p" {
		t.Fatalf("session closed before its idle limit: %q %v", player, ok)
	}
	c.current = c.current.Add(59 * time.Minute)
	if _, ok := r.Player(token); !ok {
		t.Fatal("using the session did not extend it")
	}
	c.current = c.current.Add(time.Hour)
	if _, ok := r.Player(token); ok {
		t.Fatal("session survived an hour of idle")
	}
}

func TestExpiredSessionsAreSwept(t *testing.T) {
	c := newClock()
	r := NewRegistry(c.now, time.Hour)
	open(t, r, "a")
	c.current = c.current.Add(2 * time.Hour)
	open(t, r, "b")
	if got := r.count(); got != 1 {
		t.Fatalf("registry holds %d sessions, want only the fresh one", got)
	}
}

func TestOldestSessionIsDroppedPastThePlayerCap(t *testing.T) {
	c := newClock()
	r := NewRegistry(c.now, 0)
	var tokens []string
	for range MaxSessionsPerPlayer {
		tokens = append(tokens, open(t, r, "p"))
		c.current = c.current.Add(time.Second)
	}
	other := open(t, r, "q")
	newest := open(t, r, "p")
	if _, ok := r.Player(tokens[0]); ok {
		t.Fatal("the oldest session survived past the cap")
	}
	for _, token := range append(tokens[1:], newest, other) {
		if _, ok := r.Player(token); !ok {
			t.Fatal("a session inside the cap was dropped")
		}
	}
}

func TestZeroIdleNeverExpires(t *testing.T) {
	c := newClock()
	r := NewRegistry(c.now, 0)
	token := open(t, r, "p")
	c.current = c.current.Add(365 * 24 * time.Hour)
	if _, ok := r.Player(token); !ok {
		t.Fatal("session expired without an idle limit")
	}
}
