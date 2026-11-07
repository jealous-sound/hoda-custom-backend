package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
)

const createOpenGuild = `{"request_id":"create-guild-1","name":"Champions","description":"Kirkwall",` +
	`"message_of_the_day":"","language":"en","status":0,"icon":3,"required_level":5}`

const duelistProfile = `{"version":9,"account":{"name":"Isabela","level":14,"experience":10,"energy":5,` +
	`"energy_max":30,"stamina":8,"stamina_max":8,"gold":10,"gems":7},"current_squad":0,` +
	`"squads":[[7,null,null,null,null]],"roster":{"starters":[],"additions":[{"id":"UNIT_VIVIENNE",` +
	`"instance_id":7,"tier":"II","level":9}]},"runes":{"items":[{"uuid":3,"def":"rune-power-1",` +
	`"expire_at":1900000000},{"uuid":4,"def":"rune-health-1","expire_at":0}],"squads":[{"slots":[3,4,0]}]},` +
	`"pvp":{"trophies":120}}`

type revisioned struct {
	Revision int64 `json:"revision"`
	Profile  struct {
		Account struct {
			Gold int64 `json:"gold"`
			Gems int64 `json:"gems"`
		} `json:"account"`
	} `json:"profile"`
	Granted struct {
		Gold        int64 `json:"gold"`
		Gems        int64 `json:"gems"`
		Banners     int   `json:"banners"`
		EventRank   int   `json:"event_rank"`
		TrophyDelta int   `json:"trophy_delta"`
	} `json:"granted"`
}

func (h *harness) act(player, token string, revision int64, body string) revisioned {
	h.t.Helper()
	response, reply := h.do("POST", "/v1/players/"+player+"/actions", token, fmt.Sprint(revision), body)
	expectStatus(h.t, response, reply, http.StatusOK)
	var result revisioned
	if err := json.Unmarshal([]byte(reply), &result); err != nil {
		h.t.Fatal(err)
	}
	return result
}

