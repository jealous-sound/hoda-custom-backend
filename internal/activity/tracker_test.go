package activity

import (
	"fmt"
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

func TestPeaksAreBoundedButDaysAreKeptForGood(t *testing.T) {
	c := newClock()
	tracker := NewTracker(c.now)
	for range BucketsKept + 5 {
		sample(t, tracker)
		c.advance(BucketLength)
	}
	if got := len(tracker.History().Buckets); got != BucketsKept {
		t.Fatalf("kept %d buckets, want %d", got, BucketsKept)
	}
	for range HoursKept + 5 {
		sample(t, tracker)
		c.advance(HourLength)
	}
	if got := len(tracker.History().Hours); got != HoursKept {
		t.Fatalf("kept %d hours, want %d", got, HoursKept)
	}
	const twoYears = 2 * 366
	days := len(tracker.History().Days)
	for range twoYears {
		tracker.SignedUp()
		c.advance(24 * time.Hour)
	}
	if got := len(tracker.History().Days); got != days+twoYears {
		t.Fatalf("kept %d days, want all %d", got, days+twoYears)
	}
}

func TestEachHourKeepsItsHighestSample(t *testing.T) {
	c := newClock()
	tracker := NewTracker(c.now)
	tracker.Seen("a", UnknownClient)
	sample(t, tracker)
	c.advance(20 * time.Minute)
	tracker.Seen("a", UnknownClient)
	tracker.Seen("b", UnknownClient)
	tracker.Seen("c", UnknownClient)
	sample(t, tracker)
	c.advance(39 * time.Minute)
	sample(t, tracker)
	c.advance(time.Minute)
	sample(t, tracker)
	hours := tracker.History().Hours
	if len(hours) != 2 || hours[0].Peak != 3 || hours[1].Peak != 0 {
		t.Fatalf("hours = %+v, want a peak of 3 then 0", hours)
	}
	if hours[0].Start != c.current.Add(-time.Hour).Unix() || hours[1].Start != c.current.Unix() {
		t.Fatalf("hours = %+v, want them to start on the hour", hours)
	}
}

func TestADayRemembersWhenItsPeakWasFirstReachedAndItsAverage(t *testing.T) {
	c := newClock()
	tracker := NewTracker(c.now)
	tracker.Seen("a", UnknownClient)
	sample(t, tracker)
	c.advance(SampleEvery)
	tracker.Seen("b", UnknownClient)
	sample(t, tracker)
	peakedAt := c.current.Unix()
	c.advance(SampleEvery)
	sample(t, tracker)
	today := tracker.Today()
	if today.PeakOnline != 2 || today.PeakOnlineAt != peakedAt {
		t.Fatalf("today = %+v, want a peak of 2 first reached at %d", today, peakedAt)
	}
	if today.Samples != 3 || today.OnlineSum != 1+2+2 {
		t.Fatalf("today = %+v, want 3 samples adding up to 5", today)
	}
}

func TestRecordsHoldTheBestDayOfEachKind(t *testing.T) {
	c := newClock()
	tracker := NewTracker(c.now)
	tracker.Seen("a", UnknownClient)
	tracker.Seen("b", UnknownClient)
	sample(t, tracker)
	peakedAt := c.current.Unix()
	c.advance(24 * time.Hour)
	tracker.Seen("a", UnknownClient)
	tracker.SignedUp()
	tracker.SignedUp()
	sample(t, tracker)
	c.advance(time.Hour)
	tracker.Seen("b", UnknownClient)
	tracker.Seen("c", UnknownClient)
	c.advance(24 * time.Hour)
	tracker.SignedUp()
	records := tracker.History().Records
	want := Records{
		Since:       "2026-11-07",
		PeakOnline:  Record{Count: 2, Date: "2026-11-07", At: peakedAt},
		MostPlayers: Record{Count: 3, Date: "2026-11-08"},
		MostSignups: Record{Count: 2, Date: "2026-11-08"},
	}
	if records != want {
		t.Fatalf("records = %+v, want %+v", records, want)
	}
}

func TestHistoryFromBeforeHourlyPeaksGainsThemFromItsBuckets(t *testing.T) {
	c := newClock()
	dir := t.TempDir()
	start := c.current.Unix()
	earlier := `{"buckets":[{"start":%d,"peak":1},{"start":%d,"peak":4},{"start":%d,"peak":2}],` +
		`"days":[{"date":"2026-11-06","peak_online":7,"players":9,"signups":1},` +
		`{"date":"2026-11-07","peak_online":4,"players":5,"signups":0}],"today_players":[]}`
	saved := fmt.Sprintf(earlier, start, start+int64(BucketLength/time.Second), start+int64(time.Hour/time.Second))
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(saved), 0o644); err != nil {
		t.Fatal(err)
	}
	history := open(t, dir, c).History()
	if len(history.Hours) != 2 || history.Hours[0] != (Bucket{Start: start, Peak: 4}) ||
		history.Hours[1].Peak != 2 {
		t.Fatalf("hours = %+v, want the bucket peaks rolled up by hour", history.Hours)
	}
	firstFour := start + int64(BucketLength/time.Second)
	if history.Days[1].PeakOnlineAt != firstFour || history.Days[0].PeakOnlineAt != 0 {
		t.Fatalf("days = %+v, want only the day its buckets cover to learn its peak time", history.Days)
	}
	if record := history.Records.PeakOnline; record != (Record{Count: 7, Date: "2026-11-06"}) {
		t.Fatalf("peak record = %+v, want the older day without a time", record)
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
