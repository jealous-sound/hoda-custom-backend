package guild

import (
	"crypto/rand"
	"encoding/hex"
	"maps"
	"slices"
	"strings"
)

type Rank int

const (
	RankMember    Rank = 0
	RankOfficer   Rank = 1
	RankLeader    Rank = 2
	RankNotMember Rank = 999
)

type Status int

const (
	StatusOpen            Status = 0
	StatusApplicationOnly Status = 1
	StatusClosed          Status = 2
)

type Member struct {
	PlayerID  string `json:"id"`
	Name      string `json:"name"`
	Rank      Rank   `json:"rank"`
	JoinedAt  int64  `json:"join_date"`
	LastLogin int64  `json:"last_login_date"`
	Level     int    `json:"player_level"`
}

type Application struct {
	PlayerID    string `json:"id"`
	Name        string `json:"name"`
	Level       int    `json:"player_level"`
	RequestedAt int64  `json:"request_date"`
	Text        string `json:"application_text,omitempty"`
}

type EventParam struct {
	Text   string `json:"text"`
	LocKey bool   `json:"loc_key"`
}

type Message struct {
	ID         int64        `json:"id"`
	Time       int64        `json:"time"`
	Text       string       `json:"text"`
	PlayerID   string       `json:"player_id,omitempty"`
	PlayerName string       `json:"player_name,omitempty"`
	Level      int          `json:"player_level,omitempty"`
	Rank       Rank         `json:"player_rank"`
	Title      string       `json:"title,omitempty"`
	Params     []EventParam `json:"params,omitempty"`
}

func (m Message) IsEvent() bool {
	return m.Title != ""
}

type Guild struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Description     string           `json:"description"`
	MessageOfTheDay string           `json:"message_of_the_day"`
	Language        string           `json:"language"`
	Icon            int              `json:"icon"`
	Status          Status           `json:"status"`
	RequiredLevel   int              `json:"required_player_level"`
	CreatedAt       int64            `json:"creation_date"`
	Members         []Member         `json:"members"`
	Applications    []Application    `json:"pending_requests"`
	Messages        []Message        `json:"messages"`
	NextMessageID   int64            `json:"next_message_id"`
	ReadThrough     map[string]int64 `json:"read_through"`
}

type NoticeKind string

const (
	NoticeAccepted  NoticeKind = "accepted"
	NoticeDenied    NoticeKind = "denied"
	NoticeKicked    NoticeKind = "kicked"
	NoticePromoted  NoticeKind = "promoted"
	NoticeDemoted   NoticeKind = "demoted"
	NoticeNewLeader NoticeKind = "new_leader"
)

type Notice struct {
	Kind  NoticeKind `json:"kind"`
	Guild string     `json:"guild"`
	Rank  Rank       `json:"rank"`
	Time  int64      `json:"time"`
}

type State struct {
	Guilds  []*Guild            `json:"guilds"`
	Notices map[string][]Notice `json:"notices"`
}

func newState() *State {
	return &State{Guilds: []*Guild{}, Notices: map[string][]Notice{}}
}

func (s *State) normalize() {
	if s.Guilds == nil {
		s.Guilds = []*Guild{}
	}
	if s.Notices == nil {
		s.Notices = map[string][]Notice{}
	}
}

func (s *State) clone() *State {
	copied := &State{Guilds: make([]*Guild, 0, len(s.Guilds)), Notices: make(map[string][]Notice, len(s.Notices))}
	for _, g := range s.Guilds {
		copied.Guilds = append(copied.Guilds, g.clone())
	}
	for player, notices := range s.Notices {
		copied.Notices[player] = slices.Clone(notices)
	}
	return copied
}

func (g *Guild) clone() *Guild {
	copied := *g
	copied.Members = slices.Clone(g.Members)
	copied.Applications = slices.Clone(g.Applications)
	copied.ReadThrough = maps.Clone(g.ReadThrough)
	copied.Messages = make([]Message, len(g.Messages))
	for i, message := range g.Messages {
		message.Params = slices.Clone(message.Params)
		copied.Messages[i] = message
	}
	return &copied
}

func (s *State) GuildOf(playerID string) *Guild {
	for _, g := range s.Guilds {
		if g.member(playerID) >= 0 {
			return g
		}
	}
	return nil
}

func (s *State) Guild(id string) *Guild {
	for _, g := range s.Guilds {
		if g.ID == id {
			return g
		}
	}
	return nil
}

