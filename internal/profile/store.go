package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var (
	ErrNotFound         = errors.New("profile not found")
	ErrRevisionMismatch = errors.New("profile revision mismatch")
	ErrInvalidPlayerID  = errors.New("invalid player id")
)

var playerIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func ValidPlayerID(id string) bool {
	return playerIDPattern.MatchString(id)
}

type Record struct {
	PlayerID  string          `json:"player_id"`
	Revision  int64           `json:"revision"`
	UpdatedAt time.Time       `json:"updated_at"`
	Profile   json.RawMessage `json:"profile"`
}

type Store struct {
	dir   string
	now   func() time.Time
	mutex sync.Mutex
}

func NewStore(dir string, now func() time.Time) (*Store, error) {
	playersDir := filepath.Join(dir, "players")
	if err := os.MkdirAll(playersDir, 0o755); err != nil {
		return nil, fmt.Errorf("create profile directory: %w", err)
	}
	return &Store{dir: playersDir, now: now}, nil
}

func (s *Store) path(playerID string) string {
	return filepath.Join(s.dir, playerID+".json")
}

func (s *Store) Get(playerID string) (Record, error) {
	if !ValidPlayerID(playerID) {
		return Record{}, ErrInvalidPlayerID
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.read(playerID)
}

func (s *Store) read(playerID string) (Record, error) {
	data, err := os.ReadFile(s.path(playerID))
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("read profile %s: %w", playerID, err)
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, fmt.Errorf("decode profile %s: %w", playerID, err)
	}
	return record, nil
}

func (s *Store) Put(playerID string, expectedRevision int64, profile json.RawMessage) (Record, error) {
	if !ValidPlayerID(playerID) {
		return Record{}, ErrInvalidPlayerID
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	current, err := s.read(playerID)
	switch {
	case errors.Is(err, ErrNotFound):
		current = Record{}
	case err != nil:
		return Record{}, err
	}
	if current.Revision != expectedRevision {
		return current, ErrRevisionMismatch
	}
	next := Record{
		PlayerID:  playerID,
		Revision:  current.Revision + 1,
		UpdatedAt: s.now().UTC(),
		Profile:   profile,
	}
	if err := s.write(next); err != nil {
		return Record{}, err
	}
	return next, nil
}

func (s *Store) Delete(playerID string) error {
	if !ValidPlayerID(playerID) {
		return ErrInvalidPlayerID
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	err := os.Remove(s.path(playerID))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete profile %s: %w", playerID, err)
	}
	return nil
}

func (s *Store) write(record Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode profile %s: %w", record.PlayerID, err)
	}
	target := s.path(record.PlayerID)
	temporary := target + ".tmp"
	if err := os.WriteFile(temporary, data, 0o644); err != nil {
		return fmt.Errorf("write profile %s: %w", record.PlayerID, err)
	}
	if err := os.Rename(temporary, target); err != nil {
		os.Remove(temporary)
		return fmt.Errorf("commit profile %s: %w", record.PlayerID, err)
	}
	return nil
}
