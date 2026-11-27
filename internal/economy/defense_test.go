package economy

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const (
	rivalTrophies   = 900
	defenderStart   = 400
	attackerOpening = 300
)

var (
	weakRival   = battle.Unit{Slot: 0, ID: "UNIT_HURLOCKGRUNT", Tier: "I", Level: 1}
	strongRival = battle.Unit{Slot: 0, ID: "UNIT_HEROOFRIVERDANE", Tier: "IV", Level: 85}
)

func rivalDuels(rival battle.Unit) fixedDuels {
	return fixedDuels{
		match: DuelMatch{Name: "Rival", Level: 7, Trophies: rivalTrophies},
		squad: DuelOpponent{ID: "rival", Units: []DuelUnit{
			{Slot: rival.Slot, ID: rival.ID, Tier: rival.Tier, Level: rival.Level},
		}},
	}
}

func rivalFor(won bool) battle.Unit {
	if won {
		return weakRival
	}
	return strongRival
}

func duelRequest(t *testing.T, event bool) BattleStartRequest {
	t.Helper()
	c := mustCatalog(t).Battle
	request := BattleStartRequest{
		Mode: BattlePvp, Duel: "rival", Event: event, Squad: fixtureSquad(), Opponent: PvpOpponentRequest{Seed: 5},
		Enemy: battle.Side{Units: []battle.Unit{weakRival}, Runes: []string{}},
	}
	if event {
		request.Changer = c.PvpEventChanger(c.PvpEventCycle(ruleNow.Unix()))
	}
	return request
}

func attackerProfile(t *testing.T) json.RawMessage {
	t.Helper()
	return gameProfile(t, func(doc map[string]any) {
		section(doc, "account")["name"] = "Hawke"
		doc["pvp"] = map[string]any{"trophies": attackerOpening, "wins": 1}
	})
}

func duel(t *testing.T, raw json.RawMessage, server *profile.ServerState, request BattleStartRequest, won bool,
	at time.Time,
) (json.RawMessage, BattleResult) {
	t.Helper()
	rival := rivalFor(won)
	request.Squad = squadOf(t, asMap(t, raw))
	request.Enemy.Units = []battle.Unit{rival}
	seed, outcome := seedFor(t, request, won)
	started, start, err := ActWith(mustCatalog(t), raw, server, ActionBattleStart, encodeBody(t, request), at,
		Env{Duels: rivalDuels(rival), Seeds: fixedSeed(seed)})
	if err != nil {
		t.Fatalf("battle_start: %v", err)
	}
	claim := BattleRequest{BattleID: start.BattleID, Won: outcome.Won, CritGold: battle.CritGoldFor(outcome.Crits)}
	finished, result, err := ActWith(mustCatalog(t), started, server, ActionBattle, encodeBody(t, claim), at, Env{})
	if err != nil {
		t.Fatalf("battle: %v", err)
	}
	return finished, result
}

func defenderProfile(t *testing.T) json.RawMessage {
	t.Helper()
	return gameProfile(t, func(doc map[string]any) {
		doc["pvp"] = map[string]any{"trophies": rivalTrophies, "wins": 4, "losses": 2}
	})
}

func landed(t *testing.T, strike Strike) map[string]any {
	t.Helper()
	strike.Attacker = "attacker"
	next, err := LandStrike(defenderProfile(t), strike)
	if err != nil {
		t.Fatal(err)
	}
	return section(asMap(t, next), "pvp")
}

func TestTheFirstSquadDefendsWhicheverSquadIsSelected(t *testing.T) {
	raw := gameProfile(t, func(doc map[string]any) {
		doc["squads"] = []any{[]any{1, nil, nil, nil, nil}, []any{2, nil, nil, nil, nil}}
		doc["current_squad"] = 1
	})
	opponent := duelOpponent(t, raw)
	if len(opponent.Units) != 1 || opponent.Units[0].ID != "UNIT_ELVENMAGE" {
		t.Errorf("the defender fields %+v, want its first squad's UNIT_ELVENMAGE", opponent.Units)
	}
}

