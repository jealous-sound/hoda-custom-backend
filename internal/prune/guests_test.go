package prune

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/danbestan/hoda-custom-backend/internal/account"
	"github.com/danbestan/hoda-custom-backend/internal/profile"
	"github.com/danbestan/hoda-custom-backend/internal/session"
)

const maxIdle = 14 * 24 * time.Hour

var created = time.Unix(1_800_000_000, 0).UTC()

const (
	unnamedMidTutorial = `{"account":{"level":1},"tutorial":{"complete":false,"consume_complete":false,"restore_step":3},` +
		`"roster":{},"squads":[]}`
	namedMidTutorial    = `{"account":{"level":1,"name":"Hawke"},"tutorial":{"complete":false},"roster":{},"squads":[]}`
	unnamedTutorialDone = `{"account":{"level":2},"tutorial":{"complete":true},"roster":{},"squads":[]}`
	savedBeforeTutorial = `{"account":{"level":9},"roster":{},"squads":[]}`
)

type world struct {
	t        *testing.T
	clock    time.Time
	accounts *account.Store
	profiles *profile.Store
	sessions *session.Registry
	pruner   *Pruner
}

func newWorld(t *testing.T) *world {
	w := &world{t: t, clock: created, sessions: session.NewRegistry()}
	now := func() time.Time { return w.clock }
	dir := t.TempDir()
	var err error
	if w.accounts, err = account.NewStore(dir, now); err != nil {
		t.Fatal(err)
	}
	if w.profiles, err = profile.NewStore(dir, now); err != nil {
		t.Fatal(err)
	}
	w.pruner = NewPruner(w.accounts, w.profiles, w.sessions, now, maxIdle)
	return w
}

func (w *world) guest(savedProfile string) string {
	w.t.Helper()
	credentials, err := w.accounts.CreateGuest()
	if err != nil {
		w.t.Fatal(err)
	}
	if savedProfile != "" {
		if _, err := w.profiles.Put(credentials.PlayerID, 0, json.RawMessage(savedProfile)); err != nil {
			w.t.Fatal(err)
		}
	}
	return credentials.PlayerID
}

func (w *world) prune() []string {
	w.t.Helper()
	pruned, err := w.pruner.AbandonedGuests()
	if err != nil {
		w.t.Fatal(err)
	}
	return pruned
}

func (w *world) expectKept(playerID string, withProfile bool) {
	w.t.Helper()
	if _, err := w.accounts.Verify(playerID, ""); errors.Is(err, account.ErrNotRegistered) {
		w.t.Fatalf("%s account was deleted", playerID)
	}
	_, err := w.profiles.Get(playerID)
	if withProfile && err != nil {
		w.t.Fatalf("%s profile: %v", playerID, err)
	}
}

func (w *world) expectPruned(pruned []string, playerID string) {
	w.t.Helper()
	if !slices.Contains(pruned, playerID) {
		w.t.Fatalf("%s not in pruned %v", playerID, pruned)
	}
	if _, err := w.accounts.Verify(playerID, ""); !errors.Is(err, account.ErrNotRegistered) {
		w.t.Fatalf("%s account still exists: %v", playerID, err)
	}
	if _, err := w.profiles.Get(playerID); !errors.Is(err, profile.ErrNotFound) {
		w.t.Fatalf("%s profile still exists: %v", playerID, err)
	}
}

func TestNamedPlayerIsKept(t *testing.T) {
	w := newWorld(t)
	player := w.guest(namedMidTutorial)
	w.clock = created.Add(10 * maxIdle)
	if pruned := w.prune(); len(pruned) != 0 {
		t.Fatalf("pruned %v", pruned)
	}
	w.expectKept(player, true)
}

func TestTutorialCompletePlayerIsKept(t *testing.T) {
	w := newWorld(t)
	finished := w.guest(unnamedTutorialDone)
	legacy := w.guest(savedBeforeTutorial)
	w.clock = created.Add(10 * maxIdle)
	if pruned := w.prune(); len(pruned) != 0 {
		t.Fatalf("pruned %v", pruned)
	}
	w.expectKept(finished, true)
	w.expectKept(legacy, true)
}

func TestStaleUnnamedGuestIsPruned(t *testing.T) {
	w := newWorld(t)
	player := w.guest(unnamedMidTutorial)
	token, err := w.sessions.Open(player)
	if err != nil {
		t.Fatal(err)
	}
	w.clock = created.Add(maxIdle + time.Second)
	w.expectPruned(w.prune(), player)
	if _, open := w.sessions.Player(token); open {
		t.Fatal("pruned guest kept its session")
	}
}

func TestRecentUnnamedGuestIsKept(t *testing.T) {
	w := newWorld(t)
	fresh := w.guest(unnamedMidTutorial)
	returning := w.guest(unnamedMidTutorial)
	w.clock = created.Add(maxIdle - time.Hour)
	if err := w.accounts.RecordSession(returning); err != nil {
		t.Fatal(err)
	}
	w.clock = created.Add(maxIdle + time.Hour)
	pruned := w.prune()
	w.expectPruned(pruned, fresh)
	if slices.Contains(pruned, returning) {
		t.Fatalf("pruned %s who opened a session within the window", returning)
	}
	w.expectKept(returning, true)
}

func TestProfilelessStaleGuestIsPruned(t *testing.T) {
	w := newWorld(t)
	stale := w.guest("")
	w.clock = created.Add(maxIdle / 2)
	recent := w.guest("")
	w.clock = created.Add(maxIdle + time.Second)
	pruned := w.prune()
	w.expectPruned(pruned, stale)
	w.expectKept(recent, false)
	if len(pruned) != 1 {
		t.Fatalf("pruned %v", pruned)
	}
}
