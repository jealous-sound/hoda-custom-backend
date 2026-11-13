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
	ladderPlayerLimit      = 100
	eventLadderPlayerLimit = 250
	ladderMaxAge           = 10 * time.Second
	rereadBeforeAClaim     = time.Duration(0)
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

type eventLadderPlayer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Banners int    `json:"banners"`
}

type eventLadderResponse struct {
	Cycle   int64                   `json:"cycle"`
	Players []eventLadderPlayer     `json:"players"`
	Results []economy.EventStanding `json:"results"`
}

type rankedPlayers struct {
	mutex       sync.Mutex
	readAt      time.Time
	byTrophies  []profile.Summary
	everWasRead bool
}

func ranked(summary profile.Summary) bool {
	foughtPvp := summary.PvpWins+summary.PvpLosses > 0
	return !summary.Unreadable && summary.Name != "" && foughtPvp
}

func (s *Server) readRankedPlayers() ([]profile.Summary, error) {
	summaries, err := s.profiles.Summaries()
	if err != nil {
		return nil, err
	}
	players := slices.DeleteFunc(summaries, func(summary profile.Summary) bool { return !ranked(summary) })
	slices.SortFunc(players, func(a, b profile.Summary) int {
		return cmp.Or(cmp.Compare(b.Trophies, a.Trophies), cmp.Compare(a.PlayerID, b.PlayerID))
	})
	return players, nil
}

func (c *rankedPlayers) noOlderThan(
	maxAge time.Duration, now time.Time, read func() ([]profile.Summary, error),
) ([]profile.Summary, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if age := now.Sub(c.readAt); !c.everWasRead || age >= maxAge || age < 0 {
		players, err := read()
		if err != nil {
			return nil, err
		}
		c.byTrophies, c.readAt, c.everWasRead = players, now, true
	}
	return c.byTrophies, nil
}

func (s *Server) rankedByTrophies(maxAge time.Duration) ([]profile.Summary, error) {
	return s.rankedPlayers.noOlderThan(maxAge, s.now(), s.readRankedPlayers)
}

func (s *Server) ladder(w http.ResponseWriter, _ *http.Request, player string) {
	players, err := s.rankedByTrophies(ladderMaxAge)
	if err != nil {
		s.log.Error("read ladder", "player", player, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the ladder")
		return
	}
	rivals := []ladderPlayer{}
	for _, listed := range players {
		if listed.PlayerID != player && len(rivals) < ladderPlayerLimit {
			rivals = append(rivals, ladderPlayer{
				ID: listed.PlayerID, Name: listed.Name, Level: max(listed.Level, 1), Trophies: listed.Trophies,
			})
		}
	}
	writeJSON(w, http.StatusOK, ladderResponse{Players: rivals})
}

type eventRivals struct {
	player string
	ranked []profile.Summary
}

func (r eventRivals) RivalsAhead(cycle int64, banners int) int {
	ahead := 0
	for _, rival := range r.ranked {
		if rival.PlayerID != r.player && rival.EventBanners(cycle) > banners {
			ahead++
		}
	}
	return ahead
}

func (r eventRivals) top(cycle int64) []eventLadderPlayer {
	players := []eventLadderPlayer{}
	for _, rival := range r.ranked {
		if banners := rival.EventBanners(cycle); banners > 0 && rival.PlayerID != r.player {
			players = append(players, eventLadderPlayer{ID: rival.PlayerID, Name: rival.Name, Banners: banners})
		}
	}
	slices.SortFunc(players, func(a, b eventLadderPlayer) int {
		return cmp.Or(cmp.Compare(b.Banners, a.Banners), cmp.Compare(a.ID, b.ID))
	})
	return players[:min(len(players), eventLadderPlayerLimit)]
}

func standingsAgeFor(action string) (time.Duration, bool) {
	switch action {
	case economy.ActionPvpEventClaim:
		return rereadBeforeAClaim, true
	case economy.ActionBattle, economy.ActionBattleStart:
		return ladderMaxAge, true
	}
	return 0, false
}

func (s *Server) eventRivalsFor(player, action string) (economy.EventStandings, error) {
	maxAge, ranksAnEvent := standingsAgeFor(action)
	if !ranksAnEvent {
		return nil, nil
	}
	ranked, err := s.rankedByTrophies(maxAge)
	if err != nil {
		return nil, err
	}
	return eventRivals{player: player, ranked: ranked}, nil
}

func (s *Server) eventLadder(w http.ResponseWriter, _ *http.Request, player string) {
	ranked, err := s.rankedByTrophies(ladderMaxAge)
	if err != nil {
		s.log.Error("read event ladder", "player", player, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the event ladder")
		return
	}
	rivals := eventRivals{player: player, ranked: ranked}
	running := s.currentCycle()
	reply := eventLadderResponse{Cycle: running, Players: rivals.top(running), Results: []economy.EventStanding{}}
	record, err := s.profiles.Get(player)
	switch {
	case err == nil:
		reply.Results = economy.UnclaimedEventStandings(&s.catalog.Pvp, &record.Server, rivals, s.now())
	case !errors.Is(err, profile.ErrNotFound):
		s.log.Error("read event standings", "player", player, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the event ladder")
		return
	}
	writeJSON(w, http.StatusOK, reply)
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
