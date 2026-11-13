package battle

import (
	"encoding/json"
	"fmt"
)

const (
	StatBudgetFile       = "stat_budget.json"
	StatBudgetHeroesFile = "stat_budget_heroes.json"

	statBudgetVersion       = 6
	statBudgetHeroesVersion = 1
	neutralHeroPct          = 100
	neutralRarityPct        = 100
	minHeroPct              = 33
	maxHeroPct              = 300
	auraStatCount           = 2
	fullSharePct            = 100
	worthPerPct             = 100
	worthOfAHero            = 100 * worthPerPct
	patternCount            = int(patternAll) + 1
	budgetRarityCount       = int(rarityMythic) + 1
)

var statBudgetPatternNames = [patternCount]string{
	patternOne: "One", patternRow: "Row", patternColumn: "Column", patternAll: "All",
}

var statBudgetRarityNames = [budgetRarityCount]string{
	rarityCommon: "Common", rarityUncommon: "Uncommon", rarityRare: "Rare", rarityEpic: "Epic",
	rarityLegendary: "Legendary", rarityMythic: "Mythic",
}

type statBudget struct {
	scaleStats             bool
	budget                 int
	healthPerPower         int
	giantBudgetPct         int
	patternPowerCostPct    [patternCount]int
	auraCostPct            int
	selfAuraSharePct       int
	auraMatchingAllies     int
	auraCapPct             int
	gearCapPct             int
	formationBonusPct      int
	changerCapPct          int
	runeStatSharePct       int
	runeInitiativeSharePct int
	flatResistPerPct       int
	gearItemCapPct         int
	gearTriggerCapPct      int
	rarityBudgetPct        [budgetRarityCount]int
	heroPct                map[string]int
}

type budgetKit struct {
	giant      bool
	pattern    targetPattern
	auraPoints int
	heroPct    int
	rarityPct  int
}

type budgetStats struct {
	power  int
	health int
}

func loadStatBudget(data []byte) (*statBudget, error) {
	var raw struct {
		Version                int            `json:"version"`
		ScaleStats             *bool          `json:"scale_stats"`
		Budget                 int            `json:"budget"`
		HealthPerPower         int            `json:"health_per_power"`
		GiantBudgetPct         int            `json:"giant_budget_pct"`
		PatternPowerCostPct    map[string]int `json:"pattern_power_cost_pct"`
		AuraCostPct            int            `json:"aura_cost_pct"`
		SelfAuraSharePct       int            `json:"self_aura_share_pct"`
		AuraMatchingAllies     int            `json:"aura_matching_allies"`
		AuraCapPct             int            `json:"aura_cap_pct"`
		GearCapPct             int            `json:"gear_cap_pct"`
		FormationBonusPct      int            `json:"formation_bonus_pct_per_hero"`
		ChangerCapPct          int            `json:"changer_cap_pct"`
		RuneStatSharePct       int            `json:"rune_stat_share_pct"`
		RuneInitiativeSharePct int            `json:"rune_initiative_share_pct"`
		FlatResistPerPct       int            `json:"flat_resist_per_pct"`
		GearItemCapPct         int            `json:"gear_item_cap_pct"`
		GearTriggerCapPct      int            `json:"gear_trigger_cap_pct"`
		RarityBudgetPct        map[string]int `json:"rarity_budget_pct"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("stat budget: %w", err)
	}
	if raw.Version != statBudgetVersion {
		return nil, fmt.Errorf("stat budget: version %d", raw.Version)
	}
	if raw.ScaleStats == nil {
		return nil, fmt.Errorf("stat budget: scale_stats is missing")
	}
	b := &statBudget{
		scaleStats: *raw.ScaleStats,
		budget:     raw.Budget, healthPerPower: raw.HealthPerPower, giantBudgetPct: raw.GiantBudgetPct,
		auraCostPct: raw.AuraCostPct, selfAuraSharePct: raw.SelfAuraSharePct,
		auraMatchingAllies: raw.AuraMatchingAllies, auraCapPct: raw.AuraCapPct, gearCapPct: raw.GearCapPct,
		formationBonusPct: raw.FormationBonusPct, changerCapPct: raw.ChangerCapPct,
		runeStatSharePct: raw.RuneStatSharePct, runeInitiativeSharePct: raw.RuneInitiativeSharePct,
		flatResistPerPct: raw.FlatResistPerPct, gearItemCapPct: raw.GearItemCapPct,
		gearTriggerCapPct: raw.GearTriggerCapPct, heroPct: map[string]int{},
	}
	valid := b.budget > 0 && b.healthPerPower > 0 && b.giantBudgetPct > 0 && b.auraCostPct >= 0 &&
		b.selfAuraSharePct >= 0 && b.auraMatchingAllies >= 0 && b.auraCapPct > 0 && b.gearCapPct >= 0 &&
		b.formationBonusPct >= 0 && b.changerCapPct >= 0 && b.runeStatSharePct >= 0 &&
		b.runeInitiativeSharePct >= 0 && b.flatResistPerPct > 0 && b.gearItemCapPct >= 0 &&
		b.gearTriggerCapPct >= 0
	for pattern, name := range statBudgetPatternNames {
		b.patternPowerCostPct[pattern] = raw.PatternPowerCostPct[name]
		valid = valid && b.patternPowerCostPct[pattern] > 0
	}
	for r := rarityCommon; r <= rarityMythic; r++ {
		b.rarityBudgetPct[r] = raw.RarityBudgetPct[statBudgetRarityNames[r]]
		valid = valid && b.rarityBudgetPct[r] > 0
	}
	if !valid {
		return nil, fmt.Errorf("stat budget: a rule is missing or not positive")
	}
	return b, nil
}

func (b *statBudget) loadHeroes(data []byte) error {
	var raw struct {
		Version int            `json:"version"`
		Heroes  map[string]int `json:"heroes"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("stat budget heroes: %w", err)
	}
	if raw.Version != statBudgetHeroesVersion {
		return fmt.Errorf("stat budget heroes: version %d", raw.Version)
	}
	for id, pct := range raw.Heroes {
		if pct < minHeroPct || pct > maxHeroPct {
			return fmt.Errorf("stat budget heroes: %s has %d%%, outside %d-%d", id, pct, minHeroPct, maxHeroPct)
		}
	}
	b.heroPct = raw.Heroes
	return nil
}

