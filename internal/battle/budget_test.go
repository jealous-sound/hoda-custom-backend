package battle

import "testing"

func testBudget() *statBudget {
	return &statBudget{
		budget: 3000, healthPerPower: 5, giantBudgetPct: 150,
		patternPowerCostPct: [patternCount]int{patternOne: 100, patternRow: 200, patternColumn: 200, patternAll: 300},
		auraCostPct:         50, selfAuraSharePct: 20, auraMatchingAllies: 2, auraCapPct: 100, gearCapPct: 100,
		formationBonusPct: 5, changerCapPct: 25,
		rarityBudgetPct: [budgetRarityCount]int{
			rarityCommon: 50, rarityUncommon: 60, rarityRare: 70, rarityEpic: 85, rarityLegendary: 100,
			rarityMythic: 110,
		},
		heroPct: map[string]int{"UNIT_A": 80, "UNIT_C": 150},
	}
}

func grownHero(id string, r rarity, power, health int) *hero {
	def := &character{id: id, rarity: r}
	def.hasTier[TierI] = true
	def.stats[TierI] = tierStats{basePower: power, baseHealth: health, maxLevel: 1}
	return &hero{def: def, rarity: r, level: 1, stats: def.stats}
}

func TestStatBudgetPricesEveryGrownHeroAtOneCostAndKeepsItsShape(t *testing.T) {
	b := testBudget()
	cases := []struct {
		name                  string
		kit                   budgetKit
		power, health         int
		wantPower, wantHealth int
	}{
		{"weak", budgetKit{heroPct: neutralHeroPct}, 100, 500, 300, 1500},
		{"strong", budgetKit{heroPct: neutralHeroPct}, 2000, 20000, 200, 2000},
		{"giant", budgetKit{giant: true, heroPct: neutralHeroPct}, 100, 500, 450, 2250},
		{"statless", budgetKit{heroPct: neutralHeroPct}, 0, 0, 0, 0},
		{"hits everyone", budgetKit{pattern: patternAll, heroPct: neutralHeroPct}, 100, 500, 150, 750},
		{"aura giver", budgetKit{auraPoints: 100, heroPct: neutralHeroPct}, 100, 500, 200, 1000},
		{"handicapped up", budgetKit{heroPct: 150}, 100, 500, 450, 2250},
	}
	for _, c := range cases {
		c.kit.rarityPct = neutralRarityPct
		power, health := b.applyKit(c.kit, budgetStats{power: c.power, health: c.health}, c.power, c.health)
		if power != c.wantPower || health != c.wantHealth {
			t.Errorf("%s: %d/%d, want %d/%d", c.name, power, health, c.wantPower, c.wantHealth)
		}
	}
}

func TestStatBudgetChargesForAurasUpToTheCap(t *testing.T) {
	b := testBudget()
	passives := []passiveEffect{
		{kind: passiveStatPercent, stats: statHealth | statPower, valuePct: 40, recipient: filter{scope: scopeAllies}},
		{kind: passiveStatPercent, stats: statPower, valuePct: 100, recipient: filter{scope: scopeSelf}},
		{kind: passiveStatPercentPerMatchingAlly, stats: statHealth, valuePct: 10, recipient: filter{scope: scopeAllies}},
		{kind: passiveIncomingTargetWeight, valuePct: 500},
	}
	if points := b.auraPoints(passives); points != (80+20+20)/2 {
		t.Errorf("aura points = %d", points)
	}
	if points := b.auraPoints(nil); points != 0 {
		t.Errorf("no passives cost %d points", points)
	}
	debuff := []passiveEffect{
		{kind: passiveStatPercent, stats: statHealth | statPower, valuePct: 40, recipient: filter{scope: scopeEnemies}},
	}
	if points := b.auraPoints(debuff); points != 0 {
		t.Errorf("an enemy debuff costs %d points", points)
	}
	wide := []passiveEffect{
		{kind: passiveStatPercent, stats: statHealth | statPower, valuePct: 90, recipient: filter{scope: scopeAllies}},
	}
	b.auraCapPct = 50
	if points := b.auraPoints(wide); points != 50 {
		t.Errorf("a 90%% aura under a 50%% cap costs %d points", points)
	}
	if b.cappedAuraPct(85) != 50 || b.cappedAuraPct(20) != 20 || b.cappedAuraPct(-30) != -30 {
		t.Errorf("capped auras = %v %v %v", b.cappedAuraPct(85), b.cappedAuraPct(20), b.cappedAuraPct(-30))
	}
}

