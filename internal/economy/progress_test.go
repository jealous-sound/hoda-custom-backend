package economy

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

func rosterHero(t *testing.T, doc map[string]any, instance float64) map[string]any {
	t.Helper()
	for _, list := range rosterLists {
		entries, _ := section(doc, "roster")[list].([]any)
		for _, entry := range entries {
			if hero := entry.(map[string]any); hero["instance_id"].(float64) == instance {
				return hero
			}
		}
	}
	t.Fatalf("no hero %v in %v", instance, doc["roster"])
	return nil
}

func withSquad(instances ...any) func(doc map[string]any) {
	return func(doc map[string]any) {
		cells := append(append([]any{}, instances...), make([]any, squadSlotCount-len(instances))...)
		doc["squads"] = []any{cells}
	}
}

func merge(t *testing.T, stored, upload json.RawMessage, server *profile.ServerState) map[string]any {
	t.Helper()
	merged, err := MergeUpload(mustCatalog(t), profile.Record{Profile: stored, Revision: 1}, server, upload, ruleNow)
	if err != nil {
		t.Fatal(err)
	}
	return asMap(t, merged)
}

func TestUploadCannotRaiseHeroLevelXPOrTier(t *testing.T) {
	stored := gameProfile(t, nil)
	forged := gameProfile(t, func(doc map[string]any) {
		hero := section(doc, "roster")["additions"].([]any)[0].(map[string]any)
		hero["level"], hero["xp"], hero["tier"], hero["consume_xp"] = 35, 999999, "IV", 999999
	})
	doc := merge(t, stored, forged, &profile.ServerState{})
	hero := rosterHero(t, doc, 1)
	if hero["level"].(float64) != 3 || hero["xp"].(float64) != 36 || hero["tier"] != "I" ||
		hero["consume_xp"].(float64) != 0 {
		t.Errorf("forged hero progress was kept: %v", hero)
	}
}

func TestUploadCannotDropAHeroOutsideAConsume(t *testing.T) {
	stored := gameProfile(t, func(doc map[string]any) {
		section(doc, "gear")["heroes"] = []any{map[string]any{"instance_id": 2, "ascended": false,
			"prestige_unlocked": false, "slots": []any{"templar_longsword"}}}
	})
	dropped := gameProfile(t, func(doc map[string]any) {
		section(doc, "roster")["additions"] = section(doc, "roster")["additions"].([]any)[:1]
	})
	doc := merge(t, stored, dropped, &profile.ServerState{})
	if rosterHero(t, doc, 2)["id"] != "UNIT_ARIQUN" {
		t.Error("the dropped hero was not restored")
	}
	heroes := section(doc, "gear")["heroes"].([]any)
	if len(heroes) != 1 || heroes[0].(map[string]any)["slots"].([]any)[0] != "templar_longsword" ||
		number(t, doc, "gear", "inventory", "templar_longsword") != 2 {
		t.Errorf("the restored hero lost its gear or it was duplicated: %v", doc["gear"])
	}
}

func TestBattleGrantsHeroXPToTheSquadThatFought(t *testing.T) {
	c := mustCatalog(t)
	raw := gameProfile(t, func(doc map[string]any) {
		withSquad(1, 2)(doc)
		section(doc, "roster")["additions"].([]any)[0].(map[string]any)["xp"] = 75
	})
	request := campaignStart(t, 0, 0)
	request.Squad.Units = request.Squad.Units[:1]
	doc, _, _ := fight(t, raw, &profile.ServerState{StoreRNG: 3}, request, true)
	fought := rosterHero(t, doc, 1)
	if fought["xp"].(float64) != float64(75+c.Campaign[0].Nodes[0].XP) || fought["level"].(float64) != 4 {
		t.Errorf("squad hero = %v", fought)
	}
	if benched := rosterHero(t, doc, 2); benched["xp"].(float64) != 0 || benched["level"].(float64) != 1 {
		t.Errorf("a hero in the squad who did not fight gained XP: %v", benched)
	}
}

