package guild

import "testing"

func TestOnslaughtRanksGuildsBySummedMemberBanners(t *testing.T) {
	s, g := guildWithOfficer(t)
	rival, err := s.Create(Player{ID: "rival", Name: "Anders", Level: 9}, openSettings("Mages"), start)
	must(t, err)
	eventStart := start + 1
	for _, credit := range []struct {
		player  Player
		banners int
	}{{leader, 40}, {officer, 30}, {member, 0}, {Player{ID: "rival", Name: "Anders"}, 60}} {
		s.CreditOnslaught(credit.player, 9, eventStart, credit.banners)
	}
	standings := s.OnslaughtStandings(9)
	if len(standings) != 2 || standings[0].GuildID != g.ID || standings[0].Banners != 70 ||
		standings[1].GuildID != rival.ID || standings[1].Rank != 2 {
		t.Fatalf("standings = %+v", standings)
	}
	members := s.OnslaughtMembers(member.ID, 9)
	if len(members) != 3 || members[0].PlayerID != leader.ID || members[2].Banners != 0 {
		t.Fatalf("members = %+v", members)
	}
}

func TestOnslaughtIgnoresMembersWhoJoinedAfterTheStart(t *testing.T) {
	s, _ := guildWithOfficer(t)
	if s.CreditOnslaught(member, 9, start-1, 25) {
		t.Fatal("a member who joined after the start was credited")
	}
	if s.CreditOnslaught(Player{ID: "nobody"}, 9, start, 25) || len(s.OnslaughtStandings(9)) != 0 {
		t.Fatal("a player without a guild was credited")
	}
}

func TestOnslaughtBannersStayWithTheGuildTheyWereEarnedIn(t *testing.T) {
	s, g := guildWithOfficer(t)
	s.CreditOnslaught(member, 9, start, 25)
	must(t, s.Leave(member, start+10))
	mine, found := s.OnslaughtGuild(member.ID, 9)
	if !found || mine.GuildID != g.ID || s.OnslaughtBanners(member.ID, 9) != 25 {
		t.Fatalf("guild = %+v found %v", mine, found)
	}
	if !s.ClaimOnslaught(member.ID, 9) || s.ClaimOnslaught(member.ID, 9) || s.ClaimOnslaught(officer.ID, 9) {
		t.Fatal("a contributor claims once and others not at all")
	}
}

func TestOnslaughtKeepsTheRecentEvents(t *testing.T) {
	s, _ := guildWithOfficer(t)
	for cycle := int64(0); cycle < OnslaughtsKept+2; cycle++ {
		s.CreditOnslaught(leader, cycle*4, start, 10)
	}
	if len(s.Onslaughts) != OnslaughtsKept || s.Onslaughts[0].Cycle != 8 {
		t.Fatalf("kept %d onslaughts from cycle %d", len(s.Onslaughts), s.Onslaughts[0].Cycle)
	}
	copied := s.clone()
	copied.Onslaughts[0].Entries[0].Members[leader.ID] = 99
	if s.OnslaughtBanners(leader.ID, 8) != 10 {
		t.Fatal("a clone shares onslaught tallies")
	}
}