func (s *State) GuildNamed(name string) *Guild {
	for _, g := range s.Guilds {
		if strings.EqualFold(g.Name, name) {
			return g
		}
	}
	return nil
}

func (s *State) ApplicationsOf(playerID string) []*Guild {
	var applied []*Guild
	for _, g := range s.Guilds {
		if g.application(playerID) >= 0 {
			applied = append(applied, g)
		}
	}
	return applied
}

func (s *State) removeGuild(id string) {
	s.Guilds = slices.DeleteFunc(s.Guilds, func(g *Guild) bool { return g.ID == id })
}

func (s *State) notify(playerID string, notice Notice) {
	s.Notices[playerID] = append(s.Notices[playerID], notice)
	if extra := len(s.Notices[playerID]) - noticeLimit; extra > 0 {
		s.Notices[playerID] = slices.Clone(s.Notices[playerID][extra:])
	}
}

func (s *State) TakeNotices(playerID string) []Notice {
	notices := s.Notices[playerID]
	delete(s.Notices, playerID)
	return notices
}

func (s *State) withdrawApplications(p Player, now int64) {
	for _, g := range s.Guilds {
		if index := g.application(p.ID); index >= 0 {
			g.Applications = slices.Delete(g.Applications, index, index+1)
			g.postEvent(now, "APPLICATION_WITHDRAWN", plain(p.Name))
		}
	}
}

func (g *Guild) member(playerID string) int {
	return slices.IndexFunc(g.Members, func(m Member) bool { return m.PlayerID == playerID })
}

func (g *Guild) Member(playerID string) (Member, bool) {
	index := g.member(playerID)
	if index < 0 {
		return Member{}, false
	}
	return g.Members[index], true
}

func (g *Guild) application(playerID string) int {
	return slices.IndexFunc(g.Applications, func(a Application) bool { return a.PlayerID == playerID })
}

func (g *Guild) leader() int {
	return slices.IndexFunc(g.Members, func(m Member) bool { return m.Rank == RankLeader })
}

func (g *Guild) LastMessageID() int64 {
	if len(g.Messages) == 0 {
		return 0
	}
	return g.Messages[len(g.Messages)-1].ID
}

func (g *Guild) MessagesAfter(id int64) []Message {
	start := slices.IndexFunc(g.Messages, func(m Message) bool { return m.ID > id })
	if start < 0 {
		return []Message{}
	}
	return slices.Clone(g.Messages[start:])
}

func (g *Guild) MarkRead(playerID string, through int64) {
	through = min(through, g.LastMessageID())
	if through <= g.ReadThrough[playerID] {
		return
	}
	if g.ReadThrough == nil {
		g.ReadThrough = map[string]int64{}
	}
	g.ReadThrough[playerID] = through
}

func (g *Guild) Unread(playerID string) int {
	read := g.ReadThrough[playerID]
	unread := 0
	for _, message := range g.Messages {
		if message.ID > read && message.PlayerID != playerID {
			unread++
		}
	}
	return unread
}

func (g *Guild) forget(playerID string) {
	delete(g.ReadThrough, playerID)
}

func (g *Guild) post(message Message) {
	g.NextMessageID++
	message.ID = g.NextMessageID
	g.Messages = append(g.Messages, message)
	if extra := len(g.Messages) - MessageHistoryLimit; extra > 0 {
		g.Messages = slices.Clone(g.Messages[extra:])
	}
}

func (g *Guild) postEvent(now int64, event string, params ...EventParam) {
	g.post(Message{
		Time:   now,
		Title:  "GUILD_CHAT_EVENT_" + event + "_TITLE",
		Text:   "GUILD_CHAT_EVENT_" + event + "_MESSAGE",
		Rank:   RankNotMember,
		Params: params,
	})
}

func plain(text string) EventParam {
	return EventParam{Text: text}
}

func localized(key string) EventParam {
	return EventParam{Text: key, LocKey: true}
}

func rankKey(rank Rank) string {
	switch rank {
	case RankLeader:
		return "GUILD_RANK_LEADER"
	case RankOfficer:
		return "GUILD_RANK_OFFICER"
	default:
		return "GUILD_RANK_MEMBER"
	}
}

func statusKey(status Status) string {
	switch status {
	case StatusApplicationOnly:
		return "GUILD_STATUS_APPLICATION_ONLY"
	case StatusClosed:
		return "GUILD_STATUS_CLOSED"
	default:
		return "GUILD_STATUS_OPEN"
	}
}

func newGuildID() (string, error) {
	raw := make([]byte, guildIDBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "guild-" + hex.EncodeToString(raw), nil
}