func (b *statBudget) heroPctFor(id string) int {
	if pct, ok := b.heroPct[id]; ok {
		return pct
	}
	return neutralHeroPct
}

func effectStatCount(effect *passiveEffect) int {
	if effect.kind == passiveBonusDamagePercent {
		return 1
	}
	return bit(effect.stats&statHealth != 0) + bit(effect.stats&statPower != 0)
}

func (b *statBudget) effectPoints(effect *passiveEffect) int {
	perAlly := effect.kind == passiveStatPercentPerMatchingAlly
	if effect.kind != passiveStatPercent && effect.kind != passiveBonusDamagePercent && !perAlly {
		return 0
	}
	points := lround(effect.valuePct) * effectStatCount(effect)
	if perAlly {
		points *= b.auraMatchingAllies
	}
	switch effect.recipient.scope {
	case scopeEnemies:
		return -points
	case scopeSelf:
		return points * b.selfAuraSharePct / 100
	}
	return points
}

func (b *statBudget) auraPoints(passives []passiveEffect) int {
	points := 0
	for i := range passives {
		points += b.effectPoints(&passives[i])
	}
	points /= auraStatCount
	return min(max(points, 0), b.auraCapPct)
}

func (b *statBudget) auraChargePct(auraPoints int) int {
	return 100 + auraPoints*b.auraCostPct/100
}

func (b *statBudget) rarityPct(r rarity) int {
	if r >= rarityCommon && r <= rarityMythic {
		return b.rarityBudgetPct[r]
	}
	return neutralRarityPct
}

func (c *character) grownTier(current Tier) Tier {
	if c.hasTier[c.maxTier] {
		return c.maxTier
	}
	return current
}

func (h *hero) grownStats() budgetStats {
	st := &h.def.stats[h.def.grownTier(h.tier)]
	steps := max(st.maxLevel, 1) - 1
	return budgetStats{
		power:  st.basePower + steps*st.powerPerLevel,
		health: st.baseHealth + steps*st.healthPerLevel,
	}
}

func (b *statBudget) applyKit(kit budgetKit, grown budgetStats, power, health int) (int, int) {
	costPct := int64(grown.power)*int64(b.healthPerPower)*int64(b.patternPowerCostPct[kit.pattern]) +
		int64(grown.health)*100
	if costPct <= 0 {
		return power, health
	}
	sizePct := 100
	if kit.giant {
		sizePct = b.giantBudgetPct
	}
	numerator := int64(b.budget) * int64(sizePct) * int64(kit.heroPct) * int64(kit.rarityPct)
	denominator := costPct * int64(b.auraChargePct(kit.auraPoints)) * neutralRarityPct
	scaled := func(stat int) int { return int((int64(stat)*numerator + denominator/2) / denominator) }
	return scaled(power), scaled(health)
}

func (b *statBudget) ifScaling() *statBudget {
	if b != nil && b.scaleStats {
		return b
	}
	return nil
}

