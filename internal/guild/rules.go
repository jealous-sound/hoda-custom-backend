package guild

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	LevelUnlock           = 5
	CreationGoldCost      = 10000
	MaxMembers            = 30
	MaxApplications       = 10
	MaxRequiredLevel      = 423
	EmblemCount           = 23
	MinNameRunes          = 4
	MaxNameRunes          = 18
	MaxDescriptionRunes   = 150
	MaxMessageOfDayRunes  = 150
	MaxApplicationRunes   = 150
	MaxChatRunes          = 140
	MessageHistoryLimit   = 100
	RecommendedLimit      = 20
	InactiveLeaderSeconds = 432000
	noticeLimit           = 16
	guildIDBytes          = 8
	defaultLanguage       = "ru"
)

var guildNamePattern = regexp.MustCompile(`^[^<>\s][^<>]*$`)

var languageKeys = map[string]string{
	"en": "LANGUAGE_ENGLISH", "fr": "LANGUAGE_FRENCH", "it": "LANGUAGE_ITALIAN", "de": "LANGUAGE_GERMAN",
	"es": "LANGUAGE_SPANISH", "pt": "LANGUAGE_PORTUGUESE", "ja": "LANGUAGE_JAPANESE", "ko": "LANGUAGE_KOREAN",
	"ru": "LANGUAGE_RUSSIAN", "nl": "LANGUAGE_DUTCH", "zh-hans": "LANGUAGE_CHINESE_SIMPLIFIED",
	"zh-hant": "LANGUAGE_CHINESE_TRADITIONAL",
}

type Code string

const (
	AlreadyInGuild         Code = "ALREADY_IN_GUILD"
	NotInGuild             Code = "NOT_IN_GUILD"
	InsufficientFunds      Code = "INSUFFICIENT_FUNDS"
	NameNotUnique          Code = "GUILD_NAME_NOT_UNIQUE"
	InsufficientRank       Code = "INSUFFICIENT_RANK"
	NotFound               Code = "GUILD_NOT_FOUND"
	AlreadyRequestedJoin   Code = "ALREADY_REQUESTED_JOIN"
	PlayerNotFound         Code = "PLAYER_NOT_FOUND"
	LeaderCantLeave        Code = "LEADER_CANT_LEAVE_NON_EMPTY_GUILD"
	LeaderCantBeDemoted    Code = "LEADER_CANT_BE_DEMOTED"
	Closed                 Code = "GUILD_CLOSED"
	InvalidArgument        Code = "INVALID_ARGUMENT"
	FailedLevelRequirement Code = "FAILED_LEVEL_REQUIREMENT"
	Full                   Code = "GUILD_FULL"
	ApplicationsFull       Code = "GUILD_APPLICATIONS_FULL"
	PendingApplications    Code = "GUILD_CAN_NOT_CHANGE_STATUS_WITH_PENDING_APPLICATIONS"
	ChatMessageTooLong     Code = "CHAT_MESSAGE_TOO_LONG"
	ChatLevelTooLow        Code = "CHAT_LEVEL_TOO_LOW"
)

type Refusal struct {
	Code    Code
	Message string
}

func (r *Refusal) Error() string {
	return string(r.Code) + ": " + r.Message
}

func refuse(code Code, format string, args ...any) error {
	return &Refusal{Code: code, Message: fmt.Sprintf(format, args...)}
}

type Player struct {
	ID    string
	Name  string
	Level int
}

type Settings struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	MessageOfTheDay string `json:"message_of_the_day"`
	Language        string `json:"language"`
	Status          Status `json:"status"`
	Icon            int    `json:"icon"`
	RequiredLevel   int    `json:"required_level"`
}

func (s Settings) cleaned() Settings {
	s.Name = strings.TrimSpace(s.Name)
	s.Description = strings.TrimSpace(s.Description)
	s.MessageOfTheDay = strings.TrimSpace(s.MessageOfTheDay)
	s.Language = strings.TrimSpace(s.Language)
	if s.Language == "" {
		s.Language = defaultLanguage
	}
	return s
}

