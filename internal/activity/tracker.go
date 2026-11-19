package activity

import (
	"cmp"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

const (
	OnlineWindow    = 5 * time.Minute
	SampleEvery     = time.Minute
	BucketLength    = 5 * time.Minute
	BucketsKept     = 7 * 24 * int(time.Hour/BucketLength)
	HourLength      = time.Hour
	HoursKept       = 31 * 24
	SightingsKept   = 24 * time.Hour
	fileName        = "activity.json"
	unreadableName  = "activity.unreadable.json"
	dayLayout       = "2006-01-02"
	UnknownClient   = ""
	secondsInWindow = int64(OnlineWindow / time.Second)
)

type Presence struct {
	PlayerID string `json:"player_id"`
	Client   string `json:"client"`
	LastSeen int64  `json:"last_seen"`
}

func (p Presence) OnlineAt(now time.Time) bool {
	return now.Unix()-p.LastSeen < secondsInWindow
}

type Bucket struct {
	Start int64 `json:"start"`
	Peak  int   `json:"peak"`
}

type Day struct {
	Date         string `json:"date"`
	PeakOnline   int    `json:"peak_online"`
	PeakOnlineAt int64  `json:"peak_online_at"`
	Players      int    `json:"players"`
	Signups      int    `json:"signups"`
	Samples      int    `json:"samples"`
	OnlineSum    int64  `json:"online_sum"`
}

type Record struct {
	Count int    `json:"count"`
	Date  string `json:"date"`
	At    int64  `json:"at"`
}

type Records struct {
	Since       string `json:"since"`
	PeakOnline  Record `json:"peak_online"`
	MostPlayers Record `json:"most_players"`
	MostSignups Record `json:"most_signups"`
}

type History struct {
	Buckets []Bucket `json:"buckets"`
	Hours   []Bucket `json:"hours"`
	Days    []Day    `json:"days"`
	Records Records  `json:"records"`
}

type stored struct {
	Buckets      []Bucket `json:"buckets"`
	Hours        []Bucket `json:"hours"`
	Days         []Day    `json:"days"`
	TodayPlayers []string `json:"today_players"`
}

type sighting struct {
	client string
	at     time.Time
}

type Tracker struct {
	mutex        sync.Mutex
	path         string
	now          func() time.Time
	seen         map[string]sighting
	buckets      []Bucket
	hours        []Bucket
	days         []Day
	todayPlayers map[string]struct{}
}

func NewTracker(now func() time.Time) *Tracker {
	return &Tracker{now: now, seen: map[string]sighting{}, todayPlayers: map[string]struct{}{}}
}

func Open(dir string, now func() time.Time) (*Tracker, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	tracker := NewTracker(now)
	tracker.path = filepath.Join(dir, fileName)
	data, err := os.ReadFile(tracker.path)
	if errors.Is(err, os.ErrNotExist) {
		return tracker, nil
	}
	if err != nil {
		return nil, err
	}
	var saved stored
	if err := json.Unmarshal(data, &saved); err != nil {
		return tracker, os.Rename(tracker.path, filepath.Join(dir, unreadableName))
	}
	tracker.buckets, tracker.hours, tracker.days = saved.Buckets, saved.Hours, saved.Days
	if len(tracker.hours) == 0 {
		tracker.hours = hoursOf(saved.Buckets)
	}
	timePeaksFromBuckets(tracker.days, saved.Buckets)
	if savedToday := len(saved.Days) > 0 && saved.Days[len(saved.Days)-1].Date == dayOf(now()); savedToday {
		for _, player := range saved.TodayPlayers {
			tracker.todayPlayers[player] = struct{}{}
		}
	}
	return tracker, nil
}

func dayOf(moment time.Time) string {
	return moment.UTC().Format(dayLayout)
}

func startOf(unix int64, length time.Duration) int64 {
	return time.Unix(unix, 0).Truncate(length).Unix()
}

func hoursOf(buckets []Bucket) []Bucket {
	hours := []Bucket{}
	for _, bucket := range buckets {
		hours = keepPeak(hours, startOf(bucket.Start, HourLength), bucket.Peak, HoursKept)
	}
	return hours
}

func timePeaksFromBuckets(days []Day, buckets []Bucket) {
	for index := range days {
		day := &days[index]
		if day.PeakOnlineAt != 0 || day.PeakOnline == 0 {
			continue
		}
		for _, bucket := range buckets {
			if bucket.Peak == day.PeakOnline && dayOf(time.Unix(bucket.Start, 0)) == day.Date {
				day.PeakOnlineAt = bucket.Start
				break
			}
		}
	}
}

func recordsOf(days []Day) Records {
	var records Records
	if len(days) > 0 {
		records.Since = days[0].Date
	}
	beat := func(record *Record, count int, day Day, at int64) {
		if count > record.Count {
			*record = Record{Count: count, Date: day.Date, At: at}
		}
	}
	for _, day := range days {
		beat(&records.PeakOnline, day.PeakOnline, day, day.PeakOnlineAt)
		beat(&records.MostPlayers, day.Players, day, 0)
		beat(&records.MostSignups, day.Signups, day, 0)
	}
	return records
}

func (t *Tracker) Seen(playerID, client string) {
	now := t.now()
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.seen[playerID] = sighting{client: client, at: now}
	today := t.todayLocked(now)
	t.todayPlayers[playerID] = struct{}{}
	today.Players = len(t.todayPlayers)
}

func (t *Tracker) SignedUp() {
	now := t.now()
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.todayLocked(now).Signups++
}

func (t *Tracker) Sightings() []Presence {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	sightings := make([]Presence, 0, len(t.seen))
	for player, last := range t.seen {
		sightings = append(sightings, Presence{PlayerID: player, Client: last.client, LastSeen: last.at.Unix()})
	}
	slices.SortFunc(sightings, func(a, b Presence) int {
		return cmp.Or(cmp.Compare(b.LastSeen, a.LastSeen), cmp.Compare(a.PlayerID, b.PlayerID))
	})
	return sightings
}

func (t *Tracker) Online() []Presence {
	now := t.now()
	return slices.DeleteFunc(t.Sightings(), func(p Presence) bool { return !p.OnlineAt(now) })
}

func (t *Tracker) Today() Day {
	now := t.now()
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return *t.todayLocked(now)
}

func (t *Tracker) History() History {
	now := t.now()
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.todayLocked(now)
	return History{
		Buckets: append([]Bucket{}, t.buckets...), Hours: append([]Bucket{}, t.hours...),
		Days: append([]Day{}, t.days...), Records: recordsOf(t.days),
	}
}

func (t *Tracker) Sample() error {
	now := t.now()
	t.mutex.Lock()
	defer t.mutex.Unlock()
	online := 0
	for player, last := range t.seen {
		switch idle := now.Sub(last.at); {
		case idle < OnlineWindow:
			online++
		case idle >= SightingsKept:
			delete(t.seen, player)
		}
	}
	t.recordLocked(now, online)
	today := t.todayLocked(now)
	if online > today.PeakOnline {
		today.PeakOnline, today.PeakOnlineAt = online, now.Unix()
	}
	today.Samples++
	today.OnlineSum += int64(online)
	return t.saveLocked()
}

func keepPeak(series []Bucket, start int64, online, kept int) []Bucket {
	if last := len(series) - 1; last >= 0 && series[last].Start == start {
		series[last].Peak = max(series[last].Peak, online)
		return series
	}
	series = append(series, Bucket{Start: start, Peak: online})
	if extra := len(series) - kept; extra > 0 {
		series = slices.Clone(series[extra:])
	}
	return series
}

func (t *Tracker) recordLocked(now time.Time, online int) {
	t.buckets = keepPeak(t.buckets, now.Truncate(BucketLength).Unix(), online, BucketsKept)
	t.hours = keepPeak(t.hours, now.Truncate(HourLength).Unix(), online, HoursKept)
}

func (t *Tracker) todayLocked(now time.Time) *Day {
	date := dayOf(now)
	if len(t.days) == 0 || t.days[len(t.days)-1].Date != date {
		t.days = append(t.days, Day{Date: date})
		t.todayPlayers = map[string]struct{}{}
	}
	return &t.days[len(t.days)-1]
}

func (t *Tracker) saveLocked() error {
	if t.path == "" {
		return nil
	}
	players := make([]string, 0, len(t.todayPlayers))
	for player := range t.todayPlayers {
		players = append(players, player)
	}
	slices.Sort(players)
	data, err := json.Marshal(stored{Buckets: t.buckets, Hours: t.hours, Days: t.days, TodayPlayers: players})
	if err != nil {
		return err
	}
	temporary := t.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(temporary, t.path); err != nil {
		os.Remove(temporary)
		return err
	}
	return nil
}
