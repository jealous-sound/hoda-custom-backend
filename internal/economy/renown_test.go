package economy

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const (
	levelCapXP      = 50490
	rankElevenXP    = 61040
	rankTwelveXP    = 72590
	cappedEnergyMax = StartEnergyMax + MaxPlayerLevel - 1
	dropSeedsTried  = 3000
	mostKeysOfAKind = 99
)

func withRenown(perks map[string]any) func(doc map[string]any) {
	return func(doc map[string]any) { doc[renownSection] = map[string]any{renownPerksField: perks} }
}

func rankFor(perks map[string]any) int {
	rank := 0
	for _, bought := range perks {
		if ranks, counted := bought.(int); counted {
			rank += ranks
		}
	}
	return rank
}

func withEarnedRenown(t *testing.T, perks map[string]any) func(doc map[string]any) {
	t.Helper()
	firstLevel := mustCatalog(t).Renown.LevelsPerRank * rankFor(perks)
	return func(doc map[string]any) {
		if firstLevel > 0 {
			seasoned(min(firstLevel, MaxPlayerLevel), cumulativeXPThroughLevel(firstLevel-1))(doc)
		}
		withRenown(perks)(doc)
	}
}

func seasoned(level int, experience int64) func(doc map[string]any) {
	return func(doc map[string]any) {
		account := section(doc, "account")
		account["level"], account["experience"] = level, experience
	}
}

func withAccount(fields map[string]any) func(doc map[string]any) {
	return func(doc map[string]any) {
		for name, value := range fields {
			section(doc, "account")[name] = value
		}
	}
}

func edited(t *testing.T, edits ...func(doc map[string]any)) json.RawMessage {
	t.Helper()
	return gameProfile(t, func(doc map[string]any) {
		for _, edit := range edits {
			edit(doc)
		}
	})
}

