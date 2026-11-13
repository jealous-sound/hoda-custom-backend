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

type playerDuels struct {
	server *Server
	player string
	rivals map[string]duelRival
}

type duelBody struct {
	Duel string `json:"duel"`
}

func (s *Server) duelRivals(body []byte) map[string]duelRival {
	var request duelBody
	if err := json.Unmarshal(body, &request); err != nil || request.Duel == "" {
		return nil
	}
	rival, err := s.duelRival(request.Duel)
	if err != nil {
		return nil
	}
	return map[string]duelRival{request.Duel: rival}
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

func (d playerDuels) onLadder(rival string) bool {
	return rival != d.player && d.rivals[rival].ranked
}

func (d playerDuels) refusal(rival string, notAGuildmate error) error {
	return &economy.Refusal{
		Code: economy.Unavailable, Message: fmt.Sprintf("%v, and %q is not on the ladder", notAGuildmate, rival),
	}
}

func (d playerDuels) DuelOpponent(rival string) (economy.DuelMatch, error) {
	stored := d.rivals[rival].opponent
	guildmate, err := d.server.duelMember(d.player, rival)
	switch {
	case err == nil:
		return economy.DuelMatch{Name: guildmate.Name, Level: guildmate.Level, Trophies: stored.Trophies}, nil
	case d.onLadder(rival):
		return economy.DuelMatch{Name: stored.Name, Level: stored.Level, Trophies: stored.Trophies}, nil
	}
	return economy.DuelMatch{}, d.refusal(rival, err)
}

func (d playerDuels) DuelSquad(rival string) (economy.DuelOpponent, error) {
	if _, err := d.server.duelMember(d.player, rival); err != nil && !d.onLadder(rival) {
		return economy.DuelOpponent{}, d.refusal(rival, err)
	}
	stored, ok := d.rivals[rival]
	if !ok {
		return economy.DuelOpponent{}, &economy.Refusal{Code: economy.Unavailable, Message: "the member has no profile"}
	}
	return stored.opponent, nil
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