func TestConsumeTiersUpOnTheServerAndCountsTheMissions(t *testing.T) {
	raw := gameProfile(t, func(doc map[string]any) {
		roster := section(doc, "roster")
		roster["additions"] = append(roster["additions"].([]any), map[string]any{
			"id": "UNIT_ELVENMAGE", "instance_id": 3, "tier": "I", "level": 1, "xp": 0, "ascended": false,
			"consume_xp": 0,
		})
		roster["next_instance_id"] = 4
	})
	server := &profile.ServerState{}
	doc, _ := mustAct(t, raw, server, ActionConsume, `{"target":1,"seeds":[3]}`)
	target := rosterHero(t, doc, 1)
	if target["tier"] != "II" || target["consume_xp"].(float64) != 5876 || target["xp"].(float64) != 59 ||
		target["level"].(float64) != 3 {
		t.Errorf("consumed into %v", target)
	}
	if additions := section(doc, "roster")["additions"].([]any); len(additions) != 2 {
		t.Errorf("the seed stayed in the roster: %v", additions)
	}
	if number(t, doc, "missions", "counters", consumedMissionStat) != 1 ||
		number(t, doc, "missions", "counters", combinedMissionStat) != 1 {
		t.Errorf("counters = %v", section(doc, "missions")["counters"])
	}
	for _, mission := range []string{"a3", "a4"} {
		doc, _ = mustAct(t, reencode(t, doc), server, ActionMissionClaim, `{"mission":"`+mission+`"}`)
	}
}

func TestConsumeRefusesSquadMembersAndUnknownSeeds(t *testing.T) {
	raw := gameProfile(t, withSquad(2))
	for _, body := range []string{
		`{"target":1,"seeds":[2]}`, `{"target":1,"seeds":[1]}`, `{"target":1,"seeds":[9]}`, `{"target":1,"seeds":[]}`,
		`{"target":9,"seeds":[2]}`,
	} {
		if _, _, err := act(t, raw, &profile.ServerState{}, ActionConsume, body); refusalCode(err) != Unavailable {
			t.Errorf("%s: %v", body, err)
		}
	}
	free := gameProfile(t, nil)
	if _, _, err := act(t, free, &profile.ServerState{}, ActionConsume, `{"target":1,"seeds":[2,2]}`); refusalCode(err) != Unavailable {
		t.Errorf("a seed consumed twice: %v", err)
	}
}

func TestUploadCannotForgeMissionCountersOrProgress(t *testing.T) {
	stored := gameProfile(t, nil)
	forged := gameProfile(t, func(doc map[string]any) {
		section(doc, "missions")["counters"] = map[string]any{"consumed": 500, "crits": 500, "summoned": 500}
		section(doc, "missions")["progress"] = map[string]any{"a3": 500, "a39": 500}
	})
	server := &profile.ServerState{}
	doc := merge(t, stored, forged, server)
	if counters := section(doc, "missions", "counters"); len(counters) != 0 {
		t.Errorf("forged counters were kept: %v", counters)
	}
	if progress := section(doc, "missions", "progress"); progress["a3"] != nil || progress["a39"] != nil {
		t.Errorf("forged progress was kept: %v", progress)
	}
	if _, _, err := act(t, reencode(t, doc), server, ActionMissionClaim, `{"mission":"a3"}`); refusalCode(err) != Unavailable {
		t.Errorf("claimed a3 on forged counters: %v", err)
	}
}

func TestMissionProgressIsAServerHighWaterMark(t *testing.T) {
	levelFive := gameProfile(t, func(doc map[string]any) {
		hero := section(doc, "roster")["additions"].([]any)[0].(map[string]any)
		hero["level"], hero["xp"] = 5, 137
	})
	server := &profile.ServerState{}
	_, _ = mustAct(t, levelFive, server, ActionPvpDefend, `{}`)
	if server.MissionProgress["a68"] != 5 {
		t.Fatalf("hero level progress = %v", server.MissionProgress)
	}
	if _, _, err := act(t, gameProfile(t, nil), server, ActionMissionClaim, `{"mission":"a68"}`); err != nil {
		t.Errorf("a hero level reached before was not claimable: %v", err)
	}
}