func trimmedStat(stat, heroPct int) int {
	return int((int64(stat)*int64(heroPct) + neutralHeroPct/2) / neutralHeroPct)
}

func (b *statBudget) apply(h *hero, power, health int) (int, int) {
	if !b.scaleStats {
		heroPct := b.heroPctFor(h.def.id)
		return trimmedStat(power, heroPct), trimmedStat(health, heroPct)
	}
	kit := budgetKit{
		giant: h.def.giant, pattern: h.def.ability.target, auraPoints: b.auraPoints(h.passives),
		heroPct: b.heroPctFor(h.def.id), rarityPct: b.rarityPct(h.rarity),
	}
	return b.applyKit(kit, h.grownStats(), power, health)
}

func (b *statBudget) potencyCutPct(h *hero) int {
	if pct := b.heroPctFor(h.def.id); pct < neutralHeroPct {
		return neutralHeroPct - pct
	}
	return 0
}

func (b *statBudget) passiveCutPct(h *hero) int {
	if b.scaleStats {
		return 0
	}
	return b.potencyCutPct(h)
}

func (b *statBudget) cappedAuraPct(auraPct float32) float32 {
	if limit := float32(b.auraCapPct); auraPct > limit {
		return limit
	}
	return auraPct
}

func (b *statBudget) cappedGearPct(gearPct float32) float32 {
	limit := float32(b.gearCapPct)
	if gearPct < -limit {
		return -limit
	}
	if gearPct > limit {
		return limit
	}
	return gearPct
}

func (b *statBudget) gearTriggerAllowance() int64 {
	return int64(b.gearTriggerCapPct) * worthPerPct
}

func shareOfStat(amount int64, stat int) int64 {
	if stat <= 0 {
		return 0
	}
	return max(amount, -amount) * worthOfAHero / int64(stat)
}

func worthOfPoints(points int64) int64 {
	return max(points, -points) * worthPerPct
}

func (b *statBudget) flatResistPct(flatResist int) int {
	return flatResist / b.flatResistPerPct
}

func (b *statBudget) formationBonus() float32 {
	return percent(b.formationBonusPct)
}

func (b *statBudget) cappedChangerPct(changerPct int) int {
	return min(changerPct, b.changerCapPct)
}

func runeShare(runeValue, sharePct int) int {
	return (runeValue*sharePct + fullSharePct/2) / fullSharePct
}

func (b *statBudget) limitRuneBonus(bonus *runeBonus) {
	bonus.damageShield = 0
	bonus.damageResistPct = runeShare(bonus.damageResistPct, b.runeStatSharePct)
}

func (b *statBudget) runeStat(stat, runePct int) int {
	if stat <= 0 || runePct <= 0 {
		return stat
	}
	whole := int64(100 * fullSharePct)
	return int((int64(stat)*(whole+int64(runePct)*int64(b.runeStatSharePct)) + whole/2) / whole)
}

func (b *statBudget) runeInitiative(initiative int) int {
	return runeShare(initiative, b.runeInitiativeSharePct)
}

func cutPct(pct, cut int) int {
	return (pct*(100-cut) + 50) / 100
}

func (a *attackRecipe) cutPotency(cut int) {
	a.stunPct = cutPct(a.stunPct, cut)
	a.slowPct = cutPct(a.slowPct, cut)
	a.cursePowerPct = cutPct(a.cursePowerPct, cut)
	a.curseHealthPct = cutPct(a.curseHealthPct, cut)
	a.levyPct = cutPct(a.levyPct, cut)
	a.assassinatePct = cutPct(a.assassinatePct, cut)
	a.markPct = cutPct(a.markPct, cut)
}

func (fx *secondaryEffects) cutAllyPotency(cut int) {
	fx.allyHealPct = cutPct(fx.allyHealPct, cut)
	fx.allyPowerPct = cutPct(fx.allyPowerPct, cut)
	fx.allyCritPct = cutPct(fx.allyCritPct, cut)
	fx.allyDamageBonusPct = cutPct(fx.allyDamageBonusPct, cut)
}

func (r *abilityRecipe) withPotencyCut(cut int) *abilityRecipe {
	if cut <= 0 {
		return r
	}
	weakened := *r
	weakened.hit.cutPotency(cut)
	weakened.effect.cutPotency(cut)
	weakened.followup.cutPotency(cut)
	weakened.fx.cutAllyPotency(cut)
	weakened.steps = append([]recipeStep(nil), r.steps...)
	for i := range weakened.steps {
		weakened.steps[i].attack.cutPotency(cut)
		weakened.steps[i].fx.cutAllyPotency(cut)
	}
	return &weakened
}