func validateSettings(s Settings) error {
	switch {
	case s.Status < StatusOpen || s.Status > StatusClosed:
		return refuse(InvalidArgument, "unknown status %d", s.Status)
	case s.Icon < 1 || s.Icon > EmblemCount:
		return refuse(InvalidArgument, "icon must be 1-%d", EmblemCount)
	case s.RequiredLevel < LevelUnlock || s.RequiredLevel > MaxRequiredLevel:
		return refuse(InvalidArgument, "required level must be %d-%d", LevelUnlock, MaxRequiredLevel)
	case languageKeys[s.Language] == "":
		return refuse(InvalidArgument, "unknown language %q", s.Language)
	case utf8.RuneCountInString(s.Description) > MaxDescriptionRunes:
		return refuse(InvalidArgument, "description is longer than %d characters", MaxDescriptionRunes)
	case utf8.RuneCountInString(s.MessageOfTheDay) > MaxMessageOfDayRunes:
		return refuse(InvalidArgument, "message of the day is longer than %d characters", MaxMessageOfDayRunes)
	case hasMarkup(s.Description) || hasMarkup(s.MessageOfTheDay):
		return refuse(InvalidArgument, "text cannot contain < or >")
	}
	return nil
}

func validName(name string) bool {
	runes := utf8.RuneCountInString(name)
	return runes >= MinNameRunes && runes <= MaxNameRunes && guildNamePattern.MatchString(name)
}

func (s *State) requireNoGuild(p Player) error {
	if s.GuildOf(p.ID) != nil {
		return refuse(AlreadyInGuild, "already in a guild")
	}
	return nil
}

func (s *State) membership(playerID string) (*Guild, Member, error) {
	g := s.GuildOf(playerID)
	if g == nil {
		return nil, Member{}, refuse(NotInGuild, "not in a guild")
	}
	m, _ := g.Member(playerID)
	return g, m, nil
}

func (s *State) Touch(p Player, now int64) {
	g := s.GuildOf(p.ID)
	if g == nil {
		return
	}
	index := g.member(p.ID)
	g.Members[index].LastLogin = now
	if p.Name != "" {
		g.Members[index].Name = p.Name
	}
	if p.Level > 0 {
		g.Members[index].Level = p.Level
	}
	g.replaceInactiveLeader(s, now)
}

func (g *Guild) replaceInactiveLeader(s *State, now int64) {
	leader := g.leader()
	if leader < 0 || now-g.Members[leader].LastLogin < InactiveLeaderSeconds {
		return
	}
	successor := -1
	for i, m := range g.Members {
		if i == leader || now-m.LastLogin >= InactiveLeaderSeconds {
			continue
		}
		if successor < 0 || m.Rank > g.Members[successor].Rank ||
			(m.Rank == g.Members[successor].Rank && m.JoinedAt < g.Members[successor].JoinedAt) {
			successor = i
		}
	}
	if successor < 0 {
		return
	}
	g.Members[leader].Rank = RankOfficer
	g.Members[successor].Rank = RankLeader
	g.postEvent(now, "GUILD_LEADER_AUTODEMOTION", plain(g.Members[leader].Name), plain(g.Members[successor].Name))
	s.notify(g.Members[successor].PlayerID, Notice{Kind: NoticeNewLeader, Guild: g.Name, Rank: RankLeader, Time: now})
}

