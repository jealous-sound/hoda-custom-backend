package maintenance

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	fileName   = "maintenance.json"
	BattleLead = 3 * time.Minute
	Overrun    = 10 * time.Minute
)

var ErrNotAhead = errors.New("maintenance must be scheduled for a time that is still ahead")

type stored struct {
	At int64 `json:"at"`
}

type Schedule struct {
	mutex sync.Mutex
	path  string
	now   func() time.Time
	at    time.Time
}

func Open(dir string, now func() time.Time) (*Schedule, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	schedule := &Schedule{path: filepath.Join(dir, fileName), now: now}
	data, err := os.ReadFile(schedule.path)
	if errors.Is(err, os.ErrNotExist) {
		return schedule, nil
	}
	if err != nil {
		return nil, err
	}
	var saved stored
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, err
	}
	announced := time.Unix(saved.At, 0)
	if restartAlreadyHappened := !announced.After(now()); restartAlreadyHappened {
		return schedule, os.Remove(schedule.path)
	}
	schedule.at = announced
	return schedule, nil
}

func (s *Schedule) At() (time.Time, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.currentLocked()
}

func (s *Schedule) HoldsBattles() bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	at, scheduled := s.currentLocked()
	return scheduled && !s.now().Before(at.Add(-BattleLead))
}

func (s *Schedule) Set(at time.Time) error {
	at = at.Truncate(time.Second)
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if !at.After(s.now()) {
		return ErrNotAhead
	}
	data, err := json.Marshal(stored{At: at.Unix()})
	if err != nil {
		return err
	}
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(temporary, s.path); err != nil {
		os.Remove(temporary)
		return err
	}
	s.at = at
	return nil
}

func (s *Schedule) Clear() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.forgetLocked()
}

func (s *Schedule) currentLocked() (time.Time, bool) {
	if s.at.IsZero() {
		return time.Time{}, false
	}
	if overrun := !s.now().Before(s.at.Add(Overrun)); overrun {
		s.forgetLocked()
		return time.Time{}, false
	}
	return s.at, true
}

func (s *Schedule) forgetLocked() error {
	s.at = time.Time{}
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
