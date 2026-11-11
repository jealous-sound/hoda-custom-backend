package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/jealous-sound/hoda-custom-backend/internal/economy"
	"github.com/jealous-sound/hoda-custom-backend/internal/guild"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const maxGuildBodyBytes = 4096

type guildSummary struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Description   string       `json:"description"`
	Language      string       `json:"language"`
	Icon          int          `json:"icon"`
	Status        guild.Status `json:"status"`
	RequiredLevel int          `json:"required_player_level"`
	MemberCount   int          `json:"member_count"`
	MaxMembers    int          `json:"max_members"`
}

type guildView struct {
	guildSummary
	MessageOfTheDay string              `json:"message_of_the_day"`
	CreatedAt       int64               `json:"creation_date"`
	Members         []guild.Member      `json:"members"`
	PendingRequests []guild.Application `json:"pending_requests"`
	MaxApplications int                 `json:"max_applications"`
	LastMessageID   int64               `json:"last_message_id"`
}

type guildStateReply struct {
	Rank         guild.Rank     `json:"rank"`
	Unread       int            `json:"unread"`
	Guild        *guildView     `json:"guild"`
	Applications []guildSummary `json:"applications"`
	Notices      []guild.Notice `json:"notices"`
}

type guildListReply struct {
	Guilds []guildSummary `json:"guilds"`
}

type observedGuildReply struct {
	Guild   guildView `json:"guild"`
	Applied bool      `json:"applied"`
}

type guildMessagesReply struct {
	Rank          guild.Rank      `json:"rank"`
	Unread        int             `json:"unread"`
	Messages      []guild.Message `json:"messages"`
	LastMessageID int64           `json:"last_message_id"`
}

type guildRefusalReply struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

type joinBody struct {
	ApplicationText string `json:"application_text"`
}

type rankBody struct {
	TargetRank guild.Rank `json:"target_rank"`
}

type messagesBody struct {
	After       int64  `json:"after"`
	Text        string `json:"text"`
	ReadThrough int64  `json:"read_through"`
}

type createGuildBody struct {
	guild.Settings
}

type profileIdentity struct {
	Account struct {
		Name  string `json:"name"`
		Level int    `json:"level"`
	} `json:"account"`
}

func identityFrom(playerID string, raw json.RawMessage) (guild.Player, error) {
	var identity profileIdentity
	if err := json.Unmarshal(raw, &identity); err != nil {
		return guild.Player{}, err
	}
	name := identity.Account.Name
	if name == "" {
		name = playerID
	}
	return guild.Player{ID: playerID, Name: name, Level: identity.Account.Level}, nil
}

func summarize(g *guild.Guild) guildSummary {
	return guildSummary{
		ID: g.ID, Name: g.Name, Description: g.Description, Language: g.Language, Icon: g.Icon, Status: g.Status,
		RequiredLevel: g.RequiredLevel, MemberCount: len(g.Members), MaxMembers: guild.MaxMembers,
	}
}

func viewOf(g *guild.Guild, viewerRank guild.Rank) guildView {
	view := guildView{
		guildSummary: summarize(g), MessageOfTheDay: g.MessageOfTheDay, CreatedAt: g.CreatedAt,
		Members: append([]guild.Member{}, g.Members...), PendingRequests: []guild.Application{},
		MaxApplications: guild.MaxApplications, LastMessageID: g.LastMessageID(),
	}
	if viewerRank >= guild.RankOfficer && viewerRank != guild.RankNotMember {
		view.PendingRequests = append(view.PendingRequests, g.Applications...)
	}
	return view
}

func rankIn(g *guild.Guild, playerID string) guild.Rank {
	if g == nil {
		return guild.RankNotMember
	}
	member, found := g.Member(playerID)
	if !found {
		return guild.RankNotMember
	}
	return member.Rank
}

func stateReply(state *guild.State, playerID string) guildStateReply {
	reply := guildStateReply{
		Rank: guild.RankNotMember, Applications: []guildSummary{}, Notices: state.TakeNotices(playerID),
	}
	if reply.Notices == nil {
		reply.Notices = []guild.Notice{}
	}
	if mine := state.GuildOf(playerID); mine != nil {
		reply.Rank = rankIn(mine, playerID)
		reply.Unread = mine.Unread(playerID)
		view := viewOf(mine, reply.Rank)
		reply.Guild = &view
	}
	for _, applied := range state.ApplicationsOf(playerID) {
		reply.Applications = append(reply.Applications, summarize(applied))
	}
	return reply
}

