package api

import (
	"net/http"
	"slices"

	"github.com/jealous-sound/hoda-custom-backend/internal/guild"
)

type adminGuildSummary struct {
	guildSummary
	MessageOfTheDay string `json:"message_of_the_day"`
	CreatedAt       int64  `json:"creation_date"`
	LeaderID        string `json:"leader_id"`
	LeaderName      string `json:"leader_name"`
	Applications    int    `json:"application_count"`
	LastMessageAt   int64  `json:"last_message_at"`
}

func adminSummaryOf(g *guild.Guild) adminGuildSummary {
	summary := adminGuildSummary{
		guildSummary: summarize(g), MessageOfTheDay: g.MessageOfTheDay, CreatedAt: g.CreatedAt,
		Applications: len(g.Applications),
	}
	for _, member := range g.Members {
		if member.Rank == guild.RankLeader {
			summary.LeaderID, summary.LeaderName = member.PlayerID, member.Name
		}
	}
	if len(g.Messages) > 0 {
		summary.LastMessageAt = g.Messages[len(g.Messages)-1].Time
	}
	return summary
}

type guildsReply struct {
	ServerTime int64               `json:"server_time"`
	Guilds     []adminGuildSummary `json:"guilds"`
}

func (s *Server) adminGuilds(w http.ResponseWriter, _ *http.Request) {
	reply := guildsReply{ServerTime: s.now().Unix(), Guilds: []adminGuildSummary{}}
	s.guilds.View(func(state *guild.State) {
		for _, g := range state.Guilds {
			reply.Guilds = append(reply.Guilds, adminSummaryOf(g))
		}
	})
	writeJSON(w, http.StatusOK, reply)
}

type guildReply struct {
	ServerTime   int64               `json:"server_time"`
	Guild        adminGuildSummary   `json:"guild"`
	Members      []guild.Member      `json:"members"`
	Applications []guild.Application `json:"pending_requests"`
	Messages     []guild.Message     `json:"messages"`
}

func (s *Server) adminGuild(w http.ResponseWriter, r *http.Request) {
	var reply *guildReply
	s.guilds.View(func(state *guild.State) {
		g := state.Guild(r.PathValue("guild"))
		if g == nil {
			return
		}
		reply = &guildReply{
			ServerTime: s.now().Unix(), Guild: adminSummaryOf(g), Members: append([]guild.Member{}, g.Members...),
			Applications: append([]guild.Application{}, g.Applications...), Messages: g.MessagesAfter(0),
		}
	})
	if reply == nil {
		writeError(w, http.StatusNotFound, "no such guild")
		return
	}
	slices.SortFunc(reply.Members, func(a, b guild.Member) int { return int(b.Rank) - int(a.Rank) })
	writeJSON(w, http.StatusOK, reply)
}
