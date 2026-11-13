package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
	"github.com/jealous-sound/hoda-custom-backend/internal/economy"
)

const (
	rivalLevel    = 14
	rivalTrophies = 120
)

func rankedProfile(name string, trophies, pvpWins int) string {
	return fmt.Sprintf(`{"version":9,"account":{"name":%q,"level":%d,"experience":10,"energy":5,`+
		`"energy_max":30,"stamina":8,"stamina_max":8,"gold":10,"gems":7},"current_squad":0,`+
		`"squads":[[7,null,null,null,null]],"roster":{"starters":[],"additions":[{"id":"UNIT_VIVIENNE",`+
		`"instance_id":7,"tier":"II","level":9}]},"pvp":{"trophies":%d,"wins":%d}}`,
		name, rivalLevel, trophies, pvpWins)
}

func (h *harness) storedPlayer(id, name string, trophies, pvpWins int) string {
	h.t.Helper()
	token := h.open(id)
	if _, err := h.profiles.Put(id, 0, json.RawMessage(rankedProfile(name, trophies, pvpWins))); err != nil {
		h.t.Fatal(err)
	}
	return token
}

func (h *harness) ladder(player, token string) []ladderPlayer {
	h.t.Helper()
	response, body := h.do("GET", "/v1/players/"+player+"/pvp/ladder", token, "", "")
	expectStatus(h.t, response, body, http.StatusOK)
	var reply ladderResponse
	if err := json.Unmarshal([]byte(body), &reply); err != nil || reply.Players == nil {
		h.t.Fatalf("ladder reply %s", body)
	}
	return reply.Players
}

func TestLadderListsNamedPlayersWhoFoughtPvpByTrophies(t *testing.T) {
	h := newHarness(t)
	viewer := h.storedPlayer("viewer", "Hawke", 400, 3)
	h.storedPlayer("second", "Isabela", 300, 1)
	h.storedPlayer("first", "Varric", 500, 2)
	h.storedPlayer("unnamed", "", 900, 5)
	h.storedPlayer("newcomer", "Merrill", 0, 0)

	want := []ladderPlayer{
		{ID: "first", Name: "Varric", Level: rivalLevel, Trophies: 500},
		{ID: "second", Name: "Isabela", Level: rivalLevel, Trophies: 300},
	}
	if got := h.ladder("viewer", viewer); !slices.Equal(got, want) {
		t.Fatalf("ladder = %+v, want %+v", got, want)
	}
}

func TestLadderOfAServerWithoutRivalsIsAnEmptyList(t *testing.T) {
	h := newHarness(t)
	viewer := h.storedPlayer("viewer", "Hawke", 400, 3)
	if got := h.ladder("viewer", viewer); len(got) != 0 {
		t.Fatalf("ladder = %+v, want no rivals", got)
	}
}

func TestLadderShowsANewlyRankedPlayerOnceItsListAges(t *testing.T) {
	h := newHarness(t)
	viewer := h.storedPlayer("viewer", "Hawke", 400, 3)
	h.ladder("viewer", viewer)
	h.storedPlayer("late", "Fenris", 700, 1)
	if got := h.ladder("viewer", viewer); len(got) != 0 {
		t.Fatalf("ladder re-read every profile within %v: %+v", ladderMaxAge, got)
	}
	h.pass(ladderMaxAge)
	if got := h.ladder("viewer", viewer); len(got) != 1 || got[0].ID != "late" {
		t.Fatalf("ladder = %+v, want the late player", got)
	}
}

func TestLadderStopsAtItsLimitKeepingTheTop(t *testing.T) {
	h := newHarness(t)
	viewer := h.storedPlayer("viewer", "Hawke", 0, 1)
	extra := 5
	for i := range ladderPlayerLimit + extra {
		h.storedPlayer(fmt.Sprintf("rival-%03d", i), fmt.Sprintf("Rival %d", i), i+1, 1)
	}
	got := h.ladder("viewer", viewer)
	if len(got) != ladderPlayerLimit || got[0].Trophies != ladderPlayerLimit+extra ||
		got[ladderPlayerLimit-1].Trophies != extra+1 {
		t.Fatalf("ladder holds %d players from %d to %d trophies", len(got), got[0].Trophies,
			got[len(got)-1].Trophies)
	}
}

func TestALadderRivalIsFoughtWithoutSharingAGuild(t *testing.T) {
	h := newHarness(t)
	challenger := h.guildPlayer("local", "Hawke", 9, 12000)
	h.storedPlayer("rival", "Isabela", rivalTrophies, 4)
	h.storedPlayer("newcomer", "Merrill", 0, 0)

	response, body := h.do("GET", "/v1/players/local/pvp/ladder/rival", challenger, "", "")
	expectStatus(t, response, body, http.StatusOK)
	var opponent economy.DuelOpponent
	if err := json.Unmarshal([]byte(body), &opponent); err != nil {
		t.Fatal(err)
	}
	if opponent.ID != "rival" || opponent.Name != "Isabela" || opponent.Level != rivalLevel ||
		opponent.Trophies != rivalTrophies || len(opponent.Units) != 1 || opponent.Units[0].ID != "UNIT_VIVIENNE" {
		t.Fatalf("opponent = %+v", opponent)
	}
	for _, notARival := range []string{"newcomer", "local", "nobody", "not%20an%20id"} {
		response, body = h.do("GET", "/v1/players/local/pvp/ladder/"+notARival, challenger, "", "")
		expectStatus(t, response, body, http.StatusNotFound)
	}

	enemy := battle.Side{Units: []battle.Unit{{Slot: 0, ID: "UNIT_VIVIENNE", Tier: "II", Level: 9}}, Runes: []string{}}
	fought := h.battle("local", challenger, 1, "ladder-duel", economy.BattleStartRequest{
		Mode: economy.BattlePvp, Duel: "rival", Enemy: enemy,
	})
	trophies := h.catalog().Pvp.Trophies
	if want := trophies.BaseGain + rivalTrophies/trophies.PointsPerStep; fought.Granted.TrophyDelta != want {
		t.Fatalf("beating a rival with %d trophies gained %d, want %d", rivalTrophies, fought.Granted.TrophyDelta, want)
	}

	for i, notARival := range []string{"newcomer", "local"} {
		start, _ := json.Marshal(map[string]any{
			"request_id": fmt.Sprintf("unranked-duel-%d", i), "action": economy.ActionBattleStart, "mode": "pvp",
			"duel": notARival, "squad": h.storedSquad("local"), "enemy": enemy,
		})
		response, body = h.do("POST", "/v1/players/local/actions", challenger, fmt.Sprint(fought.Revision), string(start))
		expectStatus(t, response, body, http.StatusUnprocessableEntity)
		if !strings.Contains(body, "not on the ladder") {
			t.Fatalf("refusal of %s = %s", notARival, body)
		}
	}
}