func TestAWonAttackOnAPlayerTakesWhatTheAttackerGains(t *testing.T) {
	c := mustCatalog(t)
	_, result := duel(t, attackerProfile(t), &profile.ServerState{StoreRNG: 3}, duelRequest(t, false), true, ruleNow)
	strike := result.Strike
	gain := trophyGain(&c.Pvp, attackerOpening, rivalTrophies)
	if result.TrophyDelta != gain || strike == nil || strike.Defender != "rival" || !strike.AttackerWon ||
		strike.Trophies != -gain || strike.AttackerName != "Hawke" || strike.AttackerTrophies != attackerOpening ||
		strike.Event {
		t.Fatalf("result %+v strikes %+v", result, strike)
	}
	pvp := landed(t, *strike)
	line := pvp["history"].([]any)[0].(map[string]any)
	if pvp["trophies"].(float64) != float64(rivalTrophies-gain) || pvp["defense_losses"].(float64) != 1 ||
		line["defense"] != true || line["won"] != false || line["opponent"] != "Hawke" ||
		line["opponent_id"] != "attacker" || line["trophy_delta"].(float64) != float64(-gain) ||
		line["opponent_trophies"].(float64) != attackerOpening ||
		pvp["defense_checked_at"].(float64) != float64(ruleNow.Unix()) {
		t.Errorf("the defender after the attack: %v", pvp)
	}
}

func TestALostAttackOnAPlayerGivesTheDefenderWhatTheAttackerLoses(t *testing.T) {
	c := mustCatalog(t)
	_, result := duel(t, attackerProfile(t), &profile.ServerState{StoreRNG: 3}, duelRequest(t, false), false, ruleNow)
	loss := trophyLoss(&c.Pvp, attackerOpening, rivalTrophies)
	if result.TrophyDelta != -loss || result.Strike == nil || result.Strike.AttackerWon ||
		result.Strike.Trophies != loss {
		t.Fatalf("result %+v strike %+v", result, result.Strike)
	}
	pvp := landed(t, *result.Strike)
	line := pvp["history"].([]any)[0].(map[string]any)
	if pvp["trophies"].(float64) != float64(rivalTrophies+loss) || pvp["defense_wins"].(float64) != 1 ||
		line["won"] != true || line["trophy_delta"].(float64) != float64(loss) {
		t.Errorf("the defender after holding: %v", pvp)
	}
}

func TestAnEventAttackOnAPlayerLeavesTheDefendersBannersAndTrophies(t *testing.T) {
	server := &profile.ServerState{StoreRNG: 3}
	finished, result := duel(t, attackerProfile(t), server, duelRequest(t, true), true, ruleNow)
	if result.Banners <= 0 || server.PvpEvent.Banners != result.Banners || result.Strike == nil ||
		!result.Strike.Event || result.Strike.Trophies != 0 {
		t.Fatalf("event duel result %+v strike %+v", result, result.Strike)
	}
	attack := section(asMap(t, finished), "pvp")["history"].([]any)[0].(map[string]any)
	if attack["event"] != true || attack["opponent_id"] != "rival" {
		t.Errorf("the attacker's event line %v", attack)
	}
	pvp := landed(t, *result.Strike)
	line := pvp["history"].([]any)[0].(map[string]any)
	if pvp["trophies"].(float64) != rivalTrophies || line["event"] != true || line["defense"] != true ||
		line["banners"].(float64) != 0 || line["trophy_delta"].(float64) != 0 {
		t.Errorf("the defender after an event attack: %v", pvp)
	}
}

func TestTheSamePlayerCanBeAttackedOncePerDay(t *testing.T) {
	server := &profile.ServerState{StoreRNG: 3}
	finished, _ := duel(t, attackerProfile(t), server, duelRequest(t, false), true, ruleNow)
	again := duelRequest(t, true)
	again.Squad = squadOf(t, asMap(t, finished))
	cooling := ruleNow.Add(pvpRepeatAttackSeconds*time.Second - time.Second)
	_, _, err := ActWith(mustCatalog(t), finished, server, ActionBattleStart, encodeBody(t, again), cooling,
		Env{Duels: rivalDuels(weakRival), Seeds: fixedSeed(1)})
	if refusalCode(err) != AttackCooldown {
		t.Fatalf("attacked the same player again within a day: %v", err)
	}
	attacked := section(asMap(t, finished), "pvp")["attacked"].(map[string]any)
	if attacked["rival"].(float64) != float64(ruleNow.Unix()) {
		t.Errorf("the attack time on record: %v", attacked)
	}
	duel(t, finished, server, duelRequest(t, false), true, ruleNow.Add(pvpRepeatAttackSeconds*time.Second))
}

func TestOldAttackTimesAreDropped(t *testing.T) {
	stats := pvpStats{Attacked: map[string]int64{"old": 100, "recent": 100 + pvpRepeatAttackSeconds}}
	stats.noteAttack("rival", 100+pvpRepeatAttackSeconds)
	if _, kept := stats.Attacked["old"]; kept || len(stats.Attacked) != 2 {
		t.Errorf("attack times kept: %v", stats.Attacked)
	}
}

