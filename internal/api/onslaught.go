package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/jealous-sound/hoda-custom-backend/internal/economy"
	"github.com/jealous-sound/hoda-custom-backend/internal/guild"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

type onslaughtReply struct {
	Cycle          int64                `json:"cycle"`
	Finished       bool                 `json:"finished"`
	TotalGuilds    int                  `json:"total_guilds"`
	Guilds         []guild.Standing     `json:"guilds"`
	Mine           *guild.Standing      `json:"mine"`
	Members        []guild.Contribution `json:"members"`
	MyBanners      int                  `json:"my_banners"`
	MinimumBanners int                  `json:"minimum_banners"`
	AwardRank      int                  `json:"award_rank"`
	Claimed        bool                 `json:"claimed"`
}

type onslaughtClaimBody struct {
	Cycle int64 `json:"cycle"`
}

func (s *Server) currentCycle() int64 {
	return s.now().Unix() / s.catalog.Pvp.Events.DurationSeconds
}

func (s *Server) onslaughtAward(state *guild.State, playerID string, cycle int64) (int, bool) {
	events := &s.catalog.Pvp.Events
	mine, found := state.OnslaughtGuild(playerID, cycle)
	if !found || cycle >= s.currentCycle() || !events.IsGuildCycle(cycle) {
		return 0, false
	}
	details := events.ForCycle(cycle)
	if state.OnslaughtBanners(playerID, cycle) < details.GuildMinimumBanners {
		return 0, false
	}
	if _, rewarded := details.GuildReward(mine.Rank); !rewarded {
		return 0, false
	}
	return mine.Rank, true
}

func (s *Server) onslaughtStandings(w http.ResponseWriter, r *http.Request, player string) {
	cycle, err := strconv.ParseInt(r.URL.Query().Get("cycle"), 10, 64)
	events := &s.catalog.Pvp.Events
	if err != nil || cycle < 0 || cycle > s.currentCycle() || !events.IsGuildCycle(cycle) {
		writeJSON(w, http.StatusUnprocessableEntity, guildRefusalReply{
			Error: "cycle must be a started guild event", Code: string(guild.InvalidArgument),
		})
		return
	}
	reply := onslaughtReply{
		Cycle: cycle, Finished: cycle < s.currentCycle(), Guilds: []guild.Standing{},
		MinimumBanners: events.ForCycle(cycle).GuildMinimumBanners,
	}
	s.guilds.View(func(state *guild.State) {
		standings := state.OnslaughtStandings(cycle)
		reply.TotalGuilds = len(standings)
		reply.Guilds = standings[:min(len(standings), guild.OnslaughtLeaderboardLimit)]
		if mine, found := state.OnslaughtGuild(player, cycle); found {
			reply.Mine = &mine
		}
		reply.Members = state.OnslaughtMembers(player, cycle)
		reply.MyBanners = state.OnslaughtBanners(player, cycle)
		reply.AwardRank, _ = s.onslaughtAward(state, player, cycle)
		reply.Claimed = state.OnslaughtClaimed(player, cycle)
	})
	writeJSON(w, http.StatusOK, reply)
}

func (s *Server) claimOnslaught(w http.ResponseWriter, r *http.Request, player string) {
	s.runAction(w, r, player, onslaughtClaimAction, func(body []byte) (action, error) {
		var request onslaughtClaimBody
		if err := json.Unmarshal(body, &request); err != nil {
			return nil, errors.New(`body must be {"request_id", "cycle"}`)
		}
		return func(record *profile.Record) (any, error) {
			var result economy.BattleResult
			err := s.guilds.Update(func(state *guild.State) error {
				rank, eligible := s.onslaughtAward(state, player, request.Cycle)
				if !eligible || !state.ClaimOnslaught(player, request.Cycle) {
					return &economy.Refusal{Code: economy.Unavailable, Message: "no unclaimed guild award for this event"}
				}
				reward, _ := s.catalog.Pvp.Events.ForCycle(request.Cycle).GuildReward(rank)
				next, granted, err := economy.ClaimGuildAward(s.catalog, record.Profile, &record.Server, reward, rank,
					s.now())
				if err != nil {
					return err
				}
				record.Profile, result = next, granted
				return nil
			})
			return result, err
		}, nil
	})
}

func (s *Server) creditOnslaught(record *profile.Record, playerID string, result economy.BattleResult) error {
	events := &s.catalog.Pvp.Events
	if result.Banners <= 0 || !events.IsGuildCycle(result.Cycle) {
		return nil
	}
	p, err := identityFrom(playerID, record.Profile)
	if err != nil {
		return nil
	}
	return s.guilds.Update(func(state *guild.State) error {
		state.CreditOnslaught(p, result.Cycle, events.CycleStart(result.Cycle), result.Banners)
		return nil
	})
}