func TestClaimedMissionsStayCompleteForTheClient(t *testing.T) {
	raw := gameProfile(t, func(doc map[string]any) { section(doc, "missions")["claimed"] = []any{"a3"} })
	doc, _ := mustAct(t, raw, &profile.ServerState{}, ActionPvpDefend, `{}`)
	if number(t, doc, "missions", "progress", "a3") != 1 {
		t.Errorf("a claimed mission would load as incomplete: %v", section(doc, "missions")["progress"])
	}
}

func TestBattleFactsComeFromTheReplay(t *testing.T) {
	server := &profile.ServerState{StoreRNG: 3}
	doc, _, outcome := fight(t, gameProfile(t, nil), server, campaignStart(t, 0, 0), true)
	counter := func(name string) float64 {
		value, _ := section(doc, "missions", "counters")[name].(float64)
		return value
	}
	wantFaction := 0.0
	if outcome.FactionHit {
		wantFaction = 1
	}
	if outcome.Damage <= 0 || counter(statCrits) != float64(min(outcome.Crits, maxCreditedCrits)) ||
		counter(statBattleDamage) != float64(outcome.Damage) || counter(statFactionHits) != wantFaction ||
		counter(statBattleDays) != 1 || counter(statGoldEarned) <= 0 {
		t.Errorf("counters = %v outcome = %+v", section(doc, "missions", "counters"), outcome)
	}
	doc, _, second := fight(t, reencode(t, doc), server, campaignStart(t, 0, 0), false)
	if counter(statBattleDamage) != float64(max(outcome.Damage, second.Damage)) || counter(statBattleDays) != 1 {
		t.Errorf("a second battle the same day = %v", section(doc, "missions", "counters"))
	}
}

func TestUploadCannotForgeTrophiesOrPvpRecord(t *testing.T) {
	stored := gameProfile(t, nil)
	forged := gameProfile(t, func(doc map[string]any) {
		doc["pvp"] = map[string]any{
			"trophies": 5000, "best_trophies": 5000, "trophies_seen": 5000, "wins": 300, "losses": 0,
			"win_streak": 300, "best_win_streak": 300, "trophies_won": 5000, "defense_wins": 0, "defense_losses": 0,
			"nexts_used": 0, "nexts_day": 0, "defense_checked_at": 0, "held_since": []any{1, 1, 1, 1, 1},
			"history": []any{map[string]any{"at": 1, "bot": 0, "squad": 0, "level": 1, "opponent_trophies": 0,
				"trophy_delta": 0, "gold": 0, "xp": 0, "banners": 0, "seed": 1, "defense": true, "won": false}},
			"event": map[string]any{"cycle": 0, "banners": 0, "milestones_paid": 0}, "event_results": []any{},
		}
	})
	server := &profile.ServerState{}
	doc := merge(t, stored, forged, server)
	pvp := section(doc, "pvp")
	if pvp["trophies"].(float64) != 0 || pvp["wins"].(float64) != 0 || pvp["best_win_streak"].(float64) != 0 ||
		pvp["trophies_won"].(float64) != 0 || len(pvp["history"].([]any)) != 0 ||
		pvp["held_since"].([]any)[0].(float64) != 0 {
		t.Errorf("forged pvp record was kept: %v", pvp)
	}
	if pvp["trophies_seen"].(float64) != 5000 {
		t.Errorf("the client's seen-trophies marker was dropped: %v", pvp)
	}
	opponent, err := DuelOpponentFrom("cheater", reencode(t, doc), ruleNow)
	if err != nil || opponent.Trophies != 0 {
		t.Errorf("guild duels show %d trophies: %v", opponent.Trophies, err)
	}
	for _, mission := range []string{"a5", "a12", "a60"} {
		if _, _, err := act(t, reencode(t, doc), server, ActionMissionClaim, `{"mission":"`+mission+`"}`); refusalCode(err) != Unavailable {
			t.Errorf("claimed %s on a forged pvp record: %v", mission, err)
		}
	}
	revenge := BattleStartRequest{
		Mode: BattlePvp, RevengeAt: 1, Squad: fixtureSquad(), Opponent: PvpOpponentRequest{Kind: OpponentRevenge, Seed: 1},
	}
	if _, _, err := startBattle(t, reencode(t, doc), server, revenge, 1); refusalCode(err) != Unavailable {
		t.Errorf("revenged a defense the server never ran: %v", err)
	}
}