func defenceDue(t *testing.T, squads []any) map[string]any {
	t.Helper()
	interval := mustCatalog(t).Pvp.Defense.AttackIntervalSeconds
	raw := gameProfile(t, func(doc map[string]any) {
		enlistVeterans(doc)
		doc["squads"] = squads
		doc["pvp"] = map[string]any{
			"trophies": defenderStart, "wins": 1, "defense_checked_at": ruleNow.Unix() - 3*interval,
		}
	})
	doc, _ := mustAct(t, raw, &profile.ServerState{StoreRNG: 3}, ActionPvpDefend, `{}`)
	return doc
}

func TestBotDefencesAreFoughtByTheFirstSquad(t *testing.T) {
	c := mustCatalog(t)
	veterans := defenceDue(t, []any{[]any{1, 2, 3, 4, 5}, []any{6, nil, nil, nil, nil}})
	empty := defenceDue(t, []any{[]any{nil, nil, nil, nil, nil}, []any{1, 2, 3, 4, 5}})
	for name, want := range map[string]struct {
		doc map[string]any
		won bool
	}{"veterans": {veterans, true}, "an empty first squad": {empty, false}} {
		pvp := section(want.doc, "pvp")
		history := pvp["history"].([]any)
		if int64(len(history)) != c.Pvp.Defense.MaxAttacksPerLogin {
			t.Fatalf("%s defended %d times", name, len(history))
		}
		oldest := history[len(history)-1].(map[string]any)
		stake := botDefenseTrophies(&c.Pvp, int(oldest["opponent_trophies"].(float64)), defenderStart, !want.won)
		if oldest["trophy_delta"].(float64) != float64(stake) {
			t.Errorf("%s: first defence moved %v trophies, want %d", name, oldest["trophy_delta"], stake)
		}
		for _, entry := range history {
			if entry := entry.(map[string]any); entry["won"] != want.won {
				t.Errorf("%s: defence %v", name, entry)
			}
		}
	}
}

func TestRevengeIsOfferedOnceOnAWonBotDefence(t *testing.T) {
	c := mustCatalog(t)
	doc := defenceDue(t, []any{[]any{1, 2, 3, 4, 5}})
	held := section(doc, "pvp")["history"].([]any)[0].(map[string]any)
	opponent := battle.PvpOpponent{
		Bot: int(held["bot"].(float64)), Squad: int(held["squad"].(float64)), Level: int(held["level"].(float64)),
		Seed: uint32(held["seed"].(float64)),
	}
	enemy, err := c.Battle.PvpRoster(opponent)
	if err != nil {
		t.Fatal(err)
	}
	revenge := BattleStartRequest{
		Mode: BattlePvp, RevengeAt: int64(held["at"].(float64)), Squad: veteranSquad(), Enemy: enemy,
		Opponent: PvpOpponentRequest{Kind: OpponentRevenge, Seed: opponent.Seed},
	}
	server := &profile.ServerState{StoreRNG: 3}
	revenged, _, _ := fight(t, reencode(t, doc), server, revenge, true)
	if _, _, err := startBattle(t, reencode(t, revenged), server, revenge, 1); refusalCode(err) != Unavailable {
		t.Errorf("revenged a won defence twice: %v", err)
	}
}

func TestRevengeOnAPlayerIsADuelNotABotRevenge(t *testing.T) {
	strike := Strike{Defender: "me", Attacker: "attacker", AttackerName: "Hawke", AttackerWon: true, At: ruleNow.Unix()}
	defended, err := LandStrike(attackerProfile(t), strike)
	if err != nil {
		t.Fatal(err)
	}
	revenge := BattleStartRequest{
		Mode: BattlePvp, RevengeAt: ruleNow.Unix(), Squad: fixtureSquad(), Opponent: PvpOpponentRequest{Seed: 0},
	}
	if _, _, err := startBattle(t, defended, &profile.ServerState{StoreRNG: 3}, revenge, 1); refusalCode(err) != Unavailable {
		t.Errorf("a player's attack was revenged as a bot: %v", err)
	}
	both := duelRequest(t, false)
	both.RevengeAt = ruleNow.Unix()
	if _, _, err := startBattle(t, defended, &profile.ServerState{StoreRNG: 3}, both, 1); refusalCode(err) != Unavailable {
		t.Errorf("a duel and a bot revenge at once: %v", err)
	}
}
