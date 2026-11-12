package economy

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const seedSearchLimit = 4000

func emptyGear() []string { return make([]string, gearSlotsPerHero) }

func fixtureSquad() battle.Side {
	return battle.Side{Units: []battle.Unit{
		{Slot: 0, InstanceID: 1, ID: "UNIT_ELVENMAGE", Tier: "I", Level: 3, Gear: emptyGear()},
		{Slot: 1, InstanceID: 2, ID: "UNIT_ARCANEMARIANHAWKE", Tier: "I", Level: 1, Gear: emptyGear()},
	}, Runes: []string{}}
}

func veteranSquad() battle.Side {
	squad := battle.Side{Runes: []string{}}
	for slot, hero := range []struct {
		id    string
		level int
	}{
		{"UNIT_HEROOFRIVERDANE", 85}, {"UNIT_THEARCHITECT", 85}, {"UNIT_ARCANEGARRETTHAWKE", 85},
		{"UNIT_ARCANEMARIANHAWKE", 85}, {"UNIT_STONEJAW", 65},
	} {
		squad.Units = append(squad.Units, battle.Unit{
			Slot: slot, InstanceID: int64(slot + 1), ID: hero.id, Tier: "IV", Level: hero.level, Gear: emptyGear(),
		})
	}
	return squad
}

func veteranProfile(t *testing.T) (json.RawMessage, battle.Side) {
	t.Helper()
	squad := veteranSquad()
	raw := gameProfile(t, func(doc map[string]any) {
		heroes := []any{}
		for _, unit := range squad.Units {
			heroes = append(heroes, map[string]any{
				"id": unit.ID, "instance_id": unit.InstanceID, "tier": unit.Tier, "level": unit.Level, "xp": 0,
				"ascended": false, "consume_xp": 0,
			})
		}
		roster := section(doc, "roster")
		roster["additions"], roster["next_instance_id"] = heroes, len(heroes)+1
	})
	return raw, squad
}

func squadOf(t *testing.T, doc map[string]any) battle.Side {
	t.Helper()
	return squadFrom(t, doc, fixtureSquad())
}

func squadFrom(t *testing.T, doc map[string]any, squad battle.Side) battle.Side {
	t.Helper()
	for i := range squad.Units {
		unit := &squad.Units[i]
		for _, entry := range section(doc, "roster")["additions"].([]any) {
			hero := entry.(map[string]any)
			if int64(hero["instance_id"].(float64)) == unit.InstanceID {
				unit.Tier, unit.Level = hero["tier"].(string), int(hero["level"].(float64))
				consume, _ := hero["consume_xp"].(float64)
				unit.ConsumeXP = int(consume)
			}
		}
	}
	return squad
}

func squadFor(request BattleStartRequest, won bool) BattleStartRequest {
	if !won {
		request.Squad.Units = request.Squad.Units[:1]
	}
	return request
}

func replayOf(t *testing.T, request BattleStartRequest, seed uint32) battle.Replay {
	t.Helper()
	return battle.Replay{
		Seed: seed, Player: request.Squad, Enemy: request.Enemy, Changer: canonicalChanger(request.Changer),
		StatBudget: request.Mode == BattlePvp && mustCatalog(t).Battle.PvpStatBudget(),
	}
}

func seedBeforeFlip(t *testing.T, request BattleStartRequest, wanted bool) uint32 {
	t.Helper()
	c := mustCatalog(t)
	won := func(seed uint32) bool {
		outcome, err := c.Battle.Run(replayOf(t, request, seed))
		if err != nil {
			t.Fatal(err)
		}
		return outcome.Won
	}
	for seed := uint32(1); seed < seedSearchLimit; seed++ {
		if won(seed) != wanted && won(seed+1) == wanted {
			return seed
		}
	}
	t.Fatalf("no seed before a won=%v battle", wanted)
	return 0
}

func campaignStart(t *testing.T, mapIndex, node int) BattleStartRequest {
	t.Helper()
	c := mustCatalog(t)
	enemy, ok := c.Battle.CampaignRoster(c.Campaign[mapIndex].ID, node+1)
	if !ok {
		t.Fatalf("no roster for campaign %d/%d", mapIndex, node)
	}
	return BattleStartRequest{Mode: BattleCampaign, Map: mapIndex, Node: node, Squad: fixtureSquad(), Enemy: enemy}
}

func nexusStart(t *testing.T, step int) BattleStartRequest {
	t.Helper()
	c := mustCatalog(t)
	event := &c.Nexus[0]
	enemy, changer, err := nexusEnemy(c, event, &event.Difficulties[0], step)
	if err != nil {
		t.Fatal(err)
	}
	return BattleStartRequest{Mode: BattleNexus, Step: step, Squad: fixtureSquad(), Enemy: enemy, Changer: changer}
}