func (s *State) Create(p Player, settings Settings, now int64) (*Guild, error) {
	settings = settings.cleaned()
	if p.Level < LevelUnlock {
		return nil, refuse(FailedLevelRequirement, "guilds unlock at level %d", LevelUnlock)
	}
	if err := s.requireNoGuild(p); err != nil {
		return nil, err
	}
	if !validName(settings.Name) {
		return nil, refuse(InvalidArgument, "name must be %d-%d letters, digits, spaces, ' _ -", MinNameRunes,
			MaxNameRunes)
	}
	if s.GuildNamed(settings.Name) != nil {
		return nil, refuse(NameNotUnique, "a guild called %q exists", settings.Name)
	}
	if err := validateSettings(settings); err != nil {
		return nil, err
	}
	id, err := newGuildID()
	if err != nil {
		return nil, err
	}
	g := &Guild{
		ID: id, Name: settings.Name, Description: settings.Description, MessageOfTheDay: settings.MessageOfTheDay,
		Language: settings.Language, Icon: settings.Icon, Status: settings.Status,
		RequiredLevel: settings.RequiredLevel, CreatedAt: now,
		Members:      []Member{{PlayerID: p.ID, Name: p.Name, Rank: RankLeader, JoinedAt: now, LastLogin: now, Level: p.Level}},
		Applications: []Application{},
		Messages:     []Message{},
	}
	s.withdrawApplications(p, now)
	g.postEvent(now, "GUILD_CREATION", plain(p.Name))
	s.Guilds = append(s.Guilds, g)
	return g, nil
}

func (s *State) Join(p Player, guildID, text string, now int64) error {
	if p.Level < LevelUnlock {
		return refuse(FailedLevelRequirement, "guilds unlock at level %d", LevelUnlock)
	}
	if err := s.requireNoGuild(p); err != nil {
		return err
	}
	g := s.Guild(guildID)
	if g == nil {
		return refuse(NotFound, "no guild %q", guildID)
	}
	if p.Level < g.RequiredLevel {
		return refuse(FailedLevelRequirement, "guild needs level %d", g.RequiredLevel)
	}
	switch g.Status {
	case StatusClosed:
		return refuse(Closed, "guild is closed")
	case StatusOpen:
		if len(g.Members) >= MaxMembers {
			return refuse(Full, "guild is full")
		}
		s.withdrawApplications(p, now)
		g.Members = append(g.Members, Member{
			PlayerID: p.ID, Name: p.Name, Rank: RankMember, JoinedAt: now, LastLogin: now, Level: p.Level,
		})
		g.postEvent(now, "MEMBER_JOINED", plain(p.Name))
		return nil
	}
	if g.application(p.ID) >= 0 {
		return refuse(AlreadyRequestedJoin, "already applied")
	}
	if len(g.Applications) >= MaxApplications {
		return refuse(ApplicationsFull, "guild has %d applications", MaxApplications)
	}
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > MaxApplicationRunes || hasMarkup(text) {
		return refuse(InvalidArgument, "application must be up to %d characters without < or >", MaxApplicationRunes)
	}
	s.withdrawApplications(p, now)
	g.Applications = append(g.Applications, Application{
		PlayerID: p.ID, Name: p.Name, Level: p.Level, RequestedAt: now, Text: text,
	})
	g.postEvent(now, "PLAYER_APPLICATION", plain(p.Name))
	return nil
}

func (s *State) CancelApplication(p Player, guildID string, now int64) error {
	g := s.Guild(guildID)
	if g == nil {
		return refuse(NotFound, "no guild %q", guildID)
	}
	index := g.application(p.ID)
	if index < 0 {
		return refuse(PlayerNotFound, "no application to %q", g.Name)
	}
	g.Applications = slices.Delete(g.Applications, index, index+1)
	g.postEvent(now, "APPLICATION_WITHDRAWN", plain(p.Name))
	return nil
}

func (s *State) officerGuild(actor Player) (*Guild, Member, error) {
	g, m, err := s.membership(actor.ID)
	if err != nil {
		return nil, Member{}, err
	}
	if m.Rank < RankOfficer {
		return nil, Member{}, refuse(InsufficientRank, "officers and the leader only")
	}
	return g, m, nil
}

