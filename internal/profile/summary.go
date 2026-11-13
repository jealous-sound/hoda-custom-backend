package profile

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
)

const recordSuffix = ".json"

type Summary struct {
	PlayerID         string
	Revision         int64
	UpdatedAt        time.Time
	ProfileBytes     int
	Name             string
	Level            int
	Gold             int64
	Gems             int64
	Trophies         int
	BestTrophies     int
	PvpWins          int
	PvpLosses        int
	Heroes           int
	PacksOpened      int
	TutorialComplete bool
	Unreadable       bool
	Event            PvpEventState
	EventResults     []PvpEventResult
}

func (s Summary) EventBanners(cycle int64) int {
	if s.Event.Cycle == cycle {
		return s.Event.Banners
	}
	for _, finished := range s.EventResults {
		if finished.Cycle == cycle {
			return finished.Banners
		}
	}
	return 0
}

type summarizedSections struct {
	Account struct {
		Name  string  `json:"name"`
		Level float64 `json:"level"`
		Gold  float64 `json:"gold"`
		Gems  float64 `json:"gems"`
	} `json:"account"`
	Pvp struct {
		Trophies     float64 `json:"trophies"`
		BestTrophies float64 `json:"best_trophies"`
		Wins         float64 `json:"wins"`
		Losses       float64 `json:"losses"`
	} `json:"pvp"`
	Roster struct {
		Starters  []json.RawMessage `json:"starters"`
		Additions []json.RawMessage `json:"additions"`
	} `json:"roster"`
	Store struct {
		OpenedTotal float64 `json:"opened_total"`
	} `json:"store"`
	Tutorial *struct {
		Complete bool `json:"complete"`
	} `json:"tutorial"`
}

func Summarize(record Record) Summary {
	summary := Summary{
		PlayerID: record.PlayerID, Revision: record.Revision, UpdatedAt: record.UpdatedAt,
		ProfileBytes: len(record.Profile),
	}
	var sections summarizedSections
	if err := json.Unmarshal(record.Profile, &sections); err != nil {
		summary.Unreadable = true
		return summary
	}
	summary.Name = sections.Account.Name
	summary.Level = int(sections.Account.Level)
	summary.Gold = int64(sections.Account.Gold)
	summary.Gems = int64(sections.Account.Gems)
	summary.Trophies = int(sections.Pvp.Trophies)
	summary.BestTrophies = int(sections.Pvp.BestTrophies)
	summary.PvpWins = int(sections.Pvp.Wins)
	summary.PvpLosses = int(sections.Pvp.Losses)
	summary.Heroes = len(sections.Roster.Starters) + len(sections.Roster.Additions)
	summary.PacksOpened = int(sections.Store.OpenedTotal)
	savedBeforeTutorialExisted := sections.Tutorial == nil
	summary.TutorialComplete = savedBeforeTutorialExisted || sections.Tutorial.Complete
	summary.Event = record.Server.PvpEvent
	summary.EventResults = slices.Clone(record.Server.PvpResults)
	return summary
}

func (s *Store) Summaries() ([]Summary, error) {
	changed, err := s.takeChanged()
	if err != nil {
		return nil, err
	}
	for _, playerID := range changed {
		record, err := s.Get(playerID)
		s.mutex.Lock()
		switch {
		case errors.Is(err, ErrNotFound):
			delete(s.summaries, playerID)
		case err != nil:
			s.summaries[playerID] = Summary{PlayerID: playerID, Unreadable: true}
		default:
			s.summaries[playerID] = Summarize(record)
		}
		s.mutex.Unlock()
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	summaries := make([]Summary, 0, len(s.summaries))
	for _, summary := range s.summaries {
		summaries = append(summaries, summary)
	}
	slices.SortFunc(summaries, func(a, b Summary) int { return cmp.Compare(a.PlayerID, b.PlayerID) })
	return summaries, nil
}

func (s *Store) takeChanged() ([]string, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.summaries == nil {
		stored, err := s.storedPlayersLocked()
		if err != nil {
			return nil, err
		}
		s.summaries = map[string]Summary{}
		return stored, nil
	}
	changed := make([]string, 0, len(s.changed))
	for playerID := range s.changed {
		changed = append(changed, playerID)
	}
	clear(s.changed)
	return changed, nil
}

func (s *Store) storedPlayersLocked() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	var players []string
	for _, entry := range entries {
		playerID, isRecord := strings.CutSuffix(entry.Name(), recordSuffix)
		if isRecord && !entry.IsDir() && ValidPlayerID(playerID) {
			players = append(players, playerID)
		}
	}
	return players, nil
}

func (s *Store) summaryChangedLocked(playerID string) {
	if summariesWereRequested := s.summaries != nil; summariesWereRequested {
		s.changed[playerID] = struct{}{}
	}
}
