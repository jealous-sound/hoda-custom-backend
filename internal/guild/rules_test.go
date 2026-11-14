package guild

import (
	"errors"
	"testing"
)

const start = int64(1_800_000_000)

var (
	leader  = Player{ID: "leader", Name: "Hawke", Level: 20}
	officer = Player{ID: "officer", Name: "Varric", Level: 18}
	member  = Player{ID: "member", Name: "Merrill", Level: 12}
	rookie  = Player{ID: "rookie", Name: "Sandal", Level: 3}
)

func openSettings(name string) Settings {
	return Settings{Name: name, Description: "Kirkwall", Language: "en", Status: StatusOpen, Icon: 7,
		RequiredLevel: LevelUnlock}
}

func expectCode(t *testing.T, err error, code Code) {
	t.Helper()
	var refusal *Refusal
	if !errors.As(err, &refusal) || refusal.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func guildWithOfficer(t *testing.T) (*State, *Guild) {
	t.Helper()
	s := newState()
	g, err := s.Create(leader, openSettings("Champions"), start)
	must(t, err)
	must(t, s.Join(officer, g.ID, "", start))
	must(t, s.Join(member, g.ID, "", start))
	must(t, s.ChangeRank(leader, officer.ID, RankOfficer, start))
	return s, g
}

func TestCreateFollowsTheOriginalLimits(t *testing.T) {
	s := newState()
	_, err := s.Create(rookie, openSettings("Champions"), start)
	expectCode(t, err, FailedLevelRequirement)
	_, err = s.Create(leader, openSettings("Abc"), start)
	expectCode(t, err, InvalidArgument)
	_, err = s.Create(leader, openSettings("Nineteen characters"), start)
	expectCode(t, err, InvalidArgument)
	bad := openSettings("Champions")
	bad.Icon = 0
	_, err = s.Create(leader, bad, start)
	expectCode(t, err, InvalidArgument)
	g, err := s.Create(leader, openSettings("Champions"), start)
	must(t, err)
	if g.Members[0].Rank != RankLeader || g.Messages[0].Title != "GUILD_CHAT_EVENT_GUILD_CREATION_TITLE" {
		t.Fatalf("unexpected guild %+v", g)
	}
	_, err = s.Create(officer, openSettings("champions"), start)
	expectCode(t, err, NameNotUnique)
	_, err = s.Create(leader, openSettings("Second"), start)
	expectCode(t, err, AlreadyInGuild)
}

func TestApplicationOnlyGuildsQueueOneApplicationPerPlayer(t *testing.T) {
	s := newState()
	settings := openSettings("Wardens")
	settings.Status = StatusApplicationOnly
	first, err := s.Create(leader, settings, start)
	must(t, err)
	settings.Name = "Templars"
	second, err := s.Create(officer, settings, start)
	must(t, err)
	must(t, s.Join(member, first.ID, "let me in", start))
	expectCode(t, s.Join(member, first.ID, "", start), AlreadyRequestedJoin)
	must(t, s.Join(member, second.ID, "", start))
	if len(first.Applications) != 0 || len(second.Applications) != 1 {
		t.Fatalf("applications not moved: %d, %d", len(first.Applications), len(second.Applications))
	}
	if last := first.Messages[len(first.Messages)-1]; last.Text != "GUILD_CHAT_EVENT_APPLICATION_WITHDRAWN_MESSAGE" {
		t.Fatalf("withdrawal not announced: %+v", last)
	}
	expectCode(t, s.Accept(member, member.ID, start), NotInGuild)
	must(t, s.Accept(officer, member.ID, start))
	if s.GuildOf(member.ID) != second {
		t.Fatal("accepted applicant is not a member")
	}
	notices := s.TakeNotices(member.ID)
	if len(notices) != 1 || notices[0].Kind != NoticeAccepted {
		t.Fatalf("want an accepted notice, got %+v", notices)
	}
}

func TestClosedAndFullGuildsRefuseMembers(t *testing.T) {
	s := newState()
	settings := openSettings("Circle")
	settings.Status = StatusClosed
	closed, err := s.Create(leader, settings, start)
	must(t, err)
	expectCode(t, s.Join(member, closed.ID, "", start), Closed)
	open, err := s.Create(officer, openSettings("Coterie"), start)
	must(t, err)
	for i := len(open.Members); i < MaxMembers; i++ {
		open.Members = append(open.Members, Member{PlayerID: string(rune('a' + i)), Rank: RankMember})
	}
	expectCode(t, s.Join(member, open.ID, "", start), Full)
	expectCode(t, s.Join(rookie, "missing", "", start), FailedLevelRequirement)
}

func TestOnlyTheLeaderChangesRanks(t *testing.T) {
	s, g := guildWithOfficer(t)
	expectCode(t, s.ChangeRank(officer, member.ID, RankOfficer, start), InsufficientRank)
	expectCode(t, s.ChangeRank(leader, member.ID, RankLeader, start), InvalidArgument)
	must(t, s.ChangeRank(leader, officer.ID, RankLeader, start))
	newLeader, _ := g.Member(officer.ID)
	oldLeader, _ := g.Member(leader.ID)
	if newLeader.Rank != RankLeader || oldLeader.Rank != RankOfficer {
		t.Fatalf("leadership not transferred: %+v %+v", newLeader, oldLeader)
	}
	expectCode(t, s.ChangeRank(leader, member.ID, RankOfficer, start), InsufficientRank)
	must(t, s.ChangeRank(officer, leader.ID, RankMember, start))
	expectCode(t, s.ChangeRank(officer, leader.ID, RankLeader, start), InvalidArgument)
	expectCode(t, s.ChangeRank(officer, officer.ID, RankMember, start), PlayerNotFound)
}

func TestKickNeedsAHigherRank(t *testing.T) {
	s, _ := guildWithOfficer(t)
	expectCode(t, s.Kick(member, officer.ID, start), InsufficientRank)
	expectCode(t, s.Kick(officer, leader.ID, start), InsufficientRank)
	must(t, s.Kick(officer, member.ID, start))
	if s.GuildOf(member.ID) != nil {
		t.Fatal("kicked member still in guild")
	}
	if notices := s.TakeNotices(member.ID); len(notices) != 1 || notices[0].Kind != NoticeKicked {
		t.Fatalf("want a kicked notice, got %+v", notices)
	}
}

func TestLeaderLeavesOnlyAnEmptyGuild(t *testing.T) {
	s, g := guildWithOfficer(t)
	expectCode(t, s.Leave(leader, start), LeaderCantLeave)
	must(t, s.Leave(member, start))
	must(t, s.Leave(officer, start))
	must(t, s.Leave(leader, start))
	if s.Guild(g.ID) != nil {
		t.Fatal("empty guild was kept")
	}
}

func TestIdleLeaderIsReplacedByTheMostSeniorActiveMember(t *testing.T) {
	s, g := guildWithOfficer(t)
	later := start + InactiveLeaderSeconds
	s.Touch(officer, later-1)
	s.Touch(member, later)
	newLeader, _ := g.Member(officer.ID)
	if newLeader.Rank != RankLeader {
		t.Fatalf("idle leader kept the guild: %+v", g.Members)
	}
	last := g.Messages[len(g.Messages)-1]
	if last.Title != "GUILD_CHAT_EVENT_GUILD_LEADER_AUTODEMOTION_TITLE" || last.Params[0].Text != leader.Name {
		t.Fatalf("auto-demotion not announced: %+v", last)
	}
}

func TestSettingsAnnounceEachChange(t *testing.T) {
	s, g := guildWithOfficer(t)
	settings := openSettings("Ignored")
	settings.Language = "ru"
	settings.RequiredLevel = 10
	settings.Status = StatusApplicationOnly
	expectCode(t, s.ChangeSettings(member, settings, start), InsufficientRank)
	before := len(g.Messages)
	must(t, s.ChangeSettings(officer, settings, start))
	if g.Name != "Champions" || g.Language != "ru" || g.RequiredLevel != 10 || len(g.Messages) != before+3 {
		t.Fatalf("settings not applied: %+v", g)
	}
	if param := g.Messages[before].Params[1]; param.Text != "LANGUAGE_RUSSIAN" || !param.LocKey {
		t.Fatalf("language event param %+v", param)
	}
	must(t, s.Join(Player{ID: "applicant", Name: "Bethany", Level: 30}, g.ID, "", start))
	settings.Status = StatusOpen
	expectCode(t, s.ChangeSettings(leader, settings, start), PendingApplications)
}

func TestChatKeepsABoundedHistory(t *testing.T) {
	s, g := guildWithOfficer(t)
	expectCode(t, s.Say(rookie, "hello", start), NotInGuild)
	expectCode(t, s.Say(member, string(make([]rune, MaxChatRunes+1)), start), ChatMessageTooLong)
	expectCode(t, s.Say(member, "<color=red>hi</color>", start), InvalidArgument)
	seen := g.LastMessageID()
	must(t, s.Say(member, "  hello  ", start))
	fresh := g.MessagesAfter(seen)
	if len(fresh) != 1 || fresh[0].Text != "hello" || fresh[0].PlayerName != member.Name {
		t.Fatalf("unexpected chat %+v", fresh)
	}
	if officerUnread := g.Unread(officer.ID); officerUnread == 0 || g.Unread(member.ID) >= officerUnread {
		t.Fatalf("own messages count as unread: officer %d member %d", officerUnread, g.Unread(member.ID))
	}
	g.MarkRead(officer.ID, g.LastMessageID()+10)
	if g.Unread(officer.ID) != 0 || g.ReadThrough[officer.ID] != g.LastMessageID() {
		t.Fatalf("read marker %d unread %d", g.ReadThrough[officer.ID], g.Unread(officer.ID))
	}
	for range MessageHistoryLimit + 5 {
		must(t, s.Say(member, "spam", start))
	}
	if len(g.Messages) != MessageHistoryLimit || g.MessagesAfter(0)[0].ID <= seen {
		t.Fatalf("history not bounded: %d", len(g.Messages))
	}
}

func TestRecommendedSkipsClosedFullAndTooHighGuilds(t *testing.T) {
	s := newState()
	_, err := s.Create(leader, openSettings("Champions"), start)
	must(t, err)
	high := openSettings("Veterans")
	high.RequiredLevel = 50
	_, err = s.Create(officer, high, start)
	must(t, err)
	found := s.Recommended(member)
	if len(found) != 1 || found[0].Name != "Champions" {
		t.Fatalf("unexpected recommendations %+v", found)
	}
}

func TestRemovedLeaderHandsTheGuildToTheMostSeniorMember(t *testing.T) {
	s, g := guildWithOfficer(t)
	s.TakeNotices(officer.ID)
	s.RemovePlayer(leader.ID, start)
	heir, _ := g.Member(officer.ID)
	if _, stayed := g.Member(leader.ID); stayed || heir.Rank != RankLeader || len(g.Members) != 2 {
		t.Fatalf("members after the leader was removed: %+v", g.Members)
	}
	if notices := s.TakeNotices(officer.ID); len(notices) != 1 || notices[0].Kind != NoticeNewLeader {
		t.Fatalf("want a new leader notice, got %+v", notices)
	}
	last := g.Messages[len(g.Messages)-1]
	if last.Title != "GUILD_CHAT_EVENT_MEMBER_LEFT_TITLE" || last.Params[0].Text != leader.Name {
		t.Fatalf("departure not announced: %+v", last)
	}
}

func TestRemovedPlayerLeavesNoApplicationsNoticesOrEmptyGuild(t *testing.T) {
	s, g := guildWithOfficer(t)
	settings := openSettings("Wardens")
	settings.Status = StatusApplicationOnly
	wardens, err := s.Create(Player{ID: "warden", Name: "Alistair", Level: 20}, settings, start)
	must(t, err)
	must(t, s.Kick(leader, member.ID, start))
	must(t, s.Join(member, wardens.ID, "let me in", start))
	s.RemovePlayer(member.ID, start)
	if len(wardens.Applications) != 0 || len(s.TakeNotices(member.ID)) != 0 {
		t.Fatalf("removed player left applications %+v", wardens.Applications)
	}
	s.RemovePlayer("warden", start)
	if s.Guild(wardens.ID) != nil || s.Guild(g.ID) == nil {
		t.Fatal("removing the only member should close that guild alone")
	}
	s.RemovePlayer("stranger", start)
}