func (s *Server) guildPlayer(w http.ResponseWriter, playerID string) (guild.Player, bool) {
	record, err := s.profiles.Get(playerID)
	if errors.Is(err, profile.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no profile yet")
		return guild.Player{}, false
	}
	if err != nil {
		s.log.Error("read profile", "player", playerID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read profile")
		return guild.Player{}, false
	}
	p, err := identityFrom(playerID, record.Profile)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "profile has no readable account")
		return guild.Player{}, false
	}
	return p, true
}

func readGuildBody(w http.ResponseWriter, r *http.Request, into any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxGuildBodyBytes+1))
	if err != nil || len(body) > maxGuildBodyBytes {
		writeError(w, http.StatusBadRequest, "could not read body")
		return false
	}
	if len(body) == 0 {
		return true
	}
	if err := json.Unmarshal(body, into); err != nil {
		writeError(w, http.StatusBadRequest, "body is not valid JSON for this call")
		return false
	}
	return true
}

func (s *Server) writeGuildFailure(w http.ResponseWriter, playerID string, err error) {
	var refusal *guild.Refusal
	if errors.As(err, &refusal) {
		writeJSON(w, http.StatusUnprocessableEntity, guildRefusalReply{Error: refusal.Message, Code: string(refusal.Code)})
		return
	}
	s.log.Error("guild call", "player", playerID, "error", err)
	writeError(w, http.StatusInternalServerError, "could not update guilds")
}

type guildChange func(state *guild.State, p guild.Player, now int64) error

func (s *Server) changeGuilds(w http.ResponseWriter, playerID string, change guildChange) {
	p, ok := s.guildPlayer(w, playerID)
	if !ok {
		return
	}
	now := s.now().Unix()
	var reply guildStateReply
	err := s.guilds.Update(func(state *guild.State) error {
		state.Touch(p, now)
		if err := change(state, p, now); err != nil {
			return err
		}
		reply = stateReply(state, p.ID)
		return nil
	})
	if err != nil {
		s.writeGuildFailure(w, playerID, err)
		return
	}
	writeJSON(w, http.StatusOK, reply)
}

func (s *Server) getGuild(w http.ResponseWriter, _ *http.Request, player string) {
	s.changeGuilds(w, player, func(*guild.State, guild.Player, int64) error { return nil })
}

func (s *Server) recommendedGuilds(w http.ResponseWriter, _ *http.Request, player string) {
	p, ok := s.guildPlayer(w, player)
	if !ok {
		return
	}
	reply := guildListReply{Guilds: []guildSummary{}}
	s.guilds.View(func(state *guild.State) {
		for _, g := range state.Recommended(p) {
			reply.Guilds = append(reply.Guilds, summarize(g))
		}
	})
	writeJSON(w, http.StatusOK, reply)
}

func (s *Server) searchGuilds(w http.ResponseWriter, r *http.Request, _ string) {
	reply := guildListReply{Guilds: []guildSummary{}}
	name := r.URL.Query().Get("name")
	s.guilds.View(func(state *guild.State) {
		if found := state.GuildNamed(name); found != nil {
			reply.Guilds = append(reply.Guilds, summarize(found))
		}
	})
	writeJSON(w, http.StatusOK, reply)
}

func (s *Server) observeGuild(w http.ResponseWriter, r *http.Request, player string) {
	var reply observedGuildReply
	found := false
	s.guilds.View(func(state *guild.State) {
		g := state.Guild(r.PathValue("guild"))
		if g == nil {
			return
		}
		found = true
		reply.Guild = viewOf(g, rankIn(g, player))
		for _, applied := range state.ApplicationsOf(player) {
			reply.Applied = reply.Applied || applied.ID == g.ID
		}
	})
	if !found {
		writeJSON(w, http.StatusNotFound, guildRefusalReply{Error: "no such guild", Code: string(guild.NotFound)})
		return
	}
	writeJSON(w, http.StatusOK, reply)
}

