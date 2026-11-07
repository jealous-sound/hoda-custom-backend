package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/economy"
)

const (
	guildHeroID       = "UNIT_ARIQUN"
	guildHeroTier     = "IV"
	guildHeroLevel    = 30
	battleSeedSearch  = 4000
	guildHeroInstance = 1
)

func guildSquad() battle.Side {
	return battle.Side{Units: []battle.Unit{{
		Slot: 0, InstanceID: guildHeroInstance, ID: guildHeroID, Tier: guildHeroTier, Level: guildHeroLevel,
		Gear: make([]string, 5),
	}}, Runes: []string{}}
}

func (h *harness) eventStart(level int, seed uint32) economy.BattleStartRequest {
	h.t.Helper()
	data := h.catalog().Battle
	now := h.now.Unix()
	enemy, err := data.PvpRoster(data.PvpEventOpponent(level, 0, 0, now, seed))
	if err != nil {
		h.t.Fatal(err)
	}
	return economy.BattleStartRequest{
		Mode: economy.BattlePvp, Event: true, Squad: guildSquad(), Enemy: enemy,
		Changer:  data.PvpEventChanger(data.PvpEventCycle(now)),
		Opponent: economy.PvpOpponentRequest{Kind: economy.OpponentEvent, Seed: seed},
	}
}

func (h *harness) catalog() *catalog.Catalog {
	h.t.Helper()
	c, err := catalog.Default()
	if err != nil {
		h.t.Fatal(err)
	}
	return c
}

func (h *harness) winningSeed(request economy.BattleStartRequest) (uint32, battle.Outcome) {
	h.t.Helper()
	for seed := uint32(1); seed < battleSeedSearch; seed++ {
		changer := request.Changer
		if changer != nil && len(changer.Effects) == 0 {
			changer = nil
		}
		outcome, err := h.catalog().Battle.Run(battle.Replay{
			Seed: seed, Player: request.Squad, Enemy: request.Enemy, Changer: changer,
		})
		if err != nil {
			h.t.Fatal(err)
		}
		if outcome.Won {
			return seed, outcome
		}
	}
	h.t.Fatal("no winning seed")
	return 0, battle.Outcome{}
}

func (h *harness) storedSquad(player string) battle.Side {
	h.t.Helper()
	record, err := h.profiles.Get(player)
	if err != nil {
		h.t.Fatal(err)
	}
	var stored struct {
		Roster struct {
			Additions []battle.Unit `json:"additions"`
		} `json:"roster"`
	}
	if err := json.Unmarshal(record.Profile, &stored); err != nil {
		h.t.Fatal(err)
	}
	squad := guildSquad()
	for _, hero := range stored.Roster.Additions {
		if hero.InstanceID == squad.Units[0].InstanceID {
			squad.Units[0].Tier, squad.Units[0].Level, squad.Units[0].ConsumeXP = hero.Tier, hero.Level, hero.ConsumeXP
		}
	}
	return squad
}

func (h *harness) battle(player, token string, revision int64, id string, request economy.BattleStartRequest) revisioned {
	h.t.Helper()
	request.Squad = h.storedSquad(player)
	seed, outcome := h.winningSeed(request)
	*h.seed = seed
	body := map[string]any{"request_id": id + "-start", "action": economy.ActionBattleStart}
	raw, _ := json.Marshal(request)
	if err := json.Unmarshal(raw, &body); err != nil {
		h.t.Fatal(err)
	}
	body["request_id"], body["action"] = id+"-start", economy.ActionBattleStart
	encoded, _ := json.Marshal(body)
	response, reply := h.do("POST", "/v1/players/"+player+"/actions", token, fmt.Sprint(revision), string(encoded))
	expectStatus(h.t, response, reply, http.StatusOK)
	var started struct {
		Revision int64 `json:"revision"`
		Granted  struct {
			BattleID int64  `json:"battle_id"`
			Seed     uint32 `json:"seed"`
		} `json:"granted"`
	}
	if err := json.Unmarshal([]byte(reply), &started); err != nil || started.Granted.Seed != seed {
		h.t.Fatalf("start reply %s", reply)
	}
	finish := fmt.Sprintf(`{"request_id":"%s-finish","action":"battle","battle_id":%d,"won":true,"crit_gold":%d}`,
		id, started.Granted.BattleID, battle.CritGoldFor(outcome.Crits))
	return h.act(player, token, started.Revision, finish)
}

func TestAMismatchedClaimIsRefusedButTheReplaySettles(t *testing.T) {
	h := newHarness(t)
	token := h.guildPlayer("local", "Hawke", 9, 10)
	data := h.catalog().Battle
	opponent := data.PvpLadderOpponent(9, 0, h.now.Unix()/battle.SecondsPerDay, 11)
	enemy, err := data.PvpRoster(opponent)
	if err != nil {
		t.Fatal(err)
	}
	request := economy.BattleStartRequest{
		Mode: economy.BattlePvp, Squad: h.storedSquad("local"), Enemy: enemy,
		Opponent: economy.PvpOpponentRequest{Kind: economy.OpponentLadder, Seed: 11},
	}
	seed, outcome := h.winningSeed(request)
	*h.seed = seed
	fields := map[string]any{}
	raw, _ := json.Marshal(request)
	_ = json.Unmarshal(raw, &fields)
	fields["request_id"], fields["action"] = "mismatch-start", economy.ActionBattleStart
	raw, _ = json.Marshal(fields)
	response, reply := h.do("POST", "/v1/players/local/actions", token, "1", string(raw))
	expectStatus(t, response, reply, http.StatusOK)
	var started struct {
		Revision int64 `json:"revision"`
		Granted  struct {
			BattleID int64 `json:"battle_id"`
		} `json:"granted"`
	}
	if err := json.Unmarshal([]byte(reply), &started); err != nil {
		t.Fatal(err)
	}
	claim := fmt.Sprintf(`{"request_id":"mismatch-finish","action":"battle","battle_id":%d,"won":true,"crit_gold":%d}`,
		started.Granted.BattleID, battle.CritGoldFor(outcome.Crits+3))
	response, reply = h.do("POST", "/v1/players/local/actions", token, fmt.Sprint(started.Revision), claim)
	expectStatus(t, response, reply, http.StatusUnprocessableEntity)
	var refused struct {
		Code     string `json:"code"`
		Revision int64  `json:"revision"`
		Profile  struct {
			Account struct {
				Gold int64 `json:"gold"`
			} `json:"account"`
		} `json:"profile"`
	}
	if err := json.Unmarshal([]byte(reply), &refused); err != nil {
		t.Fatal(err)
	}
	if refused.Code != string(economy.OutcomeMismatch) || refused.Revision != started.Revision+1 ||
		refused.Profile.Account.Gold <= 10 {
		t.Fatalf("refusal = %s", reply)
	}
	response, again := h.do("POST", "/v1/players/local/actions", token, fmt.Sprint(started.Revision), claim)
	expectStatus(t, response, again, http.StatusUnprocessableEntity)
	var retried struct {
		Code     string `json:"code"`
		Revision int64  `json:"revision"`
	}
	if err := json.Unmarshal([]byte(again), &retried); err != nil || retried.Code != refused.Code ||
		retried.Revision != refused.Revision {
		t.Fatalf("retry = %s", again)
	}
}
