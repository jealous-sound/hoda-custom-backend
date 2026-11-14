package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jealous-sound/hoda-custom-backend/internal/economy"
	"github.com/jealous-sound/hoda-custom-backend/internal/guild"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const (
	firstRank          = 1
	sixthRank          = 6
	tenthRank          = 10
	xpBelowTheFirstRun = 10
	rankField          = `"renown_rank"`
)

func atExperience(xp int64) func(doc map[string]any) {
	return func(doc map[string]any) { doc["account"].(map[string]any)["experience"] = xp }
}

func (h *harness) rankedAt(id, name string, trophies int, xp int64) string {
	h.t.Helper()
	return h.storedAs(id, editedProfile(h.t, rankedProfile(name, trophies, 1), atExperience(xp)))
}

func (h *harness) guildPlayerAt(id, name string, xp int64) string {
	h.t.Helper()
	return h.storedAs(id, editedProfile(h.t, guildProfile(name, 9, 12000), atExperience(xp)))
}

func (h *harness) earn(player string, xp int64) {
	h.t.Helper()
	_, err := h.profiles.Transact(player, func(record *profile.Record) error {
		record.Profile = json.RawMessage(editedProfile(h.t, string(record.Profile), atExperience(xp)))
		return nil
	})
	if err != nil {
		h.t.Fatal(err)
	}
}

type rankedGuildView struct {
	Guild struct {
		ID              string              `json:"id"`
		Members         []guild.Member      `json:"members"`
		PendingRequests []guild.Application `json:"pending_requests"`
		LastMessageID   int64               `json:"last_message_id"`
	} `json:"guild"`
}

func (h *harness) guildSeenBy(player, token string) (rankedGuildView, string) {
	h.t.Helper()
	response, body := h.do("GET", "/v1/players/"+player+"/guild", token, "", "")
	expectStatus(h.t, response, body, http.StatusOK)
	var view rankedGuildView
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		h.t.Fatalf("%v: %s", err, body)
	}
	return view, body
}

func renownRanksOf(members []guild.Member) map[string]int {
	ranks := map[string]int{}
	for _, member := range members {
		ranks[member.PlayerID] = member.RenownRank
	}
	return ranks
}

func TestLadderRowsCarryEachRivalsRenownRank(t *testing.T) {
	h := newHarness(t)
	viewer := h.rankedAt("viewer", "Hawke", 400, sixthRankXP)
	h.rankedAt("veteran", "Varric", 500, levelCapXP)
	h.rankedAt("rising", "Isabela", 300, firstRankXP)
	h.rankedAt("fresh", "Merrill", 200, xpBelowTheFirstRun)

	rows := h.ladder("viewer", viewer)
	if len(rows) != 3 || rows[0].RenownRank != tenthRank || rows[1].RenownRank != firstRank || rows[2].RenownRank != 0 {
		t.Fatalf("ladder = %+v, want renown ranks %d, %d and none", rows, tenthRank, firstRank)
	}
	response, body := h.do("GET", "/v1/players/viewer/pvp/ladder", viewer, "", "")
	expectStatus(t, response, body, http.StatusOK)
	if strings.Count(body, rankField) != 2 {
		t.Fatalf("a rival below the first rank is sent with a renown rank: %s", body)
	}
}

func TestEventLadderRowsCarryEachRivalsRenownRank(t *testing.T) {
	h := newHarness(t)
	running := h.currentEvent()
	viewer := h.eventPlayer("viewer", "Hawke", profile.PvpEventState{Cycle: running, Banners: 300})
	h.eventPlayer("veteran", "Varric", profile.PvpEventState{Cycle: running, Banners: 900})
	h.eventPlayer("fresh", "Merrill", profile.PvpEventState{Cycle: running, Banners: 200})
	h.earn("veteran", sixthRankXP)

	want := []eventLadderPlayer{
		{ID: "veteran", Name: "Varric", Banners: 900, RenownRank: sixthRank},
		{ID: "fresh", Name: "Merrill", Banners: 200},
	}
	if reply := h.eventLadder("viewer", viewer); len(reply.Players) != 2 || reply.Players[0] != want[0] ||
		reply.Players[1] != want[1] {
		t.Fatalf("event ladder = %+v, want %+v", reply.Players, want)
	}
	response, body := h.do("GET", "/v1/players/viewer/pvp/event/ladder", viewer, "", "")
	expectStatus(t, response, body, http.StatusOK)
	if strings.Count(body, rankField) != 1 {
		t.Fatalf("a rival below the first rank is sent with a renown rank: %s", body)
	}
}

