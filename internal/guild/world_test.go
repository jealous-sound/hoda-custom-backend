package guild

import (
	"strings"
	"testing"
)

func TestWorldChatIsOpenToPlayersWithoutAGuild(t *testing.T) {
	s, _ := guildWithOfficer(t)
	wanderer := Player{ID: "wanderer", Name: "Isabela", Level: WorldSpeakerLevel}
	must(t, s.SayToWorld(wanderer, "  anyone for the arena?  ", start))
	must(t, s.SayToWorld(leader, "Champions are recruiting", start+1))
	lines := s.World.MessagesAfter(0)
	if len(lines) != 2 || s.World.LastMessageID() != 2 {
		t.Fatalf("lines = %+v", lines)
	}
	if lines[0].Text != "anyone for the arena?" || lines[0].GuildName != "" || lines[0].Rank != RankNotMember {
		t.Fatalf("guildless line = %+v", lines[0])
	}
	if lines[1].GuildName != "Champions" || lines[1].Rank != RankLeader || lines[1].PlayerName != leader.Name {
		t.Fatalf("guild line = %+v", lines[1])
	}
	if after := s.World.MessagesAfter(1); len(after) != 1 || after[0].ID != 2 {
		t.Fatalf("after 1 = %+v", after)
	}
}

func TestWorldChatRefusesLowLevelsAndBadLines(t *testing.T) {
	s := newState()
	expectCode(t, s.SayToWorld(rookie, "hello", start), ChatLevelTooLow)
	expectCode(t, s.SayToWorld(member, "   ", start), InvalidArgument)
	expectCode(t, s.SayToWorld(member, "<b>loud</b>", start), InvalidArgument)
	expectCode(t, s.SayToWorld(member, strings.Repeat("a", MaxChatRunes+1), start), ChatMessageTooLong)
	if s.World.LastMessageID() != 0 {
		t.Fatalf("refused lines were stored: %+v", s.World.Messages)
	}
}

func TestWorldChatKeepsOnlyRecentHistory(t *testing.T) {
	s := newState()
	for i := 0; i < WorldHistoryLimit+5; i++ {
		must(t, s.SayToWorld(member, "line", start+int64(i)))
	}
	if len(s.World.Messages) != WorldHistoryLimit || s.World.Messages[0].ID != 6 {
		t.Fatalf("kept %d lines from id %d", len(s.World.Messages), s.World.Messages[0].ID)
	}
	if copied := s.clone(); copied.World.LastMessageID() != s.World.LastMessageID() {
		t.Fatalf("clone lost world chat")
	}
}
