package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
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
	Server    ServerState     `json:"server"`
}

type ServerState struct {
	StoreRNG        uint32           `json:"store_rng,omitempty"`
	ScriptedPacks   []string         `json:"scripted_packs,omitempty"`
	RecentActions   []ActionReceipt  `json:"recent_actions,omitempty"`
	RegenAt         int64            `json:"regen_at,omitempty"`
	TutorialRewards []string         `json:"tutorial_rewards,omitempty"`
	PvpEvent        PvpEventState    `json:"pvp_event"`
	PvpResults      []PvpEventResult `json:"pvp_results,omitempty"`
	MissionProgress map[string]int64 `json:"mission_progress,omitempty"`
	BattleDay       int64            `json:"battle_day,omitempty"`
	Battles         int64            `json:"battles,omitempty"`
	Battle          *PendingBattle   `json:"battle,omitempty"`
}

type PendingBattle struct {
	ID         int64         `json:"id"`
	Mode       string        `json:"mode"`
	Map        int           `json:"map,omitempty"`
	Node       int           `json:"node,omitempty"`
	Event      int           `json:"event,omitempty"`
	Difficulty int           `json:"difficulty,omitempty"`
	Step       int           `json:"step,omitempty"`
	PvpEvent   bool          `json:"pvp_event,omitempty"`
	Opponent   PendingRival  `json:"opponent,omitempty"`
	BannerPct  int           `json:"banner_pct,omitempty"`
	StartedAt  int64         `json:"started_at"`
	Replay     battle.Replay `json:"replay"`
}

type PendingRival struct {
	Bot      int    `json:"bot,omitempty"`
	Squad    int    `json:"squad,omitempty"`
	Level    int    `json:"level,omitempty"`
	Trophies int    `json:"trophies,omitempty"`
	Seed     uint32 `json:"seed,omitempty"`
	Name     string `json:"name,omitempty"`
}

type PvpEventState struct {
	Cycle          int64 `json:"cycle"`
	Banners        int   `json:"banners"`
	MilestonesPaid int   `json:"milestones_paid"`
	Buffed         bool  `json:"buffed,omitempty"`
}

type PvpEventResult struct {
	Cycle   int64 `json:"cycle"`
	Banners int   `json:"banners"`
	Claimed bool  `json:"claimed"`
}

func (s ServerState) Clone() ServerState {
	s.ScriptedPacks = append([]string(nil), s.ScriptedPacks...)
	s.RecentActions = append([]ActionReceipt(nil), s.RecentActions...)
	s.TutorialRewards = append([]string(nil), s.TutorialRewards...)
	s.PvpResults = append([]PvpEventResult(nil), s.PvpResults...)
	s.MissionProgress = maps.Clone(s.MissionProgress)
	if s.Battle != nil {
		pending := *s.Battle
		s.Battle = &pending
	}
	return s
}

type ActionReceipt struct {
	RequestID string          `json:"request_id"`
	Status    int             `json:"status"`
	Response  json.RawMessage `json:"response"`
}

const recentActionLimit = 16

func (s *ServerState) Receipt(requestID string) (ActionReceipt, bool) {
	for _, receipt := range s.RecentActions {
		if receipt.RequestID == requestID {
			return receipt, true
		}
	}
	return ActionReceipt{}, false
}

func (s *ServerState) Remember(receipt ActionReceipt) {
	s.RecentActions = append(s.RecentActions, receipt)
	if extra := len(s.RecentActions) - recentActionLimit; extra > 0 {
		s.RecentActions = append([]ActionReceipt(nil), s.RecentActions[extra:]...)
	}
}

var ErrNoChange = errors.New("transaction left the record unchanged")

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
	return s.PutMerged(playerID, expectedRevision, func(_ Record, next *Record) error {
		next.Profile = profile
		return nil
	})
}

func (s *Store) PutMerged(
	playerID string, expectedRevision int64, merge func(current Record, next *Record) error,
) (Record, error) {
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
		Server:    current.Server.Clone(),
	}
	if err := merge(current, &next); err != nil {
		return current, err
	}
	if err := s.write(next); err != nil {
		return Record{}, err
	}
	return next, nil
}

func (s *Store) Transact(playerID string, change func(record *Record) error) (Record, error) {
	if !ValidPlayerID(playerID) {
		return Record{}, ErrInvalidPlayerID
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	current, err := s.read(playerID)
	if err != nil {
		return Record{}, err
	}
	next := current
	next.Server = current.Server.Clone()
	if err := change(&next); err != nil {
		return current, err
	}
	next.PlayerID = playerID
	next.Revision = current.Revision + 1
	next.UpdatedAt = s.now().UTC()
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