func (h *harness) expectDuelRank(token, route string, want int) {
	h.t.Helper()
	response, body := h.do("GET", route, token, "", "")
	expectStatus(h.t, response, body, http.StatusOK)
	var opponent economy.DuelOpponent
	if err := json.Unmarshal([]byte(body), &opponent); err != nil {
		h.t.Fatal(err)
	}
	if opponent.RenownRank != want || strings.Contains(body, rankField) != (want > 0) {
		h.t.Fatalf("%s answered %s, want renown rank %d", route, body, want)
	}
}

func TestDuelPayloadsCarryTheDefendersRenownRank(t *testing.T) {
	h := newHarness(t)
	leader, ally, _ := h.guildWithAlly(t)
	duel := "/v1/players/local/guild/members/ally/duel"
	if _, err := h.profiles.Put("ally", 1, json.RawMessage(duelistProfile)); err != nil {
		t.Fatal(err)
	}
	h.guilds(t, "ally", ally)
	h.expectDuelRank(leader, duel, 0)
	h.earn("ally", sixthRankXP)
	h.expectDuelRank(leader, duel, sixthRank)

	h.rankedAt("rival", "Isabela", rivalTrophies, levelCapXP)
	h.storedPlayer("plain", "Merrill", rivalTrophies, 2)
	h.expectDuelRank(leader, "/v1/players/local/pvp/ladder/rival", tenthRank)
	h.expectDuelRank(leader, "/v1/players/local/pvp/ladder/plain", 0)
}

func TestGuildListsAndChatCarryEachPlayersRenownRank(t *testing.T) {
	h := newHarness(t)
	leader := h.guildPlayerAt("local", "Hawke", levelCapXP)
	response, body := h.do("POST", "/v1/players/local/guilds", leader, "1", createChampions)
	expectStatus(t, response, body, http.StatusOK)
	founded, _ := h.guildSeenBy("local", leader)
	if len(founded.Guild.Members) != 1 || founded.Guild.Members[0].RenownRank != tenthRank {
		t.Fatalf("the founder is listed as %+v, want renown rank %d", founded.Guild.Members, tenthRank)
	}

	applicant := h.guildPlayerAt("applicant", "Merrill", firstRankXP)
	plain := h.guildPlayerAt("plain", "Sandal", xpBelowTheFirstRun)
	for player, token := range map[string]string{"applicant": applicant, "plain": plain} {
		response, body = h.do("POST", "/v1/players/"+player+"/guilds/"+founded.Guild.ID+"/join", token, "", `{}`)
		expectStatus(t, response, body, http.StatusOK)
	}
	applied, body := h.guildSeenBy("local", leader)
	requested := map[string]int{}
	for _, request := range applied.Guild.PendingRequests {
		requested[request.PlayerID] = request.RenownRank
	}
	if len(requested) != 2 || requested["applicant"] != firstRank || requested["plain"] != 0 ||
		strings.Count(body, rankField) != 2 {
		t.Fatalf("applications are listed as %s, want renown rank %d on one of them", body, firstRank)
	}

	for _, accepted := range []string{"applicant", "plain"} {
		response, body = h.do("POST", "/v1/players/local/guild/applications/"+accepted+"/accept", leader, "", "")
		expectStatus(t, response, body, http.StatusOK)
	}
	joined, body := h.guildSeenBy("plain", plain)
	if ranks := renownRanksOf(joined.Guild.Members); len(ranks) != 3 || ranks["local"] != tenthRank ||
		ranks["applicant"] != firstRank || ranks["plain"] != 0 || strings.Count(body, rankField) != 2 {
		t.Fatalf("members are listed as %s, want renown ranks %d, %d and none", body, tenthRank, firstRank)
	}

	h.earn("applicant", sixthRankXP)
	response, body = h.do("POST", "/v1/players/applicant/guild/messages", applicant, "",
		fmt.Sprintf(`{"after":%d,"text":"Hello, Hawke","renown_rank":40}`, joined.Guild.LastMessageID))
	expectStatus(t, response, body, http.StatusOK)
	var chat guildMessagesReply
	if err := json.Unmarshal([]byte(body), &chat); err != nil {
		t.Fatal(err)
	}
	if len(chat.Messages) != 1 || chat.Messages[0].RenownRank != sixthRank {
		t.Fatalf("chat = %s, want the speaker's renown rank %d", body, sixthRank)
	}
	response, body = h.do("POST", "/v1/players/plain/guild/messages", plain, "",
		fmt.Sprintf(`{"after":%d,"text":"Enchantment?"}`, chat.LastMessageID))
	expectStatus(t, response, body, http.StatusOK)
	if strings.Contains(body, rankField) {
		t.Fatalf("a speaker below the first rank is sent with a renown rank: %s", body)
	}
	promoted, _ := h.guildSeenBy("local", leader)
	if ranks := renownRanksOf(promoted.Guild.Members); ranks["applicant"] != sixthRank {
		t.Fatalf("after earning a rank the member is listed as %+v", promoted.Guild.Members)
	}
}

