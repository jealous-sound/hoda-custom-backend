package api

import (
	"cmp"
	"errors"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/economy"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const (
	ladderPlayerLimit = 100
	ladderMaxAge      = 10 * time.Second
)

type ladderPlayer struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Level    int    `json:"level"`
	Trophies int    `json:"trophies"`
}

type ladderResponse struct {
	Players []ladderPlayer `json:"players"`
}

type rankedPlayers struct {
	mutex   sync.Mutex
	readAt  time.Time
	players []ladderPlayer
}

func ranked(summary profile.Summary) bool {
	foughtPvp := summary.PvpWins+summary.PvpLosses > 0
	return !summary.Unreadable && summary.Name != "" && foughtPvp
}

func (s *Server) readRankedPlayers() ([]ladderPlayer, error) {
	summaries, err := s.profiles.Summaries()
	if err != nil {
		return nil, err
	}
	players := []ladderPlayer{}
	for _, summary := range summaries {
		if ranked(summary) {
			players = append(players, ladderPlayer{
				ID: summary.PlayerID, Name: summary.Name, Level: max(summary.Level, 1), Trophies: summary.Trophies,
			})
		}
	}
	slices.SortFunc(players, func(a, b ladderPlayer) int {
		return cmp.Or(cmp.Compare(b.Trophies, a.Trophies), cmp.Compare(a.ID, b.ID))
	})
	return players, nil
}

func (c *rankedPlayers) current(now time.Time, read func() ([]ladderPlayer, error)) ([]ladderPlayer, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if age := now.Sub(c.readAt); c.players == nil || age >= ladderMaxAge || age < 0 {
		players, err := read()
		if err != nil {
			return nil, err
		}
		c.players, c.readAt = players, now
	}
	return slices.Clone(c.players), nil
}

func (s *Server) ladder(w http.ResponseWriter, _ *http.Request, player string) {
	players, err := s.rankedPlayers.current(s.now(), s.readRankedPlayers)
	if err != nil {
		s.log.Error("read ladder", "player", player, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the ladder")
		return
	}
	rivals := slices.DeleteFunc(players, func(listed ladderPlayer) bool { return listed.ID == player })
	writeJSON(w, http.StatusOK, ladderResponse{Players: rivals[:min(len(rivals), ladderPlayerLimit)]})
}

type duelRival struct {
	opponent economy.DuelOpponent
	ranked   bool
}

func (s *Server) duelRival(playerID string) (duelRival, error) {
	record, err := s.profiles.Get(playerID)
	if err != nil {
		return duelRival{}, err
	}
	opponent, err := economy.DuelOpponentFrom(playerID, record.Profile, s.now())
	if err != nil {
		return duelRival{}, err
	}
	return duelRival{opponent: opponent, ranked: ranked(profile.Summarize(record))}, nil
}

func (s *Server) ladderRival(w http.ResponseWriter, r *http.Request, player string) {
	rivalID := r.PathValue("rival")
	rival, err := s.duelRival(rivalID)
	var unreadable *economy.Refusal
	noSuchPlayer := errors.Is(err, profile.ErrNotFound) || errors.Is(err, profile.ErrInvalidPlayerID) ||
		errors.As(err, &unreadable)
	if err != nil && !noSuchPlayer {
		s.log.Error("read ladder rival", "player", player, "rival", rivalID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the rival")
		return
	}
	if noSuchPlayer || rivalID == player || !rival.ranked || len(rival.opponent.Units) == 0 {
		writeError(w, http.StatusNotFound, "no such rival on the ladder")
		return
	}
	writeJSON(w, http.StatusOK, rival.opponent)
}
