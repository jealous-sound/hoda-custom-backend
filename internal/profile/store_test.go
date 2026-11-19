package profile

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAnAmendmentKeepsTheRevisionTheOwnerWritesAgainst(t *testing.T) {
	clock := time.Unix(1_800_000_000, 0)
	store, err := NewStore(t.TempDir(), func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	put(t, store, "hawke", 0, summarizedProfile)
	summaries(t, store)
	clock = clock.Add(time.Hour)
	amended, err := store.AmendKeepingRevision("hawke", func(record *Record) error {
		record.Profile = json.RawMessage(strings.Replace(summarizedProfile, `"trophies":114`, `"trophies":90`, 1))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if amended.Revision != 1 || !amended.UpdatedAt.Equal(time.Unix(1_800_000_000, 0)) {
		t.Errorf("amended record = revision %d updated %v", amended.Revision, amended.UpdatedAt)
	}
	if listed := summaries(t, store); len(listed) != 1 || listed[0].Trophies != 90 {
		t.Errorf("summaries after the amendment = %+v", listed)
	}
	if _, err := store.Put("hawke", 1, json.RawMessage(summarizedProfile)); err != nil {
		t.Errorf("the owner's next write at its last seen revision: %v", err)
	}
}