func TestStatBudgetHandicapScalesOneHeroAndCutsItsPotencyBelowNeutral(t *testing.T) {
	b := testBudget()
	heroNamed := func(id string) *hero { return grownHero(id, rarityLegendary, 100, 500) }
	if power, health := b.apply(heroNamed("UNIT_C"), 100, 500); power != 450 || health != 2250 {
		t.Errorf("a 150%% handicap gives %d/%d", power, health)
	}
	if power, health := b.apply(heroNamed("UNIT_B"), 100, 500); power != 300 || health != 1500 {
		t.Errorf("an unlisted hero gets %d/%d", power, health)
	}
	for id, want := range map[string]int{"UNIT_A": 20, "UNIT_B": 0, "UNIT_C": 0} {
		if cut := b.potencyCutPct(heroNamed(id)); cut != want {
			t.Errorf("%s potency cut = %d, want %d", id, cut, want)
		}
	}
}

func TestPotencyCutWeakensStatusChancesAndAllyGrantsOnACopy(t *testing.T) {
	recipe := &abilityRecipe{
		hit:   attackRecipe{dmgPct: 150, stunPct: 75, drainPct: 65, levyPct: 20},
		fx:    secondaryEffects{allyCritPct: 7, selfPowerPct: 25},
		steps: []recipeStep{{attack: attackRecipe{stunPct: 100}, fx: secondaryEffects{allyPowerPct: 50}}},
	}
	if recipe.withPotencyCut(0) != recipe {
		t.Error("no cut should keep the shared recipe")
	}
	cut := recipe.withPotencyCut(50)
	if cut.hit.stunPct != 38 || cut.hit.levyPct != 10 || cut.fx.allyCritPct != 4 ||
		cut.steps[0].attack.stunPct != 50 || cut.steps[0].fx.allyPowerPct != 25 {
		t.Errorf("cut recipe = %+v", cut)
	}
	if cut.hit.dmgPct != 150 || cut.hit.drainPct != 65 || cut.fx.selfPowerPct != 25 {
		t.Errorf("the cut touched stat-scaled effects: %+v", cut)
	}
	if recipe.hit.stunPct != 75 || recipe.steps[0].attack.stunPct != 100 || recipe.steps[0].fx.allyPowerPct != 50 {
		t.Errorf("the shared recipe changed: %+v", recipe)
	}
}

func TestStatBudgetKeepsTheShareOfGrowthAHeroHasEarned(t *testing.T) {
	b := testBudget()
	def := &character{id: "UNIT_GROWER", rarity: rarityLegendary, maxTier: TierII}
	def.hasTier[TierI], def.hasTier[TierII] = true, true
	def.stats[TierI] = tierStats{basePower: 100, baseHealth: 500, maxLevel: 10, powerPerLevel: 10, healthPerLevel: 50}
	def.stats[TierII] = tierStats{
		basePower: 200, baseHealth: 1000, maxLevel: 11, powerPerLevel: 20, healthPerLevel: 100,
	}
	h := &hero{def: def, rarity: def.rarity, stats: def.stats}
	if grown := h.grownStats(); grown.power != 400 || grown.health != 2000 {
		t.Fatalf("grown stats = %+v", grown)
	}
	stages := []struct {
		tier                  Tier
		level                 int
		wantPower, wantHealth int
	}{
		{TierI, 1, 75, 375}, {TierI, 10, 143, 713}, {TierII, 1, 150, 750}, {TierII, 11, 300, 1500},
		{TierII, 11 + ascensionBonusLevels, 375, 1875},
	}
	for t := range h.stats {
		h.stats[t].maxLevel += ascensionBonusLevels
	}
	if grown := h.grownStats(); grown.power != 400 || grown.health != 2000 {
		t.Fatalf("ascension moved the grown stats to %+v", grown)
	}
	for _, stage := range stages {
		h.tier, h.level = stage.tier, stage.level
		power, health := b.apply(h, h.statPower(), h.statHealth())
		if power != stage.wantPower || health != stage.wantHealth {
			t.Errorf("tier %s level %d: %d/%d, want %d/%d", stage.tier, stage.level, power, health,
				stage.wantPower, stage.wantHealth)
		}
	}
	h.tier, h.level = TierII, 11
	h.fortifyPower, h.fortifyHealth = 400, 2000
	if power, health := b.apply(h, h.statPower(), h.statHealth()); power != 600 || health != 3000 {
		t.Errorf("a fortified grown hero gets %d/%d", power, health)
	}
}