func (s *Server) createGuild(w http.ResponseWriter, r *http.Request, player string) {
	s.runAction(w, r, player, guildCreateAction, func(body []byte) (action, error) {
		var request createGuildBody
		if err := json.Unmarshal(body, &request); err != nil || request.Name == "" {
			return nil, errors.New(`body must be {"request_id", "name", "description", "message_of_the_day", ` +
				`"language", "status", "icon", "required_level"}`)
		}
		return func(record *profile.Record) (any, error) {
			p, err := identityFrom(player, record.Profile)
			if err != nil {
				return nil, &economy.BadRequest{Message: "profile has no readable account"}
			}
			now := s.now()
			var reply guildStateReply
			err = s.guilds.Update(func(state *guild.State) error {
				if _, err := state.Create(p, request.Settings, now.Unix()); err != nil {
					return err
				}
				next, err := economy.ChargeGold(s.catalog, record.Profile, &record.Server, guild.CreationGoldCost, now)
				if err != nil {
					return err
				}
				record.Profile = next
				reply = stateReply(state, p.ID)
				return nil
			})
			var refusal *guild.Refusal
			if errors.As(err, &refusal) {
				return nil, &economy.Refusal{Code: economy.RefusalCode(refusal.Code), Message: refusal.Message}
			}
			return reply, err
		}, nil
	})
}

func (s *Server) joinGuild(w http.ResponseWriter, r *http.Request, player string) {
	var request joinBody
	if !readGuildBody(w, r, &request) {
		return
	}
	id := r.PathValue("guild")
	s.changeGuilds(w, player, func(state *guild.State, p guild.Player, now int64) error {
		return state.Join(p, id, request.ApplicationText, now)
	})
}

func (s *Server) cancelGuildApplication(w http.ResponseWriter, r *http.Request, player string) {
	id := r.PathValue("guild")
	s.changeGuilds(w, player, func(state *guild.State, p guild.Player, now int64) error {
		return state.CancelApplication(p, id, now)
	})
}

func (s *Server) changeGuildSettings(w http.ResponseWriter, r *http.Request, player string) {
	var request guild.Settings
	if !readGuildBody(w, r, &request) {
		return
	}
	s.changeGuilds(w, player, func(state *guild.State, p guild.Player, now int64) error {
		return state.ChangeSettings(p, request, now)
	})
}

func (s *Server) acceptGuildApplication(w http.ResponseWriter, r *http.Request, player string) {
	member := r.PathValue("member")
	s.changeGuilds(w, player, func(state *guild.State, p guild.Player, now int64) error {
		return state.Accept(p, member, now)
	})
}

func (s *Server) denyGuildApplication(w http.ResponseWriter, r *http.Request, player string) {
	member := r.PathValue("member")
	s.changeGuilds(w, player, func(state *guild.State, p guild.Player, now int64) error {
		return state.Deny(p, member, now)
	})
}

func (s *Server) changeGuildRank(w http.ResponseWriter, r *http.Request, player string) {
	var request rankBody
	if !readGuildBody(w, r, &request) {
		return
	}
	member := r.PathValue("member")
	s.changeGuilds(w, player, func(state *guild.State, p guild.Player, now int64) error {
		return state.ChangeRank(p, member, request.TargetRank, now)
	})
}

func (s *Server) kickGuildMember(w http.ResponseWriter, r *http.Request, player string) {
	member := r.PathValue("member")
	s.changeGuilds(w, player, func(state *guild.State, p guild.Player, now int64) error {
		return state.Kick(p, member, now)
	})
}

func (s *Server) leaveGuild(w http.ResponseWriter, _ *http.Request, player string) {
	s.changeGuilds(w, player, func(state *guild.State, p guild.Player, now int64) error {
		return state.Leave(p, now)
	})
}

func (s *Server) guildMessages(w http.ResponseWriter, r *http.Request, player string) {
	var request messagesBody
	if !readGuildBody(w, r, &request) {
		return
	}
	if request.Text != "" && !s.allow(w, s.limiters.chatByPlayer, player) {
		return
	}
	p, ok := s.guildPlayer(w, player)
	if !ok {
		return
	}
	now := s.now().Unix()
	var reply guildMessagesReply
	err := s.guilds.Update(func(state *guild.State) error {
		state.Touch(p, now)
		if request.Text != "" {
			if err := state.Say(p, request.Text, now); err != nil {
				return err
			}
		}
		mine := state.GuildOf(p.ID)
		reply = guildMessagesReply{Rank: rankIn(mine, p.ID), Messages: []guild.Message{}}
		if mine != nil {
			mine.MarkRead(p.ID, request.ReadThrough)
			reply.Messages = mine.MessagesAfter(request.After)
			reply.LastMessageID = mine.LastMessageID()
			reply.Unread = mine.Unread(p.ID)
		}
		return nil
	})
	if err != nil {
		s.writeGuildFailure(w, player, err)
		return
	}
	writeJSON(w, http.StatusOK, reply)
}