func matchOpponent(t *testing.T, raw json.RawMessage, server *profile.ServerState, request PvpMatchRequest,
	roll uint32,
) (json.RawMessage, profile.PvpMatch) {
	t.Helper()
	next, result, err := ActWith(mustCatalog(t), raw, server, ActionPvpMatch, encodeBody(t, request), ruleNow,
		Env{Seeds: fixedSeed(roll)})
	if err != nil {
		t.Fatalf("pvp_match: %v", err)
	}
	if result.Match == nil || server.PvpMatch == nil || *server.PvpMatch != *result.Match {
		t.Fatalf("match = %+v stored = %+v", result.Match, server.PvpMatch)
	}
	return next, *result.Match
}

func pvpStartFor(t *testing.T, match profile.PvpMatch) BattleStartRequest {
	t.Helper()
	c := mustCatalog(t)
	enemy, err := c.Battle.PvpRoster(battle.PvpOpponent{
		Bot: match.Bot, Squad: match.Squad, Level: match.Level, Trophies: match.Trophies, Seed: match.Seed,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := BattleStartRequest{
		Mode: BattlePvp, Event: match.Event, Match: match.ID, Squad: fixtureSquad(), Enemy: enemy,
	}
	if match.Event {
		request.Changer = c.Battle.PvpEventChanger(c.Battle.PvpEventCycle(match.At))
	}
	return request
}

func pvpMatched(t *testing.T, raw json.RawMessage, server *profile.ServerState, event bool, roll uint32,
) BattleStartRequest {
	t.Helper()
	_, match := matchOpponent(t, raw, server, PvpMatchRequest{Event: event}, roll)
	return pvpStartFor(t, match)
}

func seedFor(t *testing.T, request BattleStartRequest, won bool) (uint32, battle.Outcome) {
	t.Helper()
	c := mustCatalog(t)
	for seed := uint32(1); seed < seedSearchLimit; seed++ {
		outcome, err := c.Battle.Run(replayOf(t, request, seed))
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Won == won {
			return seed, outcome
		}
	}
	t.Fatalf("no seed in %d makes the fixture squad win=%v", seedSearchLimit, won)
	return 0, battle.Outcome{}
}

func fixedSeed(seed uint32) SeedSource {
	return func() (uint32, error) { return seed, nil }
}

func encodeBody(t *testing.T, body any) []byte {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func startBattle(t *testing.T, raw json.RawMessage, server *profile.ServerState, request BattleStartRequest,
	seed uint32,
) (json.RawMessage, BattleResult, error) {
	t.Helper()
	return ActWith(mustCatalog(t), raw, server, ActionBattleStart, encodeBody(t, request), ruleNow,
		Env{Seeds: fixedSeed(seed)})
}

func finishBattle(t *testing.T, raw json.RawMessage, server *profile.ServerState, claim BattleRequest) (
	json.RawMessage, BattleResult, error,
) {
	t.Helper()
	return ActWith(mustCatalog(t), raw, server, ActionBattle, encodeBody(t, claim), ruleNow, Env{})
}

func fight(t *testing.T, raw json.RawMessage, server *profile.ServerState, request BattleStartRequest, won bool) (
	map[string]any, BattleResult, battle.Outcome,
) {
	t.Helper()
	request = squadFor(request, won)
	seed, outcome := seedFor(t, request, won)
	started, start, err := startBattle(t, raw, server, request, seed)
	if err != nil {
		t.Fatalf("battle_start: %v", err)
	}
	if start.Seed != seed || start.BattleID == 0 || server.Battle == nil {
		t.Fatalf("start = %+v pending = %+v", start, server.Battle)
	}
	claim := BattleRequest{BattleID: start.BattleID, Won: outcome.Won, CritGold: battle.CritGoldFor(outcome.Crits)}
	finished, result, err := finishBattle(t, started, server, claim)
	if err != nil {
		t.Fatalf("battle: %v", err)
	}
	if server.Battle != nil || result.Won != won || result.Crits != outcome.Crits {
		t.Fatalf("result = %+v pending = %+v", result, server.Battle)
	}
	return asMap(t, finished), result, outcome
}

func TestCampaignBattlePaysFromTheNodeTable(t *testing.T) {
	c := mustCatalog(t)
	node := c.Campaign[0].Nodes[0]
	server := &profile.ServerState{StoreRNG: 3}
	doc, result, outcome := fight(t, gameProfile(t, nil), server, campaignStart(t, 0, 0), true)
	wantGold := 1000 + int64(node.Gold) + creditedCritGold(outcome.Crits)
	if !result.FirstWin || result.Gems != int64(node.FirstWinGems) ||
		number(t, doc, "account", "gold") != float64(wantGold) ||
		number(t, doc, "account", "energy") != float64(20-node.EnergyCost) {
		t.Errorf("result = %+v account = %v", result, doc["account"])
	}
	if section(doc, "campaign")["completions"].([]any)[0].([]any)[0].(float64) != 1 {
		t.Errorf("completions = %v", section(doc, "campaign")["completions"])
	}
}

func TestCampaignBattleStartChecksTheNodeAndEnergy(t *testing.T) {
	raw := gameProfile(t, nil)
	locked := campaignStart(t, 0, 3)
	if _, _, err := startBattle(t, raw, &profile.ServerState{}, locked, 1); refusalCode(err) != Unavailable {
		t.Errorf("locked node: %v", err)
	}
	otherMap := campaignStart(t, 1, 0)
	if _, _, err := startBattle(t, raw, &profile.ServerState{}, otherMap, 1); refusalCode(err) != Unavailable {
		t.Errorf("locked map: %v", err)
	}
	tired := gameProfile(t, func(doc map[string]any) { section(doc, "account")["energy"] = 0 })
	if _, _, err := startBattle(t, tired, &profile.ServerState{}, campaignStart(t, 0, 0), 1); refusalCode(err) != CantAfford {
		t.Errorf("no energy: %v", err)
	}
}

func TestCampaignMasteryPaysOnce(t *testing.T) {
	c := mustCatalog(t)
	node := c.Campaign[0].Nodes[0]
	raw := gameProfile(t, func(doc map[string]any) {
		section(doc, "campaign")["completions"].([]any)[0].([]any)[0] = node.WinsToMaster - 1
	})
	_, result, _ := fight(t, raw, &profile.ServerState{StoreRNG: 3}, campaignStart(t, 0, 0), true)
	if !result.Mastered || result.Gems != int64(node.MasteryGems) {
		t.Errorf("result = %+v", result)
	}
	doc, result, _ := fight(t, raw, &profile.ServerState{StoreRNG: 3}, campaignStart(t, 0, 0), false)
	if result.Gold != 0 || number(t, doc, "account", "gold") != 1000 {
		t.Errorf("a loss paid %+v", result)
	}
}

func TestBattleRefusesAWinTheReplayLost(t *testing.T) {
	request := squadFor(campaignStart(t, 0, 0), false)
	seed, outcome := seedFor(t, request, false)
	server := &profile.ServerState{StoreRNG: 3}
	started, start, err := startBattle(t, gameProfile(t, nil), server, request, seed)
	if err != nil {
		t.Fatal(err)
	}
	claim := BattleRequest{BattleID: start.BattleID, Won: true, CritGold: battle.CritGoldFor(outcome.Crits + 50)}
	finished, result, err := finishBattle(t, started, server, claim)
	if refusalCode(err) != OutcomeMismatch || !CommittedRefusal(err) || finished == nil {
		t.Fatalf("a claimed win was not refused: %v", err)
	}
	doc := asMap(t, finished)
	if result.Won || result.Gold != 0 || number(t, doc, "account", "gold") != 1000 || server.Battle != nil {
		t.Errorf("result = %+v account = %v", result, doc["account"])
	}
	if losses := section(doc, "campaign")["node_losses"].([]any); losses[0].(float64) != 1 {
		t.Errorf("the replayed loss was not recorded: %v", losses)
	}
}

func TestBattleStartRefusesASquadTheServerDoesNotHave(t *testing.T) {
	raw := gameProfile(t, nil)
	stronger := campaignStart(t, 0, 0)
	stronger.Squad.Units[0].Level = 60
	if _, _, err := startBattle(t, raw, &profile.ServerState{}, stronger, 1); refusalCode(err) != SquadChanged {
		t.Errorf("inflated level: %v", err)
	}
	stranger := campaignStart(t, 0, 0)
	stranger.Squad.Units[1].InstanceID = 99
	if _, _, err := startBattle(t, raw, &profile.ServerState{}, stranger, 1); refusalCode(err) != SquadChanged {
		t.Errorf("unknown hero: %v", err)
	}
	geared := campaignStart(t, 0, 0)
	geared.Squad.Units[0].Gear[0] = "templar_longsword"
	if _, _, err := startBattle(t, raw, &profile.ServerState{}, geared, 1); refusalCode(err) != SquadChanged {
		t.Errorf("gear the hero does not wear: %v", err)
	}
	empty := campaignStart(t, 0, 0)
	empty.Squad = battle.Side{}
	if _, _, err := startBattle(t, raw, &profile.ServerState{}, empty, 1); refusalCode(err) != Unavailable {
		t.Errorf("empty squad: %v", err)
	}
}

func TestBattleStartUsesOnlyActiveRunes(t *testing.T) {
	active := gameProfile(t, func(doc map[string]any) {
		section(doc, "runes")["items"].([]any)[2].(map[string]any)["expire_at"] = ruleNow.Unix() + 600
	})
	request := campaignStart(t, 0, 0)
	request.RuneIDs = []int64{3}
	request.Squad.Runes = []string{"rune-power-1"}
	if _, _, err := startBattle(t, active, &profile.ServerState{}, request, 1); err != nil {
		t.Errorf("active rune: %v", err)
	}
	if _, _, err := startBattle(t, gameProfile(t, nil), &profile.ServerState{}, request, 1); refusalCode(err) != SquadChanged {
		t.Errorf("rune that was never activated: %v", err)
	}
	request.Squad.Runes = []string{"rune-greed-1"}
	if _, _, err := startBattle(t, active, &profile.ServerState{}, request, 1); refusalCode(err) != SquadChanged {
		t.Errorf("rune claimed under another def: %v", err)
	}
}

func TestBattleStartRefusesAnOpponentTheServerDoesNotField(t *testing.T) {
	weaker := campaignStart(t, 0, 0)
	weaker.Enemy.Units = weaker.Enemy.Units[:1]
	if _, _, err := startBattle(t, gameProfile(t, nil), &profile.ServerState{}, weaker, 1); refusalCode(err) != OpponentChanged {
		t.Errorf("fewer enemies: %v", err)
	}
	changed := campaignStart(t, 0, 0)
	changed.Changer = &battle.Changer{Effects: []battle.ChangerEffect{{PowerPct: 100}}}
	if _, _, err := startBattle(t, gameProfile(t, nil), &profile.ServerState{}, changed, 1); refusalCode(err) != OpponentChanged {
		t.Errorf("a game changer the server does not apply: %v", err)
	}
}

func TestBattleFinishNeedsTheStartedBattle(t *testing.T) {
	raw := gameProfile(t, nil)
	if _, _, err := finishBattle(t, raw, &profile.ServerState{}, BattleRequest{Won: true}); refusalCode(err) != Unavailable {
		t.Errorf("a battle without battle_start: %v", err)
	}
	server := &profile.ServerState{}
	started, start, err := startBattle(t, raw, server, campaignStart(t, 0, 0), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := finishBattle(t, started, server, BattleRequest{BattleID: start.BattleID + 1}); refusalCode(err) != Unavailable {
		t.Errorf("another battle id: %v", err)
	}
}

func TestAnUnfinishedBattleSettlesAtTheNextStart(t *testing.T) {
	request := campaignStart(t, 0, 0)
	seed, _ := seedFor(t, request, true)
	server := &profile.ServerState{StoreRNG: 3}
	started, first, err := startBattle(t, gameProfile(t, nil), server, request, seed)
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := startBattle(t, started, server, request, seed)
	if err != nil {
		t.Fatal(err)
	}
	if second.Settled == nil || !second.Settled.Won || !second.Settled.FirstWin ||
		second.Settled.BattleID != first.BattleID || server.Battle.ID != second.BattleID {
		t.Errorf("settled = %+v pending = %+v", second.Settled, server.Battle)
	}
}

func TestTutorialBattlesGetTheScriptedOutcome(t *testing.T) {
	tutorial := gameProfile(t, func(doc map[string]any) {
		doc["tutorial"] = map[string]any{"complete": false, "consume_complete": false}
	})
	c := mustCatalog(t)
	for _, want := range []string{TutorialVictory, TutorialDefeat} {
		victory := want == TutorialVictory
		request := squadFor(campaignStart(t, 0, 0), false)
		request.TutorialOutcome = want
		_, result, err := startBattle(t, tutorial, &profile.ServerState{}, request, seedBeforeFlip(t, request, victory))
		if err != nil {
			t.Fatal(err)
		}
		outcome, _ := c.Battle.Run(battle.Replay{Seed: result.Seed, Player: request.Squad, Enemy: request.Enemy})
		if outcome.Won != victory {
			t.Errorf("%s tutorial battle got seed %d that won=%v", want, result.Seed, outcome.Won)
		}
	}
	request := campaignStart(t, 0, 0)
	request.TutorialOutcome = TutorialVictory
	if _, _, err := startBattle(t, gameProfile(t, nil), &profile.ServerState{}, request, 1); refusalCode(err) != Unavailable {
		t.Errorf("a scripted outcome after the tutorial: %v", err)
	}
}

func TestPvpBattleFightsTheOpponentTheServerMatched(t *testing.T) {
	c := mustCatalog(t)
	raw, squad := veteranProfile(t)
	server := &profile.ServerState{StoreRNG: 3}
	_, match := matchOpponent(t, raw, server, PvpMatchRequest{}, 42)
	want := c.Battle.PvpLadderOpponent(5, 0, ruleNow.Unix()/battle.SecondsPerDay, pvpMatchSeed(42, false, -1))
	if match.Bot != want.Bot || match.Squad != want.Squad || match.Level != want.Level ||
		match.Trophies != want.Trophies || match.Seed != want.Seed || match.At != ruleNow.Unix() || match.Chosen {
		t.Fatalf("match = %+v, want %+v", match, want)
	}
	request := pvpStartFor(t, match)
	request.Squad = squad
	doc, result, _ := fight(t, raw, server, request, true)
	if result.Gold < pvpGold(&c.Pvp, match.Level, true) || number(t, doc, "account", "stamina") != 7 {
		t.Errorf("result = %+v level %d", result, match.Level)
	}
	if server.PvpMatch != nil || server.PvpRoll != 0 {
		t.Errorf("a fought match is still offered: %+v roll %d", server.PvpMatch, server.PvpRoll)
	}
	if _, _, err := startBattle(t, raw, server, request, 1); refusalCode(err) != OpponentChanged {
		t.Errorf("fighting a match twice: %v", err)
	}

	tired := gameProfile(t, func(doc map[string]any) { section(doc, "account")["stamina"] = 0 })
	tiredServer := &profile.ServerState{}
	if _, _, err := startBattle(t, tired, tiredServer, pvpMatched(t, tired, tiredServer, false, 42), 1); refusalCode(err) != CantAfford {
		t.Errorf("no stamina: %v", err)
	}
	unmatched := request
	unmatched.Match = 0
	if _, _, err := startBattle(t, raw, &profile.ServerState{}, unmatched, 1); refusalCode(err) != Unavailable {
		t.Errorf("a battle without a match: %v", err)
	}
	stale := &profile.ServerState{}
	request = pvpMatched(t, raw, stale, false, 42)
	request.Match++
	if _, _, err := startBattle(t, raw, stale, request, 1); refusalCode(err) != OpponentChanged {
		t.Errorf("a match the server did not issue: %v", err)
	}
	request.Match--
	request.Event = true
	if _, _, err := startBattle(t, raw, stale, request, 1); refusalCode(err) != OpponentChanged {
		t.Errorf("a ladder match fought as an event battle: %v", err)
	}
}

func TestPvpBattlesReplayUnderTheStatBudgetOnlyWhenTheRuleIsOn(t *testing.T) {
	budgeted := catalogWithPvpStatBudget(t, true)
	raw := gameProfile(t, nil)
	server := &profile.ServerState{StoreRNG: 3}
	request := pvpMatched(t, raw, server, false, 42)
	if _, _, err := ActWith(budgeted, raw, server, ActionBattleStart, encodeBody(t, request), ruleNow,
		Env{Seeds: fixedSeed(1)}); err != nil {
		t.Fatal(err)
	}
	if !server.Battle.Replay.StatBudget {
		t.Error("a PvP battle started without the stat budget")
	}
	levelled, err := budgeted.Battle.Run(server.Battle.Replay)
	if err != nil {
		t.Fatal(err)
	}
	plain := server.Battle.Replay
	plain.StatBudget = false
	unlevelled, err := budgeted.Battle.Run(plain)
	if err != nil {
		t.Fatal(err)
	}
	if levelled.Timeline == unlevelled.Timeline {
		t.Error("the stat budget did not change the battle")
	}

	campaign := &profile.ServerState{}
	if _, _, err := ActWith(budgeted, raw, campaign, ActionBattleStart, encodeBody(t, campaignStart(t, 0, 0)),
		ruleNow, Env{Seeds: fixedSeed(1)}); err != nil {
		t.Fatal(err)
	}
	if campaign.Battle.Replay.StatBudget {
		t.Error("a campaign battle started under the stat budget")
	}

	shipped, err := catalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	ladder := &profile.ServerState{StoreRNG: 3}
	if _, _, err := ActWith(shipped, raw, ladder, ActionBattleStart,
		encodeBody(t, pvpMatched(t, raw, ladder, false, 42)), ruleNow, Env{Seeds: fixedSeed(1)}); err != nil {
		t.Fatal(err)
	}
	if ladder.Battle.Replay.StatBudget != shipped.Battle.PvpStatBudget() {
		t.Error("the shipped rule and the started battle disagree about the stat budget")
	}
}

var fullSquadHeroes = []string{
	"UNIT_ELVENMAGE", "UNIT_ARCANEMARIANHAWKE", "UNIT_CARTASMUGGLER", "UNIT_TEMPLARARCHER", "UNIT_BEAR",
}

func fullSquadProfile(t *testing.T) (json.RawMessage, battle.Side) {
	t.Helper()
	squad := battle.Side{Runes: []string{}}
	additions := []any{}
	for slot, id := range fullSquadHeroes {
		instance := int64(slot + 1)
		additions = append(additions, map[string]any{
			"id": id, "instance_id": instance, "tier": "I", "level": 1, "xp": 0, "ascended": false, "consume_xp": 0,
		})
		squad.Units = append(squad.Units, battle.Unit{
			Slot: slot, InstanceID: instance, ID: id, Tier: "I", Level: 1, Gear: emptyGear(),
		})
	}
	raw := gameProfile(t, func(doc map[string]any) {
		roster := section(doc, "roster")
		roster["additions"] = additions
		roster["next_instance_id"] = len(additions) + 1
	})
	return raw, squad
}

func TestAFullSquadOfLowLevelHeroesWinsALevelledLadderBattleAndIsPaid(t *testing.T) {
	levelled := catalogWithPvpStatBudget(t, true)
	raw, squad := fullSquadProfile(t)
	server := &profile.ServerState{StoreRNG: 3}
	_, match := matchOpponent(t, raw, server, PvpMatchRequest{}, 42)
	request := pvpStartFor(t, match)
	request.Squad = squad

	seed, won := uint32(0), battle.Outcome{}
	for candidate := uint32(1); candidate < seedSearchLimit && seed == 0; candidate++ {
		outcome, err := levelled.Battle.Run(battle.Replay{
			Seed: candidate, Player: squad, Enemy: request.Enemy, StatBudget: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Won {
			seed, won = candidate, outcome
		}
	}
	if seed == 0 {
		t.Fatal("a full squad never wins a levelled ladder battle")
	}
	started, start, err := ActWith(levelled, raw, server, ActionBattleStart, encodeBody(t, request), ruleNow,
		Env{Seeds: fixedSeed(seed)})
	if err != nil {
		t.Fatal(err)
	}
	claim := BattleRequest{BattleID: start.BattleID, Won: true, CritGold: battle.CritGoldFor(won.Crits)}
	_, result, err := ActWith(levelled, started, server, ActionBattle, encodeBody(t, claim), ruleNow, Env{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Won || result.Gold < pvpGold(&levelled.Pvp, match.Level, true) {
		t.Errorf("result = %+v", result)
	}
}

func TestPvpMatchesStandUntilFoughtOrSkipped(t *testing.T) {
	c := mustCatalog(t)
	raw := gameProfile(t, nil)
	server := &profile.ServerState{}
	_, first := matchOpponent(t, raw, server, PvpMatchRequest{}, 42)
	matchOpponent(t, raw, server, PvpMatchRequest{Event: true}, 99)
	_, again := matchOpponent(t, raw, server, PvpMatchRequest{}, 99)
	if again.Seed != first.Seed || again.Squad != first.Squad || again.ID == first.ID {
		t.Fatalf("re-entering re-rolled the opponent: %+v then %+v", first, again)
	}
	later := ruleNow.Add(5 * time.Hour)
	if _, _, err := ActWith(c, raw, server, ActionBattleStart, encodeBody(t, pvpStartFor(t, again)), later,
		Env{Seeds: fixedSeed(1)}); err != nil {
		t.Errorf("a match fought five hours later: %v", err)
	}

	skipping := &profile.ServerState{}
	_, kept := matchOpponent(t, raw, skipping, PvpMatchRequest{}, 42)
	next, skipped := matchOpponent(t, raw, skipping, PvpMatchRequest{Next: true}, 99)
	if skipped.Seed != pvpMatchSeed(99, false, -1) || skipped.Seed == kept.Seed {
		t.Fatalf("next kept the opponent: %+v then %+v", kept, skipped)
	}
	pvp := section(asMap(t, next), "pvp")
	if pvp["nexts_used"].(float64) != 1 || pvp["nexts_day"].(float64) != float64(ruleNow.Unix()/secondsPerDay) {
		t.Fatalf("next was not counted: %v", pvp)
	}
	for range c.Pvp.Matchmaking.DailyNexts - 1 {
		next, _ = matchOpponent(t, next, skipping, PvpMatchRequest{Next: true}, 7)
	}
	if _, _, err := act(t, next, skipping, ActionPvpMatch, `{"next":true}`); refusalCode(err) != CounterLimit {
		t.Errorf("a next past the daily limit: %v", err)
	}
	forged := asMap(t, next)
	section(forged, "pvp")["nexts_used"] = 0
	uploaded := reencode(t, merge(t, next, reencode(t, forged), skipping))
	if _, _, err := act(t, uploaded, skipping, ActionPvpMatch, `{"next":true}`); refusalCode(err) != CounterLimit {
		t.Errorf("an upload reset the nexts: %v", err)
	}
	tomorrow := ruleNow.Add(24 * time.Hour)
	if _, _, err := ActWith(c, next, skipping, ActionPvpMatch, []byte(`{"next":true}`), tomorrow, Env{}); err != nil {
		t.Errorf("a next on the next day: %v", err)
	}
}

func TestPvpMatchRefusesNextForAChosenBotAndUnknownBots(t *testing.T) {
	raw := gameProfile(t, nil)
	if _, _, err := act(t, raw, &profile.ServerState{}, ActionPvpMatch, `{"bot":3,"next":true}`); refusalCode(err) != Unavailable {
		t.Errorf("next for a chosen bot: %v", err)
	}
	if _, _, err := act(t, raw, &profile.ServerState{}, ActionPvpMatch, `{"bot":-1}`); refusalCode(err) != Unavailable {
		t.Errorf("bot -1: %v", err)
	}
	bots := itoa(int64(mustCatalog(t).Battle.PvpBotCount()))
	if _, _, err := act(t, raw, &profile.ServerState{}, ActionPvpMatch, `{"bot":`+bots+`}`); refusalCode(err) != Unavailable {
		t.Errorf("a bot past the table: %v", err)
	}
}

func TestPvpEventMatchmakingReadsTheServersBannersAndClock(t *testing.T) {
	c := mustCatalog(t)
	raw := gameProfile(t, nil)
	cycle := c.Battle.PvpEventCycle(ruleNow.Unix())
	server := &profile.ServerState{PvpEvent: profile.PvpEventState{Cycle: cycle, Banners: 400}}
	_, match := matchOpponent(t, raw, server, PvpMatchRequest{Event: true}, 7)
	want := c.Battle.PvpEventOpponent(5, 400, ruleNow.Unix(), pvpMatchSeed(7, true, -1))
	if match.Bot != want.Bot || match.Squad != want.Squad || match.Seed != want.Seed || !match.Event {
		t.Fatalf("match = %+v, want %+v", match, want)
	}
	ended := ruleNow.Add(time.Duration(c.Pvp.Events.DurationSeconds) * time.Second)
	if _, _, err := ActWith(c, raw, server, ActionBattleStart, encodeBody(t, pvpStartFor(t, match)), ended,
		Env{Seeds: fixedSeed(1)}); refusalCode(err) != OpponentChanged {
		t.Errorf("an event match fought after its event ended: %v", err)
	}
}

func TestPvpChosenBotsKeepTheirTrophiesAndTheServersSeed(t *testing.T) {
	c := mustCatalog(t)
	raw, squad := veteranProfile(t)
	day := ruleNow.Unix() / battle.SecondsPerDay
	for _, event := range []bool{false, true} {
		bot := c.Battle.PvpBotCount() - 1
		server := &profile.ServerState{StoreRNG: 3}
		_, match := matchOpponent(t, raw, server, PvpMatchRequest{Event: event, Bot: &bot}, 5)
		want := c.Battle.PvpOpponentFromBot(bot, 5, day, pvpMatchSeed(5, event, bot))
		if !match.Chosen || match.Bot != bot || match.Seed != want.Seed || match.Squad != want.Squad ||
			match.Trophies != want.Trophies {
			t.Fatalf("event %v match = %+v, want %+v", event, match, want)
		}
		request := pvpStartFor(t, match)
		request.Squad = squad
		doc, result, _ := fight(t, raw, server, request, true)
		entry := section(doc, "pvp")["history"].([]any)[0].(map[string]any)
		if entry["bot"].(float64) != float64(bot) || entry["opponent_trophies"].(float64) != float64(want.Trophies) {
			t.Errorf("event %v history = %v", event, entry)
		}
		if event && result.Banners != maxBannersPerWin(&c.Pvp.Events, 0)*pvpBannerPct(&c.Pvp, 0, want.Trophies)/100 {
			t.Errorf("banners = %d against bot %d", result.Banners, bot)
		}
	}
}

func TestPvpEventBannersComeFromTheServer(t *testing.T) {
	c := mustCatalog(t)
	server := &profile.ServerState{StoreRNG: 3}
	raw, squad := strongerProfile(t)
	_, match := matchOpponent(t, raw, server, PvpMatchRequest{Event: true}, 7)
	request := pvpStartFor(t, match)
	request.Squad = squad
	_, result, _ := fight(t, raw, server, request, true)
	want := maxBannersPerWin(&c.Pvp.Events, 0) * pvpBannerPct(&c.Pvp, 0, match.Trophies) / 100
	if want <= 0 || result.Banners != want || server.PvpEvent.Banners != want {
		t.Fatalf("banners = %d, want %d", result.Banners, want)
	}
	losing := &profile.ServerState{StoreRNG: 3}
	request = pvpMatched(t, raw, losing, true, 7)
	request.Squad = squad
	_, lost, _ := fight(t, raw, losing, request, false)
	if lost.Banners != c.Pvp.Events.LossBanners {
		t.Errorf("loss banners = %d, want %d", lost.Banners, c.Pvp.Events.LossBanners)
	}
}

func TestPvpEventClaimPaysOnceAfterTheEvent(t *testing.T) {
	c := mustCatalog(t)
	raw, squad := strongerProfile(t)
	server := &profile.ServerState{StoreRNG: 3}
	request := pvpMatched(t, raw, server, true, 7)
	request.Squad = squad
	fight(t, raw, server, request, true)
	cycle := server.PvpEvent.Cycle
	if _, _, err := act(t, raw, server, ActionPvpEventClaim, `{"cycle":`+itoa(cycle)+`}`); refusalCode(err) != Unavailable {
		t.Errorf("claiming a running event: %v", err)
	}
	later := server.Clone()
	after := ruleNow.Add(time.Duration(c.Pvp.Events.DurationSeconds) * time.Second)
	next, result, err := Act(c, raw, &later, ActionPvpEventClaim, []byte(`{"cycle":`+itoa(cycle)+`}`), after)
	if err != nil {
		t.Fatal(err)
	}
	if result.EventRank < 1 || len(later.PvpResults) != 1 || !later.PvpResults[0].Claimed {
		t.Errorf("result = %+v server = %+v", result, later.PvpResults)
	}
	if _, _, err := Act(c, next, &later, ActionPvpEventClaim, []byte(`{"cycle":`+itoa(cycle)+`}`), after); refusalCode(err) != Unavailable {
		t.Errorf("second claim: %v", err)
	}
}

func strongerProfile(t *testing.T) (json.RawMessage, battle.Side) {
	t.Helper()
	raw := gameProfile(t, func(doc map[string]any) {
		section(doc, "account")["energy"] = 99
		for _, hero := range section(doc, "roster")["additions"].([]any) {
			hero.(map[string]any)["tier"] = "IV"
			hero.(map[string]any)["level"] = 30
		}
	})
	squad := fixtureSquad()
	for i := range squad.Units {
		squad.Units[i].Tier, squad.Units[i].Level = "IV", 30
	}
	return raw, squad
}

func TestNexusRunEscrowsAndAwards(t *testing.T) {
	c := mustCatalog(t)
	difficulty := c.Nexus[0].Difficulties[0]
	raw, squad := strongerProfile(t)
	server := &profile.ServerState{StoreRNG: 3}
	request := nexusStart(t, 1)
	request.Squad = squad
	doc, result, outcome := fight(t, raw, server, request, true)
	run := section(doc, "nexus", "run")
	wantEscrow := float64(int64(difficulty.Nodes[0].Gold) + creditedCritGold(outcome.Crits))
	if !result.Escrowed || run["committed"] != true || run["current_node_step"].(float64) != 2 ||
		run["escrow_gold"].(float64) != wantEscrow || number(t, doc, "account", "gold") != 1000 {
		t.Fatalf("run = %v result = %+v", run, result)
	}
	if _, _, err := startBattle(t, reencode(t, doc), server, request, 1); refusalCode(err) != Unavailable {
		t.Errorf("replayed step: %v", err)
	}
	awarded, result := mustAct(t, reencode(t, doc), server, ActionNexusAward, `{}`)
	if number(t, awarded, "account", "gold") != 1000+wantEscrow ||
		section(awarded, "nexus", "run")["committed"] != false || len(result.Granted.Heroes) != 1 {
		t.Errorf("award = %+v account = %v", result, awarded["account"])
	}
}

func TestNexusLossNeedsAPotionToContinue(t *testing.T) {
	c := mustCatalog(t)
	raw := gameProfile(t, func(doc map[string]any) { section(doc, "account")["energy"] = 99 })
	server := &profile.ServerState{StoreRNG: 3}
	request := nexusStart(t, 1)
	lost, _, _ := fight(t, raw, server, request, false)
	if c.Nexus[0].Difficulties[0].PotionCost == 0 || section(lost, "nexus", "run")["reset"] != true {
		t.Skip("first difficulty costs no potion")
	}
	if _, _, err := startBattle(t, reencode(t, lost), server, request, 1); refusalCode(err) != Unavailable {
		t.Errorf("fought on without a revive: %v", err)
	}
	request.Revive = true
	revived, _, _ := fight(t, reencode(t, lost), server, request, true)
	if number(t, revived, "nexus", "potions") != 1 {
		t.Errorf("potions = %v", section(revived, "nexus")["potions"])
	}
}

func TestEveryCampaignNodeHasARoster(t *testing.T) {
	c := mustCatalog(t)
	for m, campaignMap := range c.Campaign {
		for node := range campaignMap.Nodes {
			if _, err := campaignEnemy(c, campaignMap, node); err != nil {
				t.Errorf("campaign %d node %d: %v", m, node, err)
			}
		}
	}
	for _, event := range c.Nexus {
		for d := range event.Difficulties {
			for step := 1; step <= len(event.Difficulties[d].Nodes); step++ {
				if _, _, err := nexusEnemy(c, &event, &event.Difficulties[d], step); err != nil {
					t.Errorf("nexus %s: %v", event.ID, err)
				}
			}
		}
	}
}