func TestStatBudgetGivesEachRarityItsShare(t *testing.T) {
	b := testBudget()
	shares := []struct {
		rarity                rarity
		wantPower, wantHealth int
	}{
		{rarityCommon, 150, 750}, {rarityUncommon, 180, 900}, {rarityRare, 210, 1050}, {rarityEpic, 255, 1275},
		{rarityLegendary, 300, 1500}, {rarityMythic, 330, 1650},
	}
	for _, share := range shares {
		power, health := b.apply(grownHero("UNIT_RANKED", share.rarity, 100, 500), 100, 500)
		if power != share.wantPower || health != share.wantHealth {
			t.Errorf("rarity %d: %d/%d, want %d/%d", share.rarity, power, health, share.wantPower, share.wantHealth)
		}
	}
	if pct := b.rarityPct(rarityMythic + 1); pct != neutralRarityPct {
		t.Errorf("an unlisted rarity gets %d%% of the budget", pct)
	}
}

func TestStatBudgetCapsGearApartFromAuras(t *testing.T) {
	b := testBudget()
	b.auraCapPct, b.gearCapPct = 50, 30
	if b.cappedGearPct(85) != 30 || b.cappedGearPct(20) != 20 || b.cappedGearPct(-30) != -30 {
		t.Errorf("capped gear = %v %v %v", b.cappedGearPct(85), b.cappedGearPct(20), b.cappedGearPct(-30))
	}
	if total := b.cappedAuraPct(85) + b.cappedGearPct(85); total != 80 {
		t.Errorf("auras and gear over both caps add %v%%", total)
	}
}

func TestStatBudgetCountsRunesFormationAndEventBuffs(t *testing.T) {
	b := testBudget()
	bonus := runeBonus{
		powerPct: 100, healthPct: 60, damageShield: 10000, damageResistPct: 75, retaliation: 5,
		initiativeSteps: 1, stunResistPct: 60, slowResistPct: 50, drainResistPct: 40,
		cursePowerImmune: true, stealth: true, perceptive: true,
	}
	bonus.targetLevel[0] = 6
	unshielded := bonus
	unshielded.damageShield = 0
	bonus.dropFlatShield()
	if bonus != unshielded {
		t.Errorf("budgeted runes = %+v, want %+v", bonus, unshielded)
	}
	if b.formationBonus() != percent(5) {
		t.Errorf("formation bonus = %v", b.formationBonus())
	}
	for pct, want := range map[int]int{300: 25, 25: 25, 10: 10, -50: -50} {
		if got := b.cappedChangerPct(pct); got != want {
			t.Errorf("an event buff of %d%% becomes %d%%, want %d%%", pct, got, want)
		}
	}
}

func TestHandicapTableNamesOnlyKnownHeroes(t *testing.T) {
	d := loadTestData(t)
	if len(d.statBudget.heroPct) == 0 {
		t.Fatal("no handicaps loaded")
	}
	for id := range d.statBudget.heroPct {
		if !d.Knows(id) {
			t.Errorf("handicap for unknown hero %s", id)
		}
	}
}

func TestBudgetedCorpusBattlesDependOnTheBudget(t *testing.T) {
	d := loadTestData(t)
	budgeted := 0
	for _, battle := range loadCorpus(t).Battles {
		if !battle.StatBudget {
			continue
		}
		budgeted++
		plain := battle.Replay
		plain.StatBudget = false
		outcome, err := d.Run(plain)
		if err != nil {
			t.Errorf("%s: %v", battle.Name, err)
			continue
		}
		if outcome.Timeline == battle.Timeline {
			t.Errorf("%s plays the same without the stat budget", battle.Name)
		}
	}
	if budgeted == 0 {
		t.Error("the corpus has no battle under the stat budget")
	}
}