func TestWorldChatLinesCarryTheSpeakersRenownRank(t *testing.T) {
	h := newHarness(t)
	veteran := h.guildPlayerAt("veteran", "Varric", sixthRankXP)
	fresh := h.guildPlayerAt("fresh", "Merrill", xpBelowTheFirstRun)

	response, _, body := h.world("veteran", veteran, `{"text":"anyone for the arena?"}`)
	expectStatus(t, response, body, http.StatusOK)
	response, _, body = h.world("fresh", fresh, `{"text":"me","renown_rank":40}`)
	expectStatus(t, response, body, http.StatusOK)

	var lines struct {
		Messages []guild.Message `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &lines); err != nil {
		t.Fatal(err)
	}
	if len(lines.Messages) != 2 || lines.Messages[0].RenownRank != sixthRank || lines.Messages[1].RenownRank != 0 ||
		strings.Count(body, rankField) != 1 {
		t.Fatalf("world chat = %s, want renown rank %d on the first line only", body, sixthRank)
	}
}

func TestAnUploadCannotRaiseTheRenownRankOthersSee(t *testing.T) {
	h := newHarness(t)
	viewer := h.rankedAt("viewer", "Hawke", 400, xpBelowTheFirstRun)
	forger := h.rankedAt("forger", "Bodahn", 500, firstRankXP)
	forged := editedProfile(t, rankedProfile("Bodahn", 500, 1), func(doc map[string]any) {
		atExperience(levelCapXP)(doc)
		doc["renown_rank"] = 40
		doc["account"].(map[string]any)["renown_rank"] = 40
	})
	response, body := h.do("PUT", "/v1/players/forger/profile", forger, "1", forged)
	expectStatus(t, response, body, http.StatusOK)

	h.pass(ladderMaxAge)
	if rows := h.ladder("viewer", viewer); len(rows) != 1 || rows[0].RenownRank != firstRank {
		t.Fatalf("after a forged upload the ladder lists %+v, want renown rank %d", rows, firstRank)
	}
	h.expectDuelRank(viewer, "/v1/players/viewer/pvp/ladder/forger", firstRank)
	response, said, body := h.world("forger", forger, `{"text":"look at my star"}`)
	expectStatus(t, response, body, http.StatusOK)
	if len(said.Messages) != 1 || !strings.Contains(body, fmt.Sprintf(`%s:%d`, rankField, firstRank)) {
		t.Fatalf("after a forged upload world chat sends %s, want renown rank %d", body, firstRank)
	}
}
