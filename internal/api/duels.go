package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/jealous-sound/hoda-custom-backend/internal/economy"
	"github.com/jealous-sound/hoda-custom-backend/internal/guild"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

type guildDuels struct {
	server   *Server
	player   string
	trophies map[string]int
}

type duelBody struct {
	Duel string `json:"duel"`
}

func (s *Server) duelTrophies(body []byte) map[string]int {
	var request duelBody
	if err := json.Unmarshal(body, &request); err != nil || request.Duel == "" {
		return nil
	}
	record, err := s.profiles.Get(request.Duel)
	if err != nil {
		return nil
	}
	opponent, err := economy.DuelOpponentFrom(request.Duel, record.Profile, s.now())
	if err != nil {
		return nil
	}
	return map[string]int{request.Duel: opponent.Trophies}
}

func (s *Server) duelMember(player, member string) (guild.Member, error) {
	var found guild.Member
	err := refusalFor(guild.NotInGuild, "not in a guild")
	s.guilds.View(func(state *guild.State) {
		mine := state.GuildOf(player)
		if mine == nil {
			return
		}
		opponent, isMember := mine.Member(member)
		if !isMember || member == player {
			err = refusalFor(guild.PlayerNotFound, fmt.Sprintf("%q is not another member of your guild", member))
			return
		}
		found, err = opponent, nil
	})
	return found, err
}

func refusalFor(code guild.Code, message string) error {
	return &guild.Refusal{Code: code, Message: message}
}

func (d guildDuels) DuelOpponent(member string) (economy.DuelMatch, error) {
	opponent, err := d.server.duelMember(d.player, member)
	if err != nil {
		return economy.DuelMatch{}, &economy.Refusal{Code: economy.Unavailable, Message: err.Error()}
	}
	return economy.DuelMatch{Name: opponent.Name, Level: opponent.Level, Trophies: d.trophies[member]}, nil
}

func (s *Server) duelOpponent(w http.ResponseWriter, r *http.Request, player string) {
	member := r.PathValue("member")
	if _, err := s.duelMember(player, member); err != nil {
		s.writeGuildFailure(w, player, err)
		return
	}
	record, err := s.profiles.Get(member)
	if errors.Is(err, profile.ErrNotFound) {
		s.writeGuildFailure(w, player, refusalFor(guild.PlayerNotFound, "the member has no profile"))
		return
	}
	if err != nil {
		s.log.Error("read duel opponent", "player", player, "member", member, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the opponent")
		return
	}
	opponent, err := economy.DuelOpponentFrom(member, record.Profile, s.now())
	if err == nil && len(opponent.Units) == 0 {
		err = refusalFor(guild.PlayerNotFound, "the member has no squad")
	}
	if err != nil {
		var refusal *economy.Refusal
		if errors.As(err, &refusal) {
			err = refusalFor(guild.PlayerNotFound, refusal.Message)
		}
		s.writeGuildFailure(w, player, err)
		return
	}
	writeJSON(w, http.StatusOK, opponent)
}
