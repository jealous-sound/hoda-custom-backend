package activity

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type clock struct{ current time.Time }

func (c *clock) now() time.Time { return c.current }

func (c *clock) advance(by time.Duration) { c.current = c.current.Add(by) }

func newClock() *clock {
	return &clock{current: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)}
}

func open(t *testing.T, dir string, c *clock) *Tracker {
	t.Helper()
	tracker, err := Open(dir, c.now)
	if err != nil {
		t.Fatal(err)
	}
	return tracker
}

func sample(t *testing.T, tracker *Tracker) {
	t.Helper()
	if err := tracker.Sample(); err != nil {
		t.Fatal(err)
	}
}

func onlineIDs(tracker *Tracker) []string {
	ids := []string{}
	for _, presence := range tracker.Online() {
		ids = append(ids, presence.PlayerID)
	}
	return ids
}

func TestPlayerIsOnlineUntilTheWindowPasses(t *testing.T) {
	c := newClock()
	tracker := NewTracker(c.now)
	tracker.Seen("a", "v1.2.3")
	c.advance(OnlineWindow - time.Second)
	tracker.Seen("b", UnknownClient)
	if got := onlineIDs(tracker); len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Fatalf("online = %v, want b then a", got)
	}
	c.advance(time.Second)
	if got := onlineIDs(tracker); len(got) != 1 || got[0] != "b" {
		t.Fatalf("online = %v, want only b once a's window passed", got)
	}
	if sightings := tracker.Sightings(); len(sightings) != 2 || sightings[1].Client != "v1.2.3" {
		t.Fatalf("sightings = %+v, want a remembered with its client", sightings)
	}
}

func TestSamplesKeepThePeakOfEachBucket(t *testing.T) {
	c := newClock()
	tracker := NewTracker(c.now)
	tracker.Seen("a", UnknownClient)
	tracker.Seen("b", UnknownClient)
	sample(t, tracker)
	c.advance(SampleEvery)
	tracker.Seen("c", UnknownClient)
	sample(t, tracker)
	c.advance(BucketLength)
	tracker.Seen("c", UnknownClient)
	sample(t, tracker)
	history := tracker.History()
	if len(history.Buckets) != 2 || history.Buckets[0].Peak != 3 || history.Buckets[1].Peak != 1 {
		t.Fatalf("buckets = %+v, want a peak of 3 then 1", history.Buckets)
	}
	if history.Buckets[1].Start-history.Buckets[0].Start != int64(BucketLength/time.Second) {
		t.Fatalf("buckets = %+v, want them one bucket apart", history.Buckets)
	}
}

func TestDayCountsEachPlayerOnceAndStartsOverAtMidnight(t *testing.T) {
	c := newClock()
	tracker := NewTracker(c.now)
	tracker.Seen("a", UnknownClient)
	tracker.Seen("a", UnknownClient)
	tracker.Seen("b", UnknownClient)
	tracker.SignedUp()
	sample(t, tracker)
	if today := tracker.Today(); today.Players != 2 || today.Signups != 1 || today.PeakOnline != 2 {
		t.Fatalf("today = %+v", today)
	}
	c.advance(24 * time.Hour)
	tracker.Seen("a", UnknownClient)
	days := tracker.History().Days
	if len(days) != 2 || days[0].Players != 2 || days[1].Players != 1 || days[1].Signups != 0 {
		t.Fatalf("days = %+v, want yesterday kept and today started over", days)
	}
	if days[0].Date != "2026-11-07" || days[1].Date != "2026-11-08" {
		t.Fatalf("days = %+v, want UTC dates", days)
	}
}

func TestHistoryIsBounded(t *testing.T) {
	c := newClock()
	tracker := NewTracker(c.now)
	for range BucketsKept + 5 {
		sample(t, tracker)
		c.advance(BucketLength)
	}
	if got := len(tracker.History().Buckets); got != BucketsKept {
		t.Fatalf("kept %d buckets, want %d", got, BucketsKept)
	}
	for range DaysKept + 5 {
		tracker.SignedUp()
		c.advance(24 * time.Hour)
	}
	if got := len(tracker.History().Days); got != DaysKept {
		t.Fatalf("kept %d days, want %d", got, DaysKept)
	}
}

func TestOldSightingsAreForgotten(t *testing.T) {
	c := newClock()
	tracker := NewTracker(c.now)
	tracker.Seen("a", UnknownClient)
	c.advance(SightingsKept)
	tracker.Seen("b", UnknownClient)
	sample(t, tracker)
	if sightings := tracker.Sightings(); len(sightings) != 1 || sightings[0].PlayerID != "b" {
		t.Fatalf("sightings = %+v, want only b", sightings)
	}
}

func TestHistorySurvivesARestartWithTodaysPlayers(t *testing.T) {
	c := newClock()
	dir := t.TempDir()
	tracker := open(t, dir, c)
	tracker.Seen("a", UnknownClient)
	tracker.Seen("b", UnknownClient)
	tracker.SignedUp()
	sample(t, tracker)

	c.advance(time.Hour)
	restarted := open(t, dir, c)
	restarted.Seen("a", UnknownClient)
	restarted.Seen("c", UnknownClient)
	if today := restarted.Today(); today.Players != 3 || today.Signups != 1 || today.PeakOnline != 2 {
		t.Fatalf("today after a restart = %+v", today)
	}
	if got := len(restarted.Online()); got != 2 {
		t.Fatalf("%d players online after a restart, want only those seen since", got)
	}
	if buckets := restarted.History().Buckets; len(buckets) != 1 || buckets[0].Peak != 2 {
		t.Fatalf("buckets after a restart = %+v", buckets)
	}
}

func TestARestartOnALaterDayDoesNotCarryPlayersOver(t *testing.T) {
	c := newClock()
	dir := t.TempDir()
	tracker := open(t, dir, c)
	tracker.Seen("a", UnknownClient)
	sample(t, tracker)
	c.advance(24 * time.Hour)
	restarted := open(t, dir, c)
	restarted.Seen("b", UnknownClient)
	if today := restarted.Today(); today.Players != 1 {
		t.Fatalf("today = %+v, want only the player seen today", today)
	}
}

func TestUnreadableHistoryIsSetAsideInsteadOfStoppingTheServer(t *testing.T) {
	c := newClock()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	tracker := open(t, dir, c)
	tracker.Seen("a", UnknownClient)
	sample(t, tracker)
	if _, err := os.Stat(filepath.Join(dir, unreadableName)); err != nil {
		t.Fatalf("the unreadable file was not kept: %v", err)
	}
	if today := open(t, dir, c).Today(); today.Players != 1 {
		t.Fatalf("today = %+v, want the history started afresh", today)
	}
}