func (s *State) Accept(actor Player, applicantID string, now int64) error {
	g, m, err := s.officerGuild(actor)
	if err != nil {
		return err
	}
	index := g.application(applicantID)
	if index < 0 {
		return refuse(PlayerNotFound, "no application from %q", applicantID)
	}
	applicant := g.Applications[index]
	if s.GuildOf(applicantID) != nil {
		g.Applications = slices.Delete(g.Applications, index, index+1)
		return refuse(AlreadyInGuild, "%s joined another guild", applicant.Name)
	}
	if len(g.Members) >= MaxMembers {
		return refuse(Full, "guild is full")
	}
	g.Applications = slices.Delete(g.Applications, index, index+1)
	g.Members = append(g.Members, Member{
		PlayerID: applicant.PlayerID, Name: applicant.Name, Rank: RankMember, JoinedAt: now, LastLogin: now,
		Level: applicant.Level,
	})
	g.postEvent(now, "APPLICATION_ACCEPTED", plain(applicant.Name), plain(m.Name))
	s.notify(applicantID, Notice{Kind: NoticeAccepted, Guild: g.Name, Rank: RankMember, Time: now})
	return nil
}

func (s *State) Deny(actor Player, applicantID string, now int64) error {
	g, m, err := s.officerGuild(actor)
	if err != nil {
		return err
	}
	index := g.application(applicantID)
	if index < 0 {
		return refuse(PlayerNotFound, "no application from %q", applicantID)
	}
	applicant := g.Applications[index]
	g.Applications = slices.Delete(g.Applications, index, index+1)
	g.postEvent(now, "APPLICATION_REJECTED", plain(applicant.Name), plain(m.Name))
	s.notify(applicantID, Notice{Kind: NoticeDenied, Guild: g.Name, Rank: RankNotMember, Time: now})
	return nil
}

func (s *State) ChangeRank(actor Player, targetID string, rank Rank, now int64) error {
	g, m, err := s.membership(actor.ID)
	if err != nil {
		return err
	}
	index := g.member(targetID)
	if index < 0 || targetID == actor.ID {
		return refuse(PlayerNotFound, "%q is not another member", targetID)
	}
	target := g.Members[index]
	if rank < RankMember || rank > RankLeader || rank == target.Rank {
		return refuse(InvalidArgument, "cannot change %s to rank %d", target.Name, rank)
	}
	if target.Rank == RankLeader {
		return refuse(LeaderCantBeDemoted, "the leader cannot be demoted")
	}
	if m.Rank != RankLeader {
		return refuse(InsufficientRank, "only the leader changes ranks")
	}
	if rank == RankLeader && target.Rank != RankOfficer {
		return refuse(InvalidArgument, "only an officer can become leader")
	}
	g.Members[index].Rank = rank
	switch {
	case rank == RankLeader:
		g.Members[g.member(actor.ID)].Rank = RankOfficer
		g.postEvent(now, "PROMOTION", plain(target.Name), localized(rankKey(rank)), plain(m.Name))
		s.notify(targetID, Notice{Kind: NoticeNewLeader, Guild: g.Name, Rank: rank, Time: now})
	case rank > target.Rank:
		g.postEvent(now, "PROMOTION", plain(target.Name), localized(rankKey(rank)), plain(m.Name))
		s.notify(targetID, Notice{Kind: NoticePromoted, Guild: g.Name, Rank: rank, Time: now})
	default:
		g.postEvent(now, "DEMOTION", plain(target.Name), localized(rankKey(rank)), plain(m.Name))
		s.notify(targetID, Notice{Kind: NoticeDemoted, Guild: g.Name, Rank: rank, Time: now})
	}
	return nil
}

func (s *State) Kick(actor Player, targetID string, now int64) error {
	g, m, err := s.officerGuild(actor)
	if err != nil {
		return err
	}
	index := g.member(targetID)
	if index < 0 || targetID == actor.ID {
		return refuse(PlayerNotFound, "%q is not another member", targetID)
	}
	target := g.Members[index]
	if target.Rank >= m.Rank {
		return refuse(InsufficientRank, "cannot kick %s", target.Name)
	}
	g.Members = slices.Delete(g.Members, index, index+1)
	g.forget(targetID)
	g.postEvent(now, "KICK", plain(target.Name), plain(m.Name))
	s.notify(targetID, Notice{Kind: NoticeKicked, Guild: g.Name, Rank: RankNotMember, Time: now})
	return nil
}