func accountOf(t *testing.T, raw json.RawMessage) *account {
	t.Helper()
	doc, err := parseDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	a, err := loadAccount(doc, &mustCatalog(t).Renown)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func storedPerks(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	perks, ok := section(doc, renownSection)[renownPerksField].(map[string]any)
	if !ok {
		t.Fatalf("renown = %v", doc[renownSection])
	}
	return perks
}

func TestRenownRankIsTheVirtualLevelInTens(t *testing.T) {
	rules := &mustCatalog(t).Renown
	cases := []struct {
		xp          int64
		level, rank int
	}{
		{-5, 1, 0}, {0, 1, 0}, {19, 1, 0}, {20, 2, 0}, {539, 9, 0}, {540, 10, 1}, {2089, 19, 1}, {2090, 20, 2},
		{levelCapXP - 1, 99, 9}, {levelCapXP, 100, 10}, {rankElevenXP - 1, 109, 10}, {rankElevenXP, 110, 11},
		{rankTwelveXP - 1, 119, 11}, {rankTwelveXP, 120, 12},
	}
	for _, c := range cases {
		if level, rank := virtualLevel(c.xp), RenownRank(rules, c.xp); level != c.level || rank != c.rank {
			t.Errorf("%d xp is virtual level %d rank %d, want level %d rank %d", c.xp, level, rank, c.level, c.rank)
		}
	}
	for level := 2; level <= 400; level++ {
		reached := cumulativeXPThroughLevel(level - 1)
		if virtualLevel(reached) != level || virtualLevel(reached-1) != level-1 {
			t.Fatalf("virtual level %d does not start at %d xp", level, reached)
		}
	}
	if virtualLevel(math.MaxInt64) != virtualLevel(math.MaxInt32) {
		t.Error("experience past the saturation point still raises the virtual level")
	}
}

func TestStoredExperienceBoundsSpanTheRankAtTheLevelCap(t *testing.T) {
	cases := []struct {
		level            int
		experience       int64
		wantMin, wantMax float64
	}{
		{5, 160, 140, 200},
		{MaxPlayerLevel - 1, levelCapXP - 1, 49490, levelCapXP},
		{MaxPlayerLevel, levelCapXP, levelCapXP, rankElevenXP},
		{MaxPlayerLevel, rankElevenXP - 1, levelCapXP, rankElevenXP},
		{MaxPlayerLevel, rankElevenXP, rankElevenXP, rankTwelveXP},
	}
	for _, c := range cases {
		doc, _ := mustAct(t, edited(t, seasoned(c.level, c.experience)), &profile.ServerState{}, ActionPvpDefend, `{}`)
		if number(t, doc, "account", "level") != float64(c.level) ||
			number(t, doc, "account", "experience") != float64(c.experience) ||
			number(t, doc, "account", "experience_min") != c.wantMin ||
			number(t, doc, "account", "experience_max") != c.wantMax {
			t.Errorf("level %d with %d xp is stored as %v, want bounds %v-%v", c.level, c.experience, doc["account"],
				c.wantMin, c.wantMax)
		}
	}
}

func TestRenownUpgradeSpendsAFreePointAndRaisesTheMaxima(t *testing.T) {
	rules := mustCatalog(t).Renown
	energyPerRank := float64(rules.PerkPerRank[catalog.RenownEnergyMax])
	staminaPerRank := float64(rules.PerkPerRank[catalog.RenownStaminaMax])
	raw := edited(t, seasoned(MaxPlayerLevel, levelCapXP), withAccount(map[string]any{
		"energy": 7, "energy_max": cappedEnergyMax, "stamina": 3,
	}))
	server := &profile.ServerState{}
	upgrade := func(perk string) map[string]any {
		t.Helper()
		doc, _ := mustAct(t, raw, server, ActionRenownUpgrade, `{"perk":"`+perk+`"}`)
		raw = reencode(t, doc)
		return doc
	}
	expectPools := func(doc map[string]any, energyMax, staminaMax float64) {
		t.Helper()
		if number(t, doc, "account", "energy_max") != energyMax || number(t, doc, "account", "stamina_max") != staminaMax ||
			number(t, doc, "account", "energy") != 7 || number(t, doc, "account", "stamina") != 3 {
			t.Errorf("account = %v, want maxima %v and %v around 7 energy and 3 stamina", doc["account"], energyMax,
				staminaMax)
		}
	}

	doc := upgrade(catalog.RenownEnergyMax)
	if perks := storedPerks(t, doc); len(perks) != 1 || perks[catalog.RenownEnergyMax].(float64) != 1 {
		t.Errorf("perks = %v", perks)
	}
	expectPools(doc, cappedEnergyMax+energyPerRank, StartStaminaMax)

	upgrade(catalog.RenownStaminaMax)
	doc = upgrade(catalog.RenownStaminaMax)
	expectPools(doc, cappedEnergyMax+energyPerRank, StartStaminaMax+2*staminaPerRank)

	doc = upgrade(catalog.RenownPowerPct)
	perks := storedPerks(t, doc)
	if len(perks) != 3 || perks[catalog.RenownEnergyMax].(float64) != 1 ||
		perks[catalog.RenownStaminaMax].(float64) != 2 || perks[catalog.RenownPowerPct].(float64) != 1 {
		t.Errorf("perks = %v", perks)
	}
	expectPools(doc, cappedEnergyMax+energyPerRank, StartStaminaMax+2*staminaPerRank)
	if number(t, doc, "account", "gems") != 200 || number(t, doc, "account", "gold") != 1000 {
		t.Errorf("an upgrade cost currency: %v", doc["account"])
	}
}

func TestRenownUpgradeRefusesUnknownPerksFullPerksAndNoFreePoint(t *testing.T) {
	onePointLeft := edited(t, seasoned(MaxPlayerLevel, levelCapXP), withRenown(map[string]any{
		catalog.RenownPowerPct: 5, catalog.RenownHealthPct: 4,
	}))
	server := &profile.ServerState{}
	for _, body := range []string{`{"perk":"luck_pct"}`, `{"perk":"tier_ranks"}`, `{"perk":""}`, `{}`} {
		if _, _, err := act(t, onePointLeft, server, ActionRenownUpgrade, body); refusalCode(err) != Unavailable {
			t.Errorf("%s: %v", body, err)
		}
	}
	_, _, err := act(t, onePointLeft, server, ActionRenownUpgrade, `{"perk":"power_pct"}`)
	if refusalCode(err) != AlreadyFull {
		t.Errorf("a perk at its last rank: %v", err)
	}
	doc, _ := mustAct(t, onePointLeft, server, ActionRenownUpgrade, `{"perk":"health_pct"}`)
	spent := reencode(t, doc)
	if _, _, err := act(t, spent, server, ActionRenownUpgrade, `{"perk":"gold_pct"}`); refusalCode(err) != CantAfford {
		t.Errorf("an upgrade with every point spent: %v", err)
	}
	if _, _, err := act(t, spent, server, ActionRenownUpgrade, `{"perk":"health_pct"}`); refusalCode(err) != AlreadyFull {
		t.Errorf("a full perk with every point spent: %v", err)
	}

	unranked := edited(t, seasoned(9, 539))
	if _, _, err := act(t, unranked, server, ActionRenownUpgrade, `{"perk":"gold_pct"}`); refusalCode(err) != CantAfford {
		t.Errorf("an upgrade below the first rank: %v", err)
	}
	firstRank := edited(t, seasoned(10, 540))
	doc, _ = mustAct(t, firstRank, server, ActionRenownUpgrade, `{"perk":"gold_pct"}`)
	_, _, err = act(t, reencode(t, doc), server, ActionRenownUpgrade, `{"perk":"key_pct"}`)
	if refusalCode(err) != CantAfford {
		t.Errorf("a second upgrade at rank one: %v", err)
	}
}

func timerOf(t *testing.T, a *account, name string) float64 {
	t.Helper()
	value, err := a.doc.floatField(name)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestExperiencePastTheLevelCapKeepsGrowingAndRanksUp(t *testing.T) {
	rules := mustCatalog(t).Renown
	const energyPerkRank = 2
	energyPerk := int64(energyPerkRank * rules.PerkPerRank[catalog.RenownEnergyMax])
	a := accountOf(t, edited(t, seasoned(MaxPlayerLevel, rankElevenXP-5),
		withRenown(map[string]any{catalog.RenownEnergyMax: energyPerkRank}),
		withAccount(map[string]any{
			"energy": 4, "energy_max": cappedEnergyMax + energyPerk, "stamina": 2, "stamina_max": 9,
			energyRegenTimer: 55, staminaRegenTimer: 66,
		})))

	a.awardBattleXP(3)
	if a.Experience != rankElevenXP-2 || a.Level != MaxPlayerLevel || a.Gems != 200 || a.Energy != 4 || a.Stamina != 2 ||
		timerOf(t, a, energyRegenTimer) != 55 || timerOf(t, a, staminaRegenTimer) != 66 {
		t.Errorf("xp inside a rank changed more than the experience: %+v", a)
	}

	a.awardBattleXP(5)
	if a.Experience != rankElevenXP+3 || a.Level != MaxPlayerLevel {
		t.Errorf("experience %d at level %d, want %d past the cap", a.Experience, a.Level, rankElevenXP+3)
	}
	if a.Gems != 200+int64(rules.RankGems) {
		t.Errorf("a rank-up left %d gems, want %d", a.Gems, 200+rules.RankGems)
	}
	if a.EnergyMax != cappedEnergyMax+energyPerk || a.Energy != a.EnergyMax || a.StaminaMax != 9 || a.Stamina != 9 ||
		timerOf(t, a, energyRegenTimer) != 0 || timerOf(t, a, staminaRegenTimer) != 0 {
		t.Errorf("a rank-up did not refill: %+v", a)
	}
}

func TestBattleXPPaysEveryRankCrossedAndRefillsOnLevelOrRank(t *testing.T) {
	rules := mustCatalog(t).Renown
	gems := func(ranks int) int64 { return 200 + int64(ranks*rules.RankGems) }
	cases := []struct {
		name                 string
		level                int
		experience, award    int64
		wantLevel            int
		wantExperience       int64
		wantGems             int64
		wantRefill           bool
		wantEnergyMaxIfRefil int64
	}{
		{"three ranks at once", MaxPlayerLevel, levelCapXP, cumulativeXPThroughLevel(129) - levelCapXP,
			MaxPlayerLevel, cumulativeXPThroughLevel(129), gems(3), true, cappedEnergyMax},
		{"the first rank below the cap", 9, 539, 1, 10, 540, gems(1), true, energyMaxForLevel(10)},
		{"a level without a rank", 5, 199, 1, 6, 200, gems(0), true, energyMaxForLevel(6)},
		{"reaching the cap", MaxPlayerLevel - 1, levelCapXP - 1, 1, MaxPlayerLevel, levelCapXP, gems(1), true,
			cappedEnergyMax},
		{"no level and no rank", 5, 160, 39, 5, 199, gems(0), false, 0},
		{"no experience", 5, 160, 0, 5, 160, gems(0), false, 0},
		{"the saturation point", MaxPlayerLevel, math.MaxInt32 - 1, 10, MaxPlayerLevel, math.MaxInt32, gems(0), false, 0},
	}
	for _, c := range cases {
		a := accountOf(t, edited(t, seasoned(c.level, c.experience), withAccount(map[string]any{
			"energy": 1, "energy_max": 500, "stamina": 1, energyRegenTimer: 55, staminaRegenTimer: 66,
		})))
		a.awardBattleXP(c.award)
		if a.Level != c.wantLevel || a.Experience != c.wantExperience || a.Gems != c.wantGems {
			t.Errorf("%s: level %d with %d xp and %d gems, want level %d with %d xp and %d gems", c.name, a.Level,
				a.Experience, a.Gems, c.wantLevel, c.wantExperience, c.wantGems)
		}
		refilled := a.Energy == a.EnergyMax && a.Stamina == a.StaminaMax && timerOf(t, a, energyRegenTimer) == 0 &&
			timerOf(t, a, staminaRegenTimer) == 0
		untouched := a.Energy == 1 && a.EnergyMax == 500 && a.Stamina == 1 && timerOf(t, a, energyRegenTimer) == 55 &&
			timerOf(t, a, staminaRegenTimer) == 66
		if c.wantRefill && (!refilled || a.EnergyMax != c.wantEnergyMaxIfRefil) {
			t.Errorf("%s: no refill to %d energy: %+v", c.name, c.wantEnergyMaxIfRefil, a)
		}
		if !c.wantRefill && !untouched {
			t.Errorf("%s: energy or stamina moved: %+v", c.name, a)
		}
	}
}

func TestABattleAtTheLevelCapPaysTheRankUp(t *testing.T) {
	c := mustCatalog(t)
	node := c.Campaign[0].Nodes[0]
	capped := withAccount(map[string]any{"energy": 50, "energy_max": cappedEnergyMax, "stamina": 1})

	ranking := edited(t, seasoned(MaxPlayerLevel, rankElevenXP-1), capped)
	doc, result, _ := fight(t, ranking, &profile.ServerState{StoreRNG: 3}, campaignStart(t, 0, 0), true)
	if result.LevelsUp != 0 || number(t, doc, "account", "level") != MaxPlayerLevel ||
		number(t, doc, "account", "experience") != float64(rankElevenXP-1+node.XP) ||
		number(t, doc, "account", "experience_min") != rankElevenXP ||
		number(t, doc, "account", "experience_max") != rankTwelveXP {
		t.Errorf("result = %+v account = %v", result, doc["account"])
	}
	if number(t, doc, "account", "gems") != float64(200+node.FirstWinGems+c.Renown.RankGems) ||
		number(t, doc, "account", "energy") != cappedEnergyMax || number(t, doc, "account", "stamina") != StartStaminaMax {
		t.Errorf("a rank-up in battle paid and refilled %v", doc["account"])
	}

	settled := edited(t, seasoned(MaxPlayerLevel, levelCapXP), capped)
	doc, _, _ = fight(t, settled, &profile.ServerState{StoreRNG: 3}, campaignStart(t, 0, 0), true)
	if number(t, doc, "account", "experience") != float64(levelCapXP+node.XP) ||
		number(t, doc, "account", "gems") != float64(200+node.FirstWinGems) ||
		number(t, doc, "account", "energy") != float64(50-node.EnergyCost) || number(t, doc, "account", "stamina") != 1 {
		t.Errorf("a battle inside a rank left %v", doc["account"])
	}
}

type paidBattle struct {
	doc    map[string]any
	result BattleResult
}

func TestRenownGoldAndHeroXPPerksRaiseEveryBattlePayout(t *testing.T) {
	c := mustCatalog(t)
	const goldRank, heroXPRank = 2, 3
	goldPct := c.Renown.PerkValue(catalog.RenownGoldPct, goldRank)
	heroXPPct := c.Renown.PerkValue(catalog.RenownHeroXPPct, heroXPRank)
	bought := map[string]any{catalog.RenownGoldPct: goldRank, catalog.RenownHeroXPPct: heroXPRank}
	perks := withEarnedRenown(t, bought)
	unspent := func(doc map[string]any) {
		perks(doc)
		withRenown(map[string]any{})(doc)
	}
	startXP := cumulativeXPThroughLevel(c.Renown.LevelsPerRank*rankFor(bought) - 1)
	stronger := func(doc map[string]any) {
		section(doc, "account")["energy"] = 99
		for _, hero := range section(doc, "roster")["additions"].([]any) {
			hero.(map[string]any)["tier"], hero.(map[string]any)["level"] = "IV", 30
		}
	}
	_, strongerSquad := strongerProfile(t)
	asStored := func(map[string]any) {}
	modes := []struct {
		name     string
		won      bool
		escrowed bool
		prepare  func(doc map[string]any)
		request  func(raw json.RawMessage, server *profile.ServerState) BattleStartRequest
	}{
		{"campaign", true, false, asStored, func(json.RawMessage, *profile.ServerState) BattleStartRequest {
			return campaignStart(t, 0, 0)
		}},
		{"pvp", false, false, asStored, func(raw json.RawMessage, server *profile.ServerState) BattleStartRequest {
			return pvpMatched(t, raw, server, false, 42)
		}},
		{"nexus", true, true, stronger, func(json.RawMessage, *profile.ServerState) BattleStartRequest {
			request := nexusStart(t, 1)
			request.Squad = strongerSquad
			return request
		}},
	}
	for _, mode := range modes {
		play := func(edits ...func(doc map[string]any)) paidBattle {
			t.Helper()
			raw := edited(t, edits...)
			server := &profile.ServerState{StoreRNG: 3}
			doc, result, _ := fight(t, raw, server, mode.request(raw, server), mode.won)
			return paidBattle{doc: doc, result: result}
		}
		plain, perked := play(mode.prepare, unspent), play(mode.prepare, perks)

		wantGold := applyPct(plain.result.Gold, goldPct)
		if plain.result.Gold <= 0 || perked.result.Gold != wantGold || wantGold <= plain.result.Gold {
			t.Errorf("%s: %d gold with the perk and %d without, want %d", mode.name, perked.result.Gold,
				plain.result.Gold, wantGold)
		}
		banked, escrow := number(t, perked.doc, "account", "gold"), number(t, perked.doc, "nexus", "run", "escrow_gold")
		if mode.escrowed && (banked != 1000 || escrow != float64(wantGold)) {
			t.Errorf("%s: %v gold banked and %v in escrow, want %d in escrow", mode.name, banked, escrow, wantGold)
		}
		if !mode.escrowed && banked != float64(1000+wantGold) {
			t.Errorf("%s: %v gold banked, want %d", mode.name, banked, 1000+wantGold)
		}

		if plain.result.XP <= 0 || perked.result.XP != plain.result.XP ||
			number(t, perked.doc, "account", "experience") != number(t, plain.doc, "account", "experience") ||
			number(t, perked.doc, "account", "experience") != float64(startXP+plain.result.XP) {
			t.Errorf("%s: the hero perk moved the account's %d xp: %d and %v", mode.name, plain.result.XP,
				perked.result.XP, section(perked.doc, "account")["experience"])
		}
		if mode.escrowed && number(t, perked.doc, "nexus", "run", "escrow_xp") != float64(plain.result.XP) {
			t.Errorf("%s: escrowed xp = %v, want %d", mode.name, section(perked.doc, "nexus", "run")["escrow_xp"],
				plain.result.XP)
		}
		wantExtra := float64(applyPct(plain.result.XP, heroXPPct) - plain.result.XP)
		extra := rosterHero(t, perked.doc, 1)["xp"].(float64) - rosterHero(t, plain.doc, 1)["xp"].(float64)
		if wantExtra <= 0 || extra != wantExtra {
			t.Errorf("%s: the squad's hero got %v more xp from the perk, want %v", mode.name, extra, wantExtra)
		}
	}
}

func TestRenownGoldPerkJoinsTheRuneGoldBonusInOnePercentage(t *testing.T) {
	c := mustCatalog(t)
	node := c.Campaign[0].Nodes[0]
	greed, _ := c.RuneByID("rune-greed-1")
	knowledge, _ := c.RuneByID("rune-knowledge-1")
	const goldRank, heroXPRank = 2, 3
	goldPct := c.Renown.PerkValue(catalog.RenownGoldPct, goldRank)
	heroXPPct := c.Renown.PerkValue(catalog.RenownHeroXPPct, heroXPRank)
	bought := map[string]any{catalog.RenownGoldPct: goldRank, catalog.RenownHeroXPPct: heroXPRank}
	startXP := cumulativeXPThroughLevel(c.Renown.LevelsPerRank*rankFor(bought) - 1)
	raw := edited(t, withEarnedRenown(t, bought),
		func(doc map[string]any) {
			runes := section(doc, "runes")
			runes["squads"].([]any)[0].(map[string]any)["slots"] = []any{1, 2, 0}
			items := runes["items"].([]any)
			items[1].(map[string]any)["def"] = knowledge.ID
			for _, item := range items[:2] {
				item.(map[string]any)["expire_at"] = ruleNow.Unix() + 600
			}
		})
	p, err := openPlayer(c, raw, &profile.ServerState{StoreRNG: 3}, ruleNow)
	if err != nil {
		t.Fatal(err)
	}
	if gold, xp := p.payoutBonus(); greed.GoldPct <= 0 || knowledge.XPPct <= 0 || gold != greed.GoldPct+goldPct ||
		xp != knowledge.XPPct {
		t.Fatalf("payout bonus = %d%% gold and %d%% xp, want %d%% and %d%%", gold, xp, greed.GoldPct+goldPct,
			knowledge.XPPct)
	}

	doc, result, outcome := fight(t, raw, &profile.ServerState{StoreRNG: 3}, campaignStart(t, 0, 0), true)
	wantGold := applyPct(int64(node.Gold)+creditedCritGold(outcome.Crits), greed.GoldPct+goldPct)
	wantXP := applyPct(int64(node.XP), knowledge.XPPct)
	if result.Gold != wantGold || number(t, doc, "account", "gold") != float64(1000+wantGold) {
		t.Errorf("gold = %d, want %d from one %d%% bonus", result.Gold, wantGold, greed.GoldPct+goldPct)
	}
	if result.XP != wantXP || number(t, doc, "account", "experience") != float64(startXP+wantXP) {
		t.Errorf("account xp = %d, want %d from the rune alone", result.XP, wantXP)
	}
	wantHeroXP := float64(applyPct(wantXP, heroXPPct))
	if recruit := rosterHero(t, doc, 2); recruit["xp"].(float64) != wantHeroXP || wantHeroXP <= float64(wantXP) {
		t.Errorf("hero xp = %v, want %v on top of the rune's %d", recruit["xp"], wantHeroXP, wantXP)
	}
}

type winDrops struct {
	rngAfter uint32
	voucher  int
	key      int
}

func contractWinDrops(seed uint32, voucherKinds, voucherPerkPct, keyPerkPct int) winDrops {
	r := rng{state: seed}
	drops := winDrops{voucher: noRoll, key: noRoll}
	if voucherKinds > 0 && r.below(percent) < voucherWinDropPct+voucherPerkPct {
		drops.voucher = r.below(voucherKinds)
	}
	if keyPerkPct > 0 && r.below(percent) < keyPerkPct {
		drops.key = r.below(catalog.NexusKeyCount)
	}
	drops.rngAfter = r.state
	return drops
}

func rolledWinDrops(t *testing.T, c *catalog.Catalog, raw json.RawMessage, seed uint32) (winDrops, []int64) {
	t.Helper()
	p, err := openPlayer(c, raw, &profile.ServerState{StoreRNG: seed}, ruleNow)
	if err != nil {
		t.Fatal(err)
	}
	keysBefore := nexusKeys(t, p.doc)
	if err := p.awardWinDrops(); err != nil {
		t.Fatal(err)
	}
	drops := winDrops{rngAfter: p.rng.state, voucher: noRoll, key: noRoll}
	if len(p.granted.Vouchers) == 1 {
		drops.voucher = p.granted.Vouchers[0]
	}
	keys := nexusKeys(t, p.doc)
	for index := range keys {
		if keys[index] != keysBefore[index] {
			drops.key = index
		}
	}
	return drops, keys
}

func nexusKeys(t *testing.T, doc document) []int64 {
	t.Helper()
	nexus, err := doc.section("nexus")
	if err != nil {
		t.Fatal(err)
	}
	items, err := nexus.array("keys")
	if err != nil {
		t.Fatal(err)
	}
	keys, err := intArray(items, "keys")
	if err != nil || len(keys) != catalog.NexusKeyCount {
		t.Fatalf("keys = %v: %v", items, err)
	}
	return keys
}

func TestWinDropsRollTheVoucherThenTheKeyOnOneStream(t *testing.T) {
	c := mustCatalog(t)
	const perkRank = 5
	voucherPerk := c.Renown.PerkValue(catalog.RenownVoucherPct, perkRank)
	keyPerk := c.Renown.PerkValue(catalog.RenownKeyPct, perkRank)
	lucky := edited(t, withEarnedRenown(t, map[string]any{catalog.RenownVoucherPct: perkRank, catalog.RenownKeyPct: perkRank}))
	plain := gameProfile(t, nil)
	vouchers, widened, keys, plainVouchers := 0, 0, 0, 0
	for seed := uint32(1); seed <= dropSeedsTried; seed++ {
		want := contractWinDrops(seed, len(c.Vouchers), voucherPerk, keyPerk)
		got, _ := rolledWinDrops(t, c, lucky, seed)
		if got != want {
			t.Fatalf("seed %d with both perks: %+v, want %+v", seed, got, want)
		}
		unperked := contractWinDrops(seed, len(c.Vouchers), 0, 0)
		if got, _ := rolledWinDrops(t, c, plain, seed); got != unperked || got.key != noRoll {
			t.Fatalf("seed %d without perks: %+v, want %+v", seed, got, unperked)
		}
		if want.voucher != noRoll {
			vouchers++
		}
		if want.voucher != noRoll && unperked.voucher == noRoll {
			widened++
		}
		if want.key != noRoll {
			keys++
		}
		if unperked.voucher != noRoll {
			plainVouchers++
		}
	}
	if widened == 0 || keys == 0 || plainVouchers == 0 || vouchers != plainVouchers+widened {
		t.Errorf("%d vouchers (%d from the perk, %d without it) and %d keys in %d wins", vouchers, widened,
			plainVouchers, keys, dropSeedsTried)
	}
}

func TestAPlayerWithoutTheKeyPerkKeepsTheOldDropStream(t *testing.T) {
	c := mustCatalog(t)
	plain := gameProfile(t, nil)
	for seed := uint32(1); seed <= dropSeedsTried; seed++ {
		before := rng{state: seed}
		dropped := before.below(percent) < voucherWinDropPct
		if dropped {
			before.below(len(c.Vouchers))
		}
		got, _ := rolledWinDrops(t, c, plain, seed)
		if got.rngAfter != before.state || (got.voucher != noRoll) != dropped || got.key != noRoll {
			t.Fatalf("seed %d: %+v after a win without perks, want the stream at %d", seed, got, before.state)
		}
	}
}

func TestTheKeyRollStandsWithoutVoucherKindsAndStopsAtTheKeyLimit(t *testing.T) {
	shipped := mustCatalog(t)
	const perkRank = 5
	keyPerk := shipped.Renown.PerkValue(catalog.RenownKeyPct, perkRank)
	withoutVouchers := *shipped
	withoutVouchers.Vouchers = nil
	full := []any{mostKeysOfAKind, mostKeysOfAKind, mostKeysOfAKind, mostKeysOfAKind}
	lucky := edited(t, withEarnedRenown(t, map[string]any{catalog.RenownKeyPct: perkRank}))
	hoarder := edited(t, withEarnedRenown(t, map[string]any{catalog.RenownKeyPct: perkRank}), func(doc map[string]any) {
		section(doc, "nexus")["keys"] = full
	})
	dropped := 0
	for seed := uint32(1); seed <= dropSeedsTried; seed++ {
		want := contractWinDrops(seed, 0, 0, keyPerk)
		got, keys := rolledWinDrops(t, &withoutVouchers, lucky, seed)
		if got != want || got.voucher != noRoll {
			t.Fatalf("seed %d without voucher kinds: %+v, want %+v", seed, got, want)
		}
		if want.key == noRoll {
			continue
		}
		dropped++
		for index, count := range keys {
			if (index == want.key) != (count == 1) {
				t.Fatalf("seed %d: keys = %v, want one of key %d", seed, keys, want.key)
			}
		}
		capped, keys := rolledWinDrops(t, shipped, hoarder, seed)
		atLimit := contractWinDrops(seed, len(shipped.Vouchers), 0, keyPerk)
		if capped.rngAfter != atLimit.rngAfter || capped.key != noRoll {
			t.Fatalf("seed %d at the key limit: %+v, want the stream at %d", seed, capped, atLimit.rngAfter)
		}
		for _, count := range keys {
			if count != mostKeysOfAKind {
				t.Fatalf("seed %d: keys past the limit: %v", seed, keys)
			}
		}
	}
	if dropped == 0 {
		t.Fatalf("no key in %d wins", dropSeedsTried)
	}
}

func firstSeedWhere(t *testing.T, wanted func(drops winDrops) bool, voucherKinds, voucherPerkPct, keyPerkPct int) (
	uint32, winDrops,
) {
	t.Helper()
	for seed := uint32(1); seed <= dropSeedsTried; seed++ {
		if drops := contractWinDrops(seed, voucherKinds, voucherPerkPct, keyPerkPct); wanted(drops) {
			return seed, drops
		}
	}
	t.Fatal("no seed rolls the wanted drops")
	return 0, winDrops{}
}

func TestACampaignWinDropsTheKeyTheStreamRolls(t *testing.T) {
	c := mustCatalog(t)
	const perkRank = 5
	voucherPerk := c.Renown.PerkValue(catalog.RenownVoucherPct, perkRank)
	keyPerk := c.Renown.PerkValue(catalog.RenownKeyPct, perkRank)
	raw := edited(t, withEarnedRenown(t, map[string]any{catalog.RenownVoucherPct: perkRank, catalog.RenownKeyPct: perkRank}))
	wanted := []func(drops winDrops) bool{
		func(drops winDrops) bool { return drops.key != noRoll && drops.voucher == noRoll },
		func(drops winDrops) bool { return drops.key != noRoll && drops.voucher != noRoll },
		func(drops winDrops) bool { return drops.key == noRoll && drops.voucher != noRoll },
	}
	for _, want := range wanted {
		seed, drops := firstSeedWhere(t, want, len(c.Vouchers), voucherPerk, keyPerk)
		server := &profile.ServerState{StoreRNG: seed}
		doc, result, _ := fight(t, raw, server, campaignStart(t, 0, 0), true)
		if server.StoreRNG != drops.rngAfter {
			t.Errorf("seed %d: the stream is at %d after the win, want %d", seed, server.StoreRNG, drops.rngAfter)
		}
		for index, count := range section(doc, "nexus")["keys"].([]any) {
			if (index == drops.key) != (count.(float64) == 1) {
				t.Errorf("seed %d: keys = %v, want key %d", seed, section(doc, "nexus")["keys"], drops.key)
			}
		}
		if granted := result.Granted.Vouchers; (drops.voucher == noRoll) != (len(granted) == 0) ||
			(len(granted) == 1 && granted[0] != drops.voucher) {
			t.Errorf("seed %d: vouchers = %v, want %d", seed, granted, drops.voucher)
		}
	}
	lost := &profile.ServerState{StoreRNG: 7}
	fight(t, raw, lost, campaignStart(t, 0, 0), false)
	if lost.StoreRNG != 7 {
		t.Errorf("a loss rolled for drops: the stream moved to %d", lost.StoreRNG)
	}
}

func TestANexusWinRollsTheKeyBeforeTheHeroReward(t *testing.T) {
	c := mustCatalog(t)
	const perkRank = 5
	keyPerk := c.Renown.PerkValue(catalog.RenownKeyPct, perkRank)
	rewards := c.Nexus[0].Difficulties[0].Nodes[0].RewardRarity
	_, squad := strongerProfile(t)
	raw := edited(t, withEarnedRenown(t, map[string]any{catalog.RenownKeyPct: perkRank}), func(doc map[string]any) {
		section(doc, "account")["energy"] = 99
		for _, hero := range section(doc, "roster")["additions"].([]any) {
			hero.(map[string]any)["tier"], hero.(map[string]any)["level"] = "IV", 30
		}
	})
	rarityRolledFrom := func(state uint32) (int, uint32) {
		stream := rng{state: state}
		rarity := rewards[stream.below(len(rewards))]
		return rarity, stream.state
	}
	seed, drops, wantRarity := uint32(0), winDrops{}, 0
	for candidate := uint32(1); candidate <= dropSeedsTried && seed == 0; candidate++ {
		withKey := contractWinDrops(candidate, len(c.Vouchers), 0, keyPerk)
		rarityAfterKey, _ := rarityRolledFrom(withKey.rngAfter)
		rarityBeforeKey, _ := rarityRolledFrom(contractWinDrops(candidate, len(c.Vouchers), 0, 0).rngAfter)
		if withKey.key != noRoll && rarityAfterKey != rarityBeforeKey {
			seed, drops, wantRarity = candidate, withKey, rarityAfterKey
		}
	}
	if seed == 0 {
		t.Fatal("no seed tells the key roll from the hero reward roll")
	}
	_, streamAfterReward := rarityRolledFrom(drops.rngAfter)

	request := nexusStart(t, 1)
	request.Squad = squad
	server := &profile.ServerState{StoreRNG: seed}
	doc, _, _ := fight(t, raw, server, request, true)
	units := section(doc, "nexus", "run")["escrow_units"].([]any)
	for rarity, count := range units {
		if (rarity == wantRarity) != (count.(float64) == 1) {
			t.Errorf("escrowed heroes = %v, want one of rarity %d", units, wantRarity)
		}
	}
	if section(doc, "nexus")["keys"].([]any)[drops.key].(float64) != 1 || server.StoreRNG != streamAfterReward {
		t.Errorf("keys = %v and the stream at %d, want key %d and the stream at %d", section(doc, "nexus")["keys"],
			server.StoreRNG, drops.key, streamAfterReward)
	}
}

func TestBattleStartFieldsTheAttackersRenown(t *testing.T) {
	c := mustCatalog(t)
	const powerRank, healthRank, critRank = 2, 1, 3
	want := battle.NewSideRenown(
		c.Renown.PerkValue(catalog.RenownPowerPct, powerRank), c.Renown.PerkValue(catalog.RenownHealthPct, healthRank),
		c.Renown.PerkValue(catalog.RenownCritPermille, critRank),
	)
	fighter := edited(t, withEarnedRenown(t, map[string]any{
		catalog.RenownPowerPct: powerRank, catalog.RenownHealthPct: healthRank, catalog.RenownCritPermille: critRank,
		catalog.RenownGoldPct: 4,
	}))
	claim := func(renown *battle.SideRenown) BattleStartRequest {
		request := campaignStart(t, 0, 0)
		request.Squad.Renown = renown
		return request
	}

	server := &profile.ServerState{}
	if _, _, err := startBattle(t, fighter, server, claim(want), 1); err != nil {
		t.Fatalf("the squad with its renown: %v", err)
	}
	if fielded := server.Battle.Replay.Player.Renown; fielded == nil || *fielded != *want {
		t.Errorf("the pending battle fields renown %+v, want %+v", fielded, want)
	}
	if server.Battle.Replay.Enemy.Renown != nil {
		t.Errorf("a campaign enemy fights with renown %+v", server.Battle.Replay.Enemy.Renown)
	}
	doc, _, _ := fight(t, fighter, &profile.ServerState{StoreRNG: 3}, claim(want), true)
	if number(t, doc, "account", "gold") <= 1000 {
		t.Errorf("a renowned win paid nothing: %v", doc["account"])
	}

	stronger := *want
	stronger.PowerPct++
	for name, renown := range map[string]*battle.SideRenown{"no renown": nil, "more renown": &stronger} {
		if _, _, err := startBattle(t, fighter, &profile.ServerState{}, claim(renown), 1); refusalCode(err) != SquadChanged {
			t.Errorf("a squad claiming %s: %v", name, err)
		}
	}

	plain := gameProfile(t, nil)
	if _, _, err := startBattle(t, plain, &profile.ServerState{}, claim(want), 1); refusalCode(err) != SquadChanged {
		t.Errorf("renown the player never bought: %v", err)
	}
	unperked := &profile.ServerState{}
	if _, _, err := startBattle(t, plain, unperked, claim(&battle.SideRenown{}), 1); err != nil {
		t.Errorf("a squad sending an empty renown: %v", err)
	}
	if unperked.Battle.Replay.Player.Renown != nil {
		t.Errorf("an empty renown is kept as %+v", unperked.Battle.Replay.Player.Renown)
	}
	boosted := campaignStart(t, 0, 0)
	boosted.Enemy.Renown = want
	if _, _, err := startBattle(t, plain, &profile.ServerState{}, boosted, 1); refusalCode(err) != OpponentChanged {
		t.Errorf("a campaign enemy claimed with renown: %v", err)
	}
}

func TestADuelOpponentCarriesTheRenownRankItsExperienceEarned(t *testing.T) {
	c := mustCatalog(t)
	const rank = 4
	firstLevel := c.Renown.LevelsPerRank * rank
	for name, sent := range map[string]struct {
		xp   int64
		rank int
	}{
		"the first level of a rank": {cumulativeXPThroughLevel(firstLevel - 1), rank},
		"the level before it":       {cumulativeXPThroughLevel(firstLevel-1) - 1, rank - 1},
		"a new account":             {0, 0},
	} {
		defender := asMap(t, grownDefender(t))
		seasoned(firstLevel, sent.xp)(defender)
		opponent := duelOpponent(t, reencode(t, defender))
		wire := string(encodeBody(t, opponent))
		ranked := strings.Contains(wire, fmt.Sprintf(`"renown_rank":%d`, sent.rank))
		if opponent.RenownRank != sent.rank || ranked != (sent.rank > 0) || strings.Contains(wire, `"renown_rank":0`) {
			t.Errorf("a defender at %s is sent as %s, want renown rank %d", name, wire, sent.rank)
		}
	}
}

func TestADuelIsFoughtAgainstTheDefendersRenown(t *testing.T) {
	c := mustCatalog(t)
	const powerRank, critRank = 5, 1
	want := battle.NewSideRenown(
		c.Renown.PerkValue(catalog.RenownPowerPct, powerRank), 0, c.Renown.PerkValue(catalog.RenownCritPermille, critRank),
	)
	defender := asMap(t, grownDefender(t))
	withEarnedRenown(t, map[string]any{
		catalog.RenownPowerPct: powerRank, catalog.RenownCritPermille: critRank, catalog.RenownGoldPct: 3,
	})(defender)
	opponent := duelOpponent(t, reencode(t, defender))
	if opponent.Renown == nil || *opponent.Renown != *want {
		t.Fatalf("the duel opponent carries renown %+v, want %+v", opponent.Renown, want)
	}
	wire := string(encodeBody(t, opponent))
	if !strings.Contains(wire, `"renown":{"power_pct":10,"health_pct":0,"crit_permille":5}`) {
		t.Errorf("the duel opponent is sent as %s", wire)
	}

	claimed := sideOnTheWire(t, opponent)
	server, err := startDuel(t, opponent, claimed)
	if err != nil {
		t.Fatal(err)
	}
	fought := server.Battle.Replay
	if fought.Enemy.Renown == nil || *fought.Enemy.Renown != *want || fought.Player.Renown != nil {
		t.Errorf("the duel fields renown %+v against the attacker's %+v", fought.Enemy.Renown, fought.Player.Renown)
	}
	stripped := claimed
	stripped.Renown = nil
	if _, err := startDuel(t, opponent, stripped); refusalCode(err) != OpponentChanged {
		t.Errorf("a defender stripped of its renown was accepted: %v", err)
	}
	bare, err := c.Battle.Run(battle.Replay{Seed: duelSeed, Player: fixtureSquad(), Enemy: stripped})
	if err != nil {
		t.Fatal(err)
	}
	renowned, err := c.Battle.Run(fought)
	if err != nil {
		t.Fatal(err)
	}
	if fought.Seed != duelSeed || renowned.Timeline == bare.Timeline {
		t.Errorf("the defender's renown changed nothing in the duel")
	}

	for name, perks := range map[string]map[string]any{
		"no perks": {}, "perks outside the battle": {catalog.RenownGoldPct: 5, catalog.RenownEnergyMax: 5},
	} {
		peaceful := asMap(t, grownDefender(t))
		withEarnedRenown(t, perks)(peaceful)
		opponent := duelOpponent(t, reencode(t, peaceful))
		if opponent.Renown != nil || strings.Contains(string(encodeBody(t, opponent)), `"renown":`) {
			t.Errorf("a defender with %s is sent with renown %+v", name, opponent.Renown)
		}
	}
	if opponent := duelOpponent(t, grownDefender(t)); opponent.Renown != nil {
		t.Errorf("a defender without a renown section carries %+v", opponent.Renown)
	}
}

func TestAProfileWithoutARenownSectionHasNoPerks(t *testing.T) {
	rules := &mustCatalog(t).Renown
	shapes := map[string]func(doc map[string]any){
		"no section":       func(map[string]any) {},
		"an empty section": func(doc map[string]any) { doc[renownSection] = map[string]any{} },
		"no perks":         withRenown(map[string]any{}),
	}
	for name, shape := range shapes {
		raw := edited(t, seasoned(MaxPlayerLevel, levelCapXP), shape)
		doc, err := parseDocument(raw)
		if err != nil {
			t.Fatal(err)
		}
		perks := loadRenown(rules, doc)
		for _, perk := range catalog.RenownPerks {
			if perks.value(perk) != 0 {
				t.Errorf("%s: perk %s is worth %d", name, perk, perks.value(perk))
			}
		}
		if perks.spent() != 0 || perks.freePoints(levelCapXP) != 10 || perks.sideBonus() != nil {
			t.Errorf("%s: %d points spent, %d free, side bonus %+v", name, perks.spent(), perks.freePoints(levelCapXP),
				perks.sideBonus())
		}
		a := accountOf(t, raw)
		if energyMaxWith(a.Level, a.renown) != cappedEnergyMax || staminaMaxWith(a.renown) != StartStaminaMax {
			t.Errorf("%s: maxima %d and %d", name, energyMaxWith(a.Level, a.renown), staminaMaxWith(a.renown))
		}
		if _, _, err := startBattle(t, raw, &profile.ServerState{}, campaignStart(t, 0, 0), 1); err != nil {
			t.Errorf("%s: a battle with the bare squad: %v", name, err)
		}
	}

	untouched, _ := mustAct(t, gameProfile(t, nil), &profile.ServerState{}, ActionPvpDefend, `{}`)
	if _, added := untouched[renownSection]; added {
		t.Errorf("an action wrote a renown section into an old profile: %v", untouched[renownSection])
	}
	upgraded, _ := mustAct(t, edited(t, seasoned(MaxPlayerLevel, levelCapXP)), &profile.ServerState{},
		ActionRenownUpgrade, `{"perk":"key_pct"}`)
	if perks := storedPerks(t, upgraded); perks[catalog.RenownKeyPct].(float64) != 1 {
		t.Errorf("the first upgrade of an old profile stored %v", perks)
	}
}

func malformedRenownSections() map[string]any {
	return map[string]any{
		"a list for a section": []any{}, "a list for perks": map[string]any{renownPerksField: []any{}},
		"text for a section": "max",
		"a named rank":       map[string]any{renownPerksField: map[string]any{catalog.RenownGoldPct: "two"}},
		"a fractional rank":  map[string]any{renownPerksField: map[string]any{catalog.RenownGoldPct: 1.5}},
		"a section of null":  nil,
		"a null perk":        map[string]any{renownPerksField: map[string]any{catalog.RenownGoldPct: nil}},
		"a null perks field": map[string]any{renownPerksField: nil},
	}
}

func storing(section any) func(doc map[string]any) {
	return func(doc map[string]any) { doc[renownSection] = section }
}

func TestStoredPerkRanksAreReadWithinTheirLimits(t *testing.T) {
	rules := &mustCatalog(t).Renown
	read := func(stored any) *renown {
		doc, err := parseDocument(edited(t, storing(stored)))
		if err != nil {
			t.Fatal(err)
		}
		return loadRenown(rules, doc)
	}
	perks := read(map[string]any{renownPerksField: map[string]any{
		catalog.RenownPowerPct: 9, catalog.RenownHealthPct: -3, catalog.RenownGoldPct: 2, "luck_pct": 4,
		catalog.RenownKeyPct: "five", catalog.RenownVoucherPct: 2.5,
	}})
	if perks.ranks[catalog.RenownPowerPct] != rules.PerkMaxRank || perks.ranks[catalog.RenownHealthPct] != 0 ||
		perks.spent() != rules.PerkMaxRank+2 || perks.value("luck_pct") != 0 || perks.value(catalog.RenownKeyPct) != 0 ||
		perks.value(catalog.RenownVoucherPct) != 0 {
		t.Errorf("ranks = %v", perks.ranks)
	}
	for name, stored := range malformedRenownSections() {
		if perks := read(stored); perks.spent() != 0 || perks.sideBonus() != nil {
			t.Errorf("%s is read as ranks %v", name, perks.ranks)
		}
	}
}

func TestAMalformedStoredRenownSectionRefusesNothing(t *testing.T) {
	c := mustCatalog(t)
	for name, stored := range malformedRenownSections() {
		raw := edited(t, seasoned(MaxPlayerLevel, levelCapXP), storing(stored))
		if _, _, err := act(t, raw, &profile.ServerState{}, ActionPvpDefend, `{}`); err != nil {
			t.Errorf("%s: an action: %v", name, err)
		}
		if _, _, err := startBattle(t, raw, &profile.ServerState{}, campaignStart(t, 0, 0), 1); err != nil {
			t.Errorf("%s: a battle with the bare squad: %v", name, err)
		}
		upload := edited(t, seasoned(MaxPlayerLevel, levelCapXP))
		current := profile.Record{Profile: raw, Revision: 1}
		if _, err := MergeUpload(c, current, &profile.ServerState{}, upload, ruleNow); err != nil {
			t.Errorf("%s: an upload over it: %v", name, err)
		}
		defender := asMap(t, grownDefender(t))
		storing(stored)(defender)
		opponent, err := DuelOpponentFrom(c, "defender", reencode(t, defender), ruleNow)
		if err != nil || opponent.Renown != nil || len(opponent.Units) == 0 {
			t.Errorf("%s: as a duel defender it is %+v: %v", name, opponent, err)
		}
		doc, _, err := act(t, raw, &profile.ServerState{}, ActionRenownUpgrade, `{"perk":"power_pct"}`)
		if err != nil {
			t.Errorf("%s: an upgrade: %v", name, err)
			continue
		}
		if perks := storedPerks(t, doc); len(perks) != 1 || perks[catalog.RenownPowerPct].(float64) != 1 {
			t.Errorf("%s: the upgrade stored %v", name, doc[renownSection])
		}
	}
}

func TestUploadCannotForgeRenownAndIsNeverRefusedForIt(t *testing.T) {
	c := mustCatalog(t)
	earned := map[string]any{catalog.RenownEnergyMax: 1, catalog.RenownStaminaMax: 1}
	stored := edited(t, seasoned(MaxPlayerLevel, 60000), withRenown(earned), withAccount(map[string]any{
		"experience_min": levelCapXP, "experience_max": rankElevenXP, "energy_max": cappedEnergyMax + 3, "stamina_max": 9,
	}))
	staleClient := withAccount(map[string]any{
		"experience_min": levelCapXP, "experience_max": levelCapXP, "energy_max": 999, "stamina_max": 99,
	})
	uploads := map[string]func(doc map[string]any){
		"forged perks": withRenown(map[string]any{catalog.RenownPowerPct: 5, catalog.RenownEnergyMax: 5}),
		"no section":   func(doc map[string]any) { delete(doc, renownSection) },
		"a string":     func(doc map[string]any) { doc[renownSection] = "max" },
		"null":         func(doc map[string]any) { doc[renownSection] = nil },
		"listed perks": func(doc map[string]any) { doc[renownSection] = map[string]any{renownPerksField: []any{5}} },
		"named ranks":  withRenown(map[string]any{catalog.RenownPowerPct: "five"}),
	}
	for name, forge := range uploads {
		upload := edited(t, seasoned(MaxPlayerLevel, 60000), withRenown(earned), staleClient, forge)
		if _, err := profile.Validate(upload); err != nil {
			t.Errorf("%s: the upload is not valid: %v", name, err)
		}
		server := &profile.ServerState{}
		merged, err := MergeUpload(c, profile.Record{Profile: stored, Revision: 1}, server, upload, ruleNow)
		if err != nil {
			t.Errorf("%s: the upload was refused: %v", name, err)
			continue
		}
		doc := asMap(t, merged)
		perks := storedPerks(t, doc)
		if len(perks) != 2 || perks[catalog.RenownEnergyMax].(float64) != 1 ||
			perks[catalog.RenownStaminaMax].(float64) != 1 {
			t.Errorf("%s: perks after the upload = %v", name, perks)
		}
		if number(t, doc, "account", "experience") != 60000 || number(t, doc, "account", "experience_min") != levelCapXP ||
			number(t, doc, "account", "experience_max") != rankElevenXP ||
			number(t, doc, "account", "energy_max") != cappedEnergyMax+3 || number(t, doc, "account", "stamina_max") != 9 {
			t.Errorf("%s: account after the upload = %v", name, doc["account"])
		}
		if _, _, err := act(t, merged, server, ActionRenownUpgrade, `{"perk":"gold_pct"}`); err != nil {
			t.Errorf("%s: an upgrade after the upload: %v", name, err)
		}
	}
}

func TestPerksBeyondTheEarnedRankCountForNothing(t *testing.T) {
	overspent := map[string]any{catalog.RenownPowerPct: 5, catalog.RenownHealthPct: 5, catalog.RenownEnergyMax: 1}
	forged := edited(t, seasoned(MaxPlayerLevel, levelCapXP), withRenown(overspent))
	a := accountOf(t, forged)
	if a.renown.spent() != 0 || a.renown.freePoints(a.Experience) != 10 || a.renown.sideBonus() != nil {
		t.Fatalf("11 points of perks at rank 10 count as %d spent with bonus %+v", a.renown.spent(), a.renown.sideBonus())
	}
	defender := asMap(t, grownDefender(t))
	seasoned(MaxPlayerLevel, levelCapXP)(defender)
	withRenown(overspent)(defender)
	if opponent := duelOpponent(t, reencode(t, defender)); opponent.Renown != nil {
		t.Errorf("a defender with perks beyond its rank fights with %+v", opponent.Renown)
	}

	doc, _ := mustAct(t, forged, &profile.ServerState{}, ActionRenownUpgrade, `{"perk":"gold_pct"}`)
	if perks := storedPerks(t, doc); len(perks) != 1 || perks[catalog.RenownGoldPct] != float64(1) {
		t.Errorf("an upgrade over perks beyond the rank stored %v", perks)
	}
	if got := number(t, doc, "account", "energy_max"); got != cappedEnergyMax {
		t.Errorf("energy_max = %v after dropping the forged perk, want %d", got, cappedEnergyMax)
	}

	earned := map[string]any{catalog.RenownPowerPct: 5, catalog.RenownHealthPct: 5}
	if a := accountOf(t, edited(t, seasoned(MaxPlayerLevel, levelCapXP), withRenown(earned))); a.renown.spent() != 10 {
		t.Errorf("perks worth exactly the rank count as %d spent, want 10", a.renown.spent())
	}
}

func TestOldAndNewProfilesStartWithoutBoughtPerks(t *testing.T) {
	c := mustCatalog(t)
	forged := edited(t, withRenown(map[string]any{catalog.RenownPowerPct: 5}))

	first, err := MergeUpload(c, profile.Record{}, &profile.ServerState{}, forged, ruleNow)
	if err != nil {
		t.Fatal(err)
	}
	if perks := storedPerks(t, asMap(t, first)); len(perks) != 0 {
		t.Errorf("a new player starts with perks %v", perks)
	}

	beforeRenown := gameProfile(t, nil)
	server := &profile.ServerState{}
	doc := merge(t, beforeRenown, forged, server)
	if kept, _ := doc[renownSection].(map[string]any); len(kept) != 0 {
		t.Errorf("an upload gave a profile from before renown %v", doc[renownSection])
	}
	if _, _, err := startBattle(t, reencode(t, doc), server, campaignStart(t, 0, 0), 1); err != nil {
		t.Errorf("a battle after that upload: %v", err)
	}
	if _, err := profile.Validate(beforeRenown); err != nil {
		t.Errorf("a profile from before renown is not valid: %v", err)
	}
}
