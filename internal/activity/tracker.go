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
	DaysKept        = 90
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
	Date       string `json:"date"`
	PeakOnline int    `json:"peak_online"`
	Players    int    `json:"players"`
	Signups    int    `json:"signups"`
}

type History struct {
	Buckets []Bucket `json:"buckets"`
	Days    []Day    `json:"days"`
}

type stored struct {
	Buckets      []Bucket `json:"buckets"`
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
	tracker.buckets, tracker.days = saved.Buckets, saved.Days
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
	return History{Buckets: append([]Bucket{}, t.buckets...), Days: append([]Day{}, t.days...)}
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
	today.PeakOnline = max(today.PeakOnline, online)
	return t.saveLocked()
}

func (t *Tracker) recordLocked(now time.Time, online int) {
	start := now.Truncate(BucketLength).Unix()
	if last := len(t.buckets) - 1; last >= 0 && t.buckets[last].Start == start {
		t.buckets[last].Peak = max(t.buckets[last].Peak, online)
		return
	}
	t.buckets = append(t.buckets, Bucket{Start: start, Peak: online})
	if extra := len(t.buckets) - BucketsKept; extra > 0 {
		t.buckets = slices.Clone(t.buckets[extra:])
	}
}

func (t *Tracker) todayLocked(now time.Time) *Day {
	date := dayOf(now)
	if len(t.days) == 0 || t.days[len(t.days)-1].Date != date {
		t.days = append(t.days, Day{Date: date})
		t.todayPlayers = map[string]struct{}{}
		if extra := len(t.days) - DaysKept; extra > 0 {
			t.days = slices.Clone(t.days[extra:])
		}
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
	data, err := json.Marshal(stored{Buckets: t.buckets, Days: t.days, TodayPlayers: players})
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
