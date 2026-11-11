package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const summarizedProfile = `{"account":{"name":"Hawke","level":19,"gold":9277,"gems":41.0},` +
	`"pvp":{"trophies":114,"best_trophies":120,"wins":7,"losses":3},` +
	`"roster":{"starters":[{"id":"a"}],"additions":[{"id":"b"},{"id":"c"}]},` +
	`"store":{"opened_total":116},"tutorial":{"complete":true}}`

func newSummaryStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(t.TempDir(), func() time.Time { return time.Unix(1_800_000_000, 0) })
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func put(t *testing.T, store *Store, playerID string, revision int64, body string) {
	t.Helper()
	if _, err := store.Put(playerID, revision, json.RawMessage(body)); err != nil {
		t.Fatal(err)
	}
}

func summaries(t *testing.T, store *Store) []Summary {
	t.Helper()
	listed, err := store.Summaries()
	if err != nil {
		t.Fatal(err)
	}
	return listed
}

func TestSummaryReadsTheFieldsAnAdminLooksAt(t *testing.T) {
	store := newSummaryStore(t)
	put(t, store, "hawke", 0, summarizedProfile)
	listed := summaries(t, store)
	want := Summary{
		PlayerID: "hawke", Revision: 1, UpdatedAt: time.Unix(1_800_000_000, 0).UTC(),
		ProfileBytes: len(summarizedProfile), Name: "Hawke", Level: 19, Gold: 9277, Gems: 41, Trophies: 114,
		BestTrophies: 120, PvpWins: 7, PvpLosses: 3, Heroes: 3, PacksOpened: 116, TutorialComplete: true,
	}
	if len(listed) != 1 || listed[0] != want {
		t.Fatalf("summaries = %+v, want %+v", listed, want)
	}
}

func TestSummariesFollowLaterWritesAndDeletes(t *testing.T) {
	store := newSummaryStore(t)
	put(t, store, "a", 0, `{"account":{"level":1},"tutorial":{"complete":false}}`)
	put(t, store, "b", 0, `{"account":{"level":2}}`)
	if listed := summaries(t, store); len(listed) != 2 || listed[0].TutorialComplete || !listed[1].TutorialComplete {
		t.Fatalf("summaries = %+v", listed)
	}
	put(t, store, "a", 1, `{"account":{"level":5,"name":"Aveline"},"tutorial":{"complete":true}}`)
	put(t, store, "c", 0, `{"account":{"level":3}}`)
	if err := store.Delete("b"); err != nil {
		t.Fatal(err)
	}
	listed := summaries(t, store)
	if len(listed) != 2 || listed[0].Name != "Aveline" || listed[0].Level != 5 || listed[0].Revision != 2 ||
		listed[1].PlayerID != "c" {
		t.Fatalf("summaries after changes = %+v", listed)
	}
}

func TestAProfileThatCannotBeReadIsListedAsUnreadable(t *testing.T) {
	store := newSummaryStore(t)
	put(t, store, "fine", 0, `{"account":{"level":1}}`)
	put(t, store, "odd", 0, `{"account":"not an object"}`)
	if err := os.WriteFile(filepath.Join(store.dir, "torn.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	listed := summaries(t, store)
	if len(listed) != 3 || listed[0].Unreadable || !listed[1].Unreadable || !listed[2].Unreadable {
		t.Fatalf("summaries = %+v, want odd and torn marked unreadable", listed)
	}
}