func (h *harness) guildWithAlly(t *testing.T) (leader, ally, guildID string) {
	t.Helper()
	leader = h.guildPlayer("local", "Hawke", 9, 12000)
	response, body := h.do("POST", "/v1/players/local/guilds", leader, "1", createOpenGuild)
	expectStatus(t, response, body, http.StatusOK)
	var created struct {
		Granted guildStateResult `json:"granted"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	ally = h.guildPlayer("ally", "Varric", 9, 10)
	guildID = created.Granted.Guild.ID
	response, body = h.do("POST", "/v1/players/ally/guilds/"+guildID+"/join", ally, "", `{}`)
	expectStatus(t, response, body, http.StatusOK)
	return leader, ally, guildID
}

type onslaughtResult struct {
	Finished bool `json:"finished"`
	Guilds   []struct {
		Name    string `json:"name"`
		Banners int    `json:"banners"`
		Rank    int    `json:"rank"`
	} `json:"guilds"`
	Mine *struct {
		Banners int `json:"banners"`
		Rank    int `json:"rank"`
	} `json:"mine"`
	Members []struct {
		ID      string `json:"id"`
		Banners int    `json:"banners"`
	} `json:"members"`
	MyBanners      int  `json:"my_banners"`
	MinimumBanners int  `json:"minimum_banners"`
	AwardRank      int  `json:"award_rank"`
	Claimed        bool `json:"claimed"`
}

func (h *harness) onslaught(player, token string, cycle int64) onslaughtResult {
	h.t.Helper()
	response, body := h.do("GET", fmt.Sprintf("/v1/players/%s/guild/onslaught?cycle=%d", player, cycle), token, "", "")
	expectStatus(h.t, response, body, http.StatusOK)
	var result onslaughtResult
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		h.t.Fatal(err)
	}
	return result
}

func eventBattle(id int) string {
	return fmt.Sprintf(`{"request_id":"event-battle-%d","action":"battle","mode":"pvp","seed":7,"won":true,`+
		`"event":true,"banners":%d}`, id, 1000)
}

func TestOnslaughtCountsBannersOfMembersFromTheEventStart(t *testing.T) {
	h := newHarness(t)
	shop, err := catalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	events := &shop.Pvp.Events
	cycle := fixedNow.Unix() / events.DurationSeconds
	if !events.IsGuildCycle(cycle) {
		t.Fatalf("cycle %d of the fixed clock should be an Onslaught", cycle)
	}
	*h.now = time.Unix(events.CycleStart(cycle)-60, 0)
	leader, ally, _ := h.guildWithAlly(t)
	*h.now = fixedNow
	late := h.guildPlayer("late", "Fenris", 9, 10)
	response, body := h.do("POST", "/v1/players/late/guilds/"+h.guildIDOf(leader)+"/join", late, "", `{}`)
	expectStatus(t, response, body, http.StatusOK)

	revision := int64(2)
	minimum := events.ForCycle(cycle).GuildMinimumBanners
	banners := 0
	for battle := 0; banners < minimum; battle++ {
		result := h.act("local", leader, revision, eventBattle(battle))
		revision, banners = result.Revision, banners+result.Granted.Banners
	}
	allyResult := h.act("ally", ally, 1, eventBattle(100))
	h.act("late", late, 1, eventBattle(200))

	standings := h.onslaught("local", leader, cycle)
	want := banners + allyResult.Granted.Banners
	if len(standings.Guilds) != 1 || standings.Guilds[0].Banners != want || standings.Mine == nil ||
		standings.Mine.Rank != 1 || standings.MyBanners != banners || standings.Finished || standings.AwardRank != 0 {
		t.Fatalf("standings = %+v, want %d banners", standings, want)
	}
	if len(standings.Members) != 3 || standings.Members[0].ID != "local" {
		t.Fatalf("members = %+v", standings.Members)
	}
	claim := fmt.Sprintf(`{"request_id":"claim-onslaught-1","cycle":%d}`, cycle)
	response, body = h.do("POST", "/v1/players/local/guild/onslaught/claim", leader, fmt.Sprint(revision), claim)
	expectStatus(t, response, body, http.StatusUnprocessableEntity)

	*h.now = fixedNow.Add(time.Duration(events.DurationSeconds) * time.Second)
	finished := h.onslaught("local", leader, cycle)
	if !finished.Finished || finished.AwardRank != 1 || h.onslaught("ally", ally, cycle).AwardRank != 0 {
		t.Fatalf("finished = %+v", finished)
	}
	response, body = h.do("POST", "/v1/players/local/guild/onslaught/claim", leader, fmt.Sprint(revision), claim)
	expectStatus(t, response, body, http.StatusOK)
	var claimed revisioned
	if err := json.Unmarshal([]byte(body), &claimed); err != nil {
		t.Fatal(err)
	}
	reward, _ := events.ForCycle(cycle).GuildReward(1)
	if claimed.Granted.EventRank != 1 || claimed.Granted.Gems != int64(reward.Gems) {
		t.Fatalf("claimed = %+v, want %+v", claimed.Granted, reward)
	}
	again := strings.Replace(claim, "claim-onslaught-1", "claim-onslaught-2", 1)
	response, body = h.do("POST", "/v1/players/local/guild/onslaught/claim", leader, fmt.Sprint(claimed.Revision), again)
	expectStatus(t, response, body, http.StatusUnprocessableEntity)
	if !h.onslaught("local", leader, cycle).Claimed {
		t.Fatal("claim was not remembered")
	}
}

func (h *harness) guildIDOf(token string) string {
	h.t.Helper()
	response, body := h.do("GET", "/v1/players/local/guild", token, "", "")
	expectStatus(h.t, response, body, http.StatusOK)
	return decodeGuildState(h.t, body).Guild.ID
}

func TestOnslaughtStandingsNeedAStartedGuildEvent(t *testing.T) {
	h := newHarness(t)
	token := h.guildPlayer("local", "Hawke", 9, 10)
	cycle := fixedNow.Unix()/259200 + 4
	response, body := h.do("GET", fmt.Sprintf("/v1/players/local/guild/onslaught?cycle=%d", cycle), token, "", "")
	expectStatus(t, response, body, http.StatusUnprocessableEntity)
	response, body = h.do("GET", fmt.Sprintf("/v1/players/local/guild/onslaught?cycle=%d", cycle-5), token, "", "")
	expectStatus(t, response, body, http.StatusUnprocessableEntity)
}

func TestDuelFightsTheMembersCurrentSquad(t *testing.T) {
	h := newHarness(t)
	leader, ally, _ := h.guildWithAlly(t)
	if _, err := h.profiles.Put("ally", 1, json.RawMessage(duelistProfile)); err != nil {
		t.Fatal(err)
	}
	h.guilds(t, "ally", ally)

	response, body := h.do("GET", "/v1/players/local/guild/members/ally/duel", leader, "", "")
	expectStatus(t, response, body, http.StatusOK)
	var opponent struct {
		Name     string `json:"name"`
		Level    int    `json:"level"`
		Trophies int    `json:"trophies"`
		Units    []struct {
			Slot  int    `json:"slot"`
			ID    string `json:"id"`
			Tier  string `json:"tier"`
			Level int    `json:"level"`
		} `json:"units"`
		Runes []string `json:"runes"`
	}
	if err := json.Unmarshal([]byte(body), &opponent); err != nil {
		t.Fatal(err)
	}
	if opponent.Name != "Isabela" || opponent.Level != 14 || opponent.Trophies != 120 || len(opponent.Units) != 1 ||
		opponent.Units[0].ID != "UNIT_VIVIENNE" || opponent.Units[0].Tier != "II" ||
		strings.Join(opponent.Runes, ",") != "rune-power-1" {
		t.Fatalf("opponent = %+v", opponent)
	}
	response, body = h.do("GET", "/v1/players/local/guild/members/local/duel", leader, "", "")
	expectStatus(t, response, body, http.StatusUnprocessableEntity)
	stranger := h.guildPlayer("stranger", "Anders", 9, 10)
	response, body = h.do("GET", "/v1/players/stranger/guild/members/ally/duel", stranger, "", "")
	expectStatus(t, response, body, http.StatusUnprocessableEntity)
	if !strings.Contains(body, `"code":"NOT_IN_GUILD"`) {
		t.Fatalf("body = %s", body)
	}

	shop, err := catalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	rewards := shop.Pvp.Rewards
	duel := h.act("local", leader, 2,
		`{"request_id":"duel-battle-1","action":"battle","mode":"pvp","seed":7,"won":true,"duel":"ally"}`)
	if want := int64(rewards.WinGoldBase + rewards.WinGoldPerLevel*14); duel.Granted.Gold != want {
		t.Fatalf("duel gold = %d, want %d", duel.Granted.Gold, want)
	}
	trophies := shop.Pvp.Trophies
	if want := trophies.BaseGain + 120/trophies.PointsPerStep; duel.Granted.TrophyDelta != want {
		t.Fatalf("duel against 120 trophies gained %d, want %d", duel.Granted.TrophyDelta, want)
	}
	response, body = h.do("POST", "/v1/players/local/actions", leader, fmt.Sprint(duel.Revision),
		`{"request_id":"duel-battle-2","action":"battle","mode":"pvp","seed":7,"won":true,"duel":"ally","event":true}`)
	expectStatus(t, response, body, http.StatusUnprocessableEntity)
	response, body = h.do("POST", "/v1/players/stranger/actions", stranger, "1",
		`{"request_id":"duel-battle-3","action":"battle","mode":"pvp","seed":7,"won":true,"duel":"ally"}`)
	expectStatus(t, response, body, http.StatusUnprocessableEntity)
}

func (h *harness) guilds(t *testing.T, player, token string) {
	t.Helper()
	response, body := h.do("GET", "/v1/players/"+player+"/guild", token, "", "")
	expectStatus(t, response, body, http.StatusOK)
}

func TestReportsAreStoredWithTheOriginalDescriptions(t *testing.T) {
	h := newHarness(t)
	leader := h.guildPlayer("local", "Hawke", 9, 12000)
	response, body := h.do("POST", "/v1/players/local/guilds", leader, "1", createChampions)
	expectStatus(t, response, body, http.StatusOK)
	guildID := h.guildIDOf(leader)
	applicant := h.guildPlayer("applicant", "Sebastian", 9, 10)
	response, body = h.do("POST", "/v1/players/applicant/guilds/"+guildID+"/join", applicant, "",
		`{"application_text":"let me in"}`)
	expectStatus(t, response, body, http.StatusOK)

	response, body = h.do("POST", "/v1/players/applicant/reports", applicant, "",
		`{"kind":"guild","abuse_type":"NONE","guild_id":"`+guildID+`"}`)
	expectStatus(t, response, body, http.StatusUnprocessableEntity)
	response, body = h.do("POST", "/v1/players/applicant/reports", applicant, "",
		`{"kind":"guild","abuse_type":"HARRASSMENT","guild_id":"`+guildID+`"}`)
	expectStatus(t, response, body, http.StatusOK)
	response, body = h.do("POST", "/v1/players/applicant/reports", applicant, "",
		`{"kind":"application","abuse_type":"OTHER","applicant_id":"applicant"}`)
	expectStatus(t, response, body, http.StatusUnprocessableEntity)
	response, body = h.do("POST", "/v1/players/local/reports", leader, "",
		`{"kind":"application","abuse_type":"SEXUAL","applicant_id":"applicant"}`)
	expectStatus(t, response, body, http.StatusOK)

	stored, err := os.ReadFile(filepath.Join(h.dir, "reports.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(stored)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"Guild Name: Champions, Id: `+guildID+`"`) ||
		!strings.Contains(lines[0], `"reporter":"applicant"`) || !strings.Contains(lines[1], `"issue_type":"SEXUAL"`) ||
		!strings.Contains(lines[1], "Player Name: Sebastian, Id: applicant, Application Text: let me in") {
		t.Fatalf("reports = %s", stored)
	}
}