func TestPvpBattleKeepsTheRecordOnTheServer(t *testing.T) {
	c := mustCatalog(t)
	raw := gameProfile(t, nil)
	server := &profile.ServerState{StoreRNG: 3}
	won, result, _ := fight(t, raw, server, pvpMatched(t, raw, server, false, 42), true)
	pvp := section(won, "pvp")
	gained := pvp["trophies"].(float64)
	if gained < float64(c.Pvp.Trophies.MinGain) || gained > float64(c.Pvp.Trophies.MaxGain) ||
		result.TrophyDelta != int(gained) || pvp["wins"].(float64) != 1 || pvp["win_streak"].(float64) != 1 ||
		pvp["trophies_won"].(float64) != gained || pvp["defense_checked_at"].(float64) != float64(ruleNow.Unix()) {
		t.Errorf("after a win pvp = %v result = %+v", pvp, result)
	}
	entry := pvp["history"].([]any)[0].(map[string]any)
	if entry["trophy_delta"].(float64) != gained || entry["won"] != true || entry["seed"].(float64) != float64(pvpMatchSeed(42, false, -1)) {
		t.Errorf("history = %v", entry)
	}
	loss := pvpMatched(t, reencode(t, won), server, false, 43)
	loss.Squad = squadOf(t, won)
	lost, _, _ := fight(t, reencode(t, won), server, loss, false)
	pvp = section(lost, "pvp")
	if pvp["losses"].(float64) != 1 || pvp["win_streak"].(float64) != 0 || pvp["best_win_streak"].(float64) != 1 ||
		pvp["trophies"].(float64) >= gained || len(pvp["history"].([]any)) != 2 {
		t.Errorf("after a loss pvp = %v", pvp)
	}
	event := pvpMatched(t, reencode(t, lost), server, true, 44)
	event.Squad = squadOf(t, lost)
	afterEvent, _, _ := fight(t, reencode(t, lost), server, event, true)
	if section(afterEvent, "pvp")["trophies"] != pvp["trophies"] || section(afterEvent, "pvp")["wins"].(float64) != 2 {
		t.Errorf("an event battle moved trophies: %v", section(afterEvent, "pvp"))
	}
}

type fixedDuels struct {
	match DuelMatch
	squad DuelOpponent
}

func (d fixedDuels) DuelOpponent(string) (DuelMatch, error) {
	return d.match, nil
}

func (d fixedDuels) DuelSquad(string) (DuelOpponent, error) {
	return d.squad, nil
}

func TestDuelTrophiesComeFromTheServer(t *testing.T) {
	c := mustCatalog(t)
	duels := fixedDuels{
		match: DuelMatch{Name: "Rival", Level: 7, Trophies: 900},
		squad: DuelOpponent{ID: "rival", Units: []DuelUnit{{Slot: 0, ID: "UNIT_HURLOCKGRUNT", Tier: "I", Level: 1}}},
	}
	request := BattleStartRequest{
		Mode: BattlePvp, Duel: "rival", Squad: fixtureSquad(), Opponent: PvpOpponentRequest{Seed: 5},
		Enemy: battle.Side{Units: []battle.Unit{{Slot: 0, ID: "UNIT_HURLOCKGRUNT", Tier: "I", Level: 1}},
			Runes: []string{}},
	}
	seed, outcome := seedFor(t, request, true)
	server := &profile.ServerState{StoreRNG: 3}
	started, start, err := ActWith(c, gameProfile(t, nil), server, ActionBattleStart, encodeBody(t, request), ruleNow,
		Env{Duels: duels, Seeds: fixedSeed(seed)})
	if err != nil {
		t.Fatal(err)
	}
	claim := BattleRequest{BattleID: start.BattleID, Won: true, CritGold: battle.CritGoldFor(outcome.Crits)}
	next, _, err := finishBattle(t, started, server, claim)
	if err != nil {
		t.Fatal(err)
	}
	pvp := section(asMap(t, next), "pvp")
	entry := pvp["history"].([]any)[0].(map[string]any)
	if pvp["trophies"].(float64) != float64(trophyGain(&c.Pvp, 0, 900)) || entry["opponent"] != "Rival" ||
		entry["opponent_trophies"].(float64) != 900 || entry["level"].(float64) != 7 {
		t.Errorf("duel pvp = %v", pvp)
	}
}

