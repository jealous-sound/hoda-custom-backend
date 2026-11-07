package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/jealous-sound/hoda-custom-backend/internal/economy"
	"github.com/jealous-sound/hoda-custom-backend/internal/guild"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

type guildDuels struct {
	server      *Server
	player      string
	opponent    *economy.DuelOpponent
	opponentErr error
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

func (d guildDuels) DuelLevel(member string) (int, error) {
	opponent, err := d.server.duelMember(d.player, member)
	if err != nil {
		return 0, &economy.Refusal{Code: economy.Unavailable, Message: err.Error()}
	}
	return opponent.Level, nil
}

func (d guildDuels) DuelOpponent(member string) (economy.DuelOpponent, error) {
	if d.opponentErr != nil {
		return economy.DuelOpponent{}, d.opponentErr
	}
	if d.opponent == nil || d.opponent.ID != member {
		return economy.DuelOpponent{}, &economy.Refusal{Code: economy.Unavailable, Message: "no duel opponent loaded"}
	}
	return *d.opponent, nil
}

func (s *Server) fetchDuelOpponent(player, member string) (*economy.DuelOpponent, error) {
	if _, err := s.duelMember(player, member); err != nil {
		return nil, &economy.Refusal{Code: economy.Unavailable, Message: err.Error()}
	}
	record, err := s.profiles.Get(member)
	if err != nil {
		return nil, &economy.Refusal{Code: economy.Unavailable, Message: "the member has no profile"}
	}
	opponent, err := economy.DuelOpponentFrom(member, record.Profile, s.now())
	if err != nil {
		return nil, err
	}
	return &opponent, nil
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