func (s *State) Leave(p Player, now int64) error {
	g, m, err := s.membership(p.ID)
	if err != nil {
		return err
	}
	if len(g.Members) == 1 {
		s.removeGuild(g.ID)
		return nil
	}
	if m.Rank == RankLeader {
		return refuse(LeaderCantLeave, "promote a new leader first")
	}
	g.Members = slices.Delete(g.Members, g.member(p.ID), g.member(p.ID)+1)
	g.forget(p.ID)
	g.postEvent(now, "MEMBER_LEFT", plain(m.Name))
	return nil
}

func (s *State) ChangeSettings(actor Player, settings Settings, now int64) error {
	g, m, err := s.officerGuild(actor)
	if err != nil {
		return err
	}
	settings.Name = g.Name
	settings = settings.cleaned()
	if err := validateSettings(settings); err != nil {
		return err
	}
	if settings.Status != g.Status && g.Status == StatusApplicationOnly && len(g.Applications) > 0 {
		return refuse(PendingApplications, "answer the %d applications first", len(g.Applications))
	}
	if settings.Description != g.Description {
		g.Description = settings.Description
		g.postEvent(now, "GUILD_DESCRIPTION_CHANGED", plain(m.Name))
	}
	if settings.MessageOfTheDay != g.MessageOfTheDay {
		g.MessageOfTheDay = settings.MessageOfTheDay
		g.postEvent(now, "GUILD_MOTD_CHANGED", plain(m.Name))
	}
	if settings.Icon != g.Icon {
		g.Icon = settings.Icon
		g.postEvent(now, "GUILD_EMBLEM_CHANGED", plain(m.Name))
	}
	if settings.Language != g.Language {
		g.Language = settings.Language
		g.postEvent(now, "GUILD_LANGUAGE_CHANGED", plain(m.Name), localized(LanguageKey(settings.Language)))
	}
	if settings.RequiredLevel != g.RequiredLevel {
		g.RequiredLevel = settings.RequiredLevel
		g.postEvent(now, "GUILD_MINIMUM_LEVEL_CHANGED", plain(m.Name), plain(strconv.Itoa(settings.RequiredLevel)))
	}
	if settings.Status != g.Status {
		g.Status = settings.Status
		g.postEvent(now, "GUILD_STATUS_CHANGED", plain(m.Name), localized(statusKey(settings.Status)))
	}
	return nil
}

func LanguageKey(code string) string {
	return languageKeys[code]
}

func hasMarkup(text string) bool {
	return strings.ContainsAny(text, "<>")
}

func (s *State) Say(p Player, text string, now int64) error {
	g, m, err := s.membership(p.ID)
	if err != nil {
		return err
	}
	line, err := chatLine(text)
	if err != nil {
		return err
	}
	g.post(Message{Time: now, Text: line, PlayerID: p.ID, PlayerName: m.Name, Level: m.Level, Rank: m.Rank})
	return nil
}

func (s *State) Recommended(p Player) []*Guild {
	var found []*Guild
	for _, g := range s.Guilds {
		if g.Status == StatusClosed || len(g.Members) >= MaxMembers || g.RequiredLevel > p.Level ||
			g.member(p.ID) >= 0 {
			continue
		}
		found = append(found, g)
	}
	slices.SortStableFunc(found, func(a, b *Guild) int {
		if order := cmp.Compare(len(b.Members), len(a.Members)); order != 0 {
			return order
		}
		return cmp.Compare(b.LastMessageID(), a.LastMessageID())
	})
	return found[:min(len(found), RecommendedLimit)]
}