func TestDefensesAndRevengeRunOnTheServer(t *testing.T) {
	c := mustCatalog(t)
	interval := c.Pvp.Defense.AttackIntervalSeconds
	checked := ruleNow.Unix() - 3*interval - interval/2
	raw := gameProfile(t, func(doc map[string]any) {
		doc["pvp"] = map[string]any{"trophies": 400, "wins": 1, "defense_checked_at": checked}
	})
	server := &profile.ServerState{StoreRNG: 3}
	doc, _ := mustAct(t, raw, server, ActionPvpDefend, `{}`)
	pvp := section(doc, "pvp")
	history := pvp["history"].([]any)
	if int64(len(history)) != c.Pvp.Defense.MaxAttacksPerLogin ||
		pvp["defense_checked_at"].(float64) != float64(checked+3*interval) ||
		pvp["defense_wins"].(float64)+pvp["defense_losses"].(float64) != float64(len(history)) {
		t.Fatalf("pvp after defending = %v", pvp)
	}
	again, _ := mustAct(t, reencode(t, doc), server, ActionPvpDefend, `{}`)
	if len(section(again, "pvp")["history"].([]any)) != len(history) {
		t.Errorf("defenses ran twice for the same interval")
	}
	var lost map[string]any
	for _, entry := range history {
		if entry := entry.(map[string]any); entry["won"] == false {
			lost = entry
		}
	}
	if lost == nil {
		t.Fatalf("no lost defense to revenge in %v", history)
	}
	opponent := battle.PvpOpponent{
		Bot: int(lost["bot"].(float64)), Squad: int(lost["squad"].(float64)), Level: int(lost["level"].(float64)),
		Seed: uint32(lost["seed"].(float64)),
	}
	enemy, err := c.Battle.PvpRoster(opponent)
	if err != nil {
		t.Fatal(err)
	}
	_, squad := strongerProfile(t)
	revenge := BattleStartRequest{
		Mode: BattlePvp, RevengeAt: int64(lost["at"].(float64)), Squad: squad, Enemy: enemy,
		Opponent: PvpOpponentRequest{Kind: OpponentRevenge, Seed: opponent.Seed},
	}
	withHeroes := reencode(t, doc)
	stronger := asMap(t, withHeroes)
	for _, hero := range section(stronger, "roster")["additions"].([]any) {
		hero.(map[string]any)["tier"], hero.(map[string]any)["level"] = "IV", 30
	}
	section(stronger, "account")["energy"] = 99
	revenged, _, _ := fight(t, reencode(t, stronger), server, revenge, true)
	attack := section(revenged, "pvp")["history"].([]any)[0].(map[string]any)
	if attack["opponent_trophies"] != lost["opponent_trophies"] || attack["level"] != lost["level"] {
		t.Errorf("revenge fought %v instead of %v", attack, lost)
	}
	if _, _, err := startBattle(t, reencode(t, revenged), server, revenge, 1); refusalCode(err) != Unavailable {
		t.Errorf("revenged twice: %v", err)
	}
}

func TestTrophiesHeldForAWeekCountOnTheServer(t *testing.T) {
	c := mustCatalog(t)
	since := ruleNow.Add(-8 * 24 * time.Hour).Unix()
	raw := gameProfile(t, func(doc map[string]any) {
		doc["pvp"] = map[string]any{"trophies": 600, "held_since": []any{since, 0, 0, 0, 0}}
	})
	if _, _, err := Act(c, raw, &profile.ServerState{}, ActionMissionClaim, []byte(`{"mission":"a60"}`), ruleNow); err != nil {
		t.Errorf("500 trophies held for 8 days: %v", err)
	}
}
