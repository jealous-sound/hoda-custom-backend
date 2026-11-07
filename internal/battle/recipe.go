package battle

import (
	"encoding/json"
	"fmt"
)

type recipeTarget int

const (
	recipeTargetAbility recipeTarget = iota
	recipeTargetOne
	recipeTargetRow
	recipeTargetColumn
	recipeTargetAll
	recipeTargetOneAlly
	recipeTargetBackRow
	recipeTargetAny
)

var recipeTargetNames = map[string]recipeTarget{
	"one": recipeTargetOne, "row": recipeTargetRow, "column": recipeTargetColumn, "all": recipeTargetAll,
	"one_ally": recipeTargetOneAlly, "back_row": recipeTargetBackRow, "any": recipeTargetAny,
}

type attackRecipe struct {
	dmgPct         int
	stunPct        int
	slowPct        int
	slowAmt        int
	drainPct       int
	drainChance    int
	cursePowerPct  int
	curseHealthPct int
	curseTurns     int
	levyPct        int
	assassinatePct int
	markPct        int
	selfHealPct    int
	selfCritPct    int
	target         recipeTarget
	statusOnly     bool
}

type secondaryEffects struct {
	selfPowerPct          int
	selfPowerChance       int
	selfHealPct           int
	selfHealChance        int
	selfDamageBonusPct    int
	selfDamagePct         int
	allyHealPct           int
	allyHealChance        int
	allyPowerPct          int
	allyPowerChance       int
	allyPowerFromActorPct int
	allySlowAmt           int
	allyCritPct           int
	allyDamageBonusPct    int
	allyDamagePct         int
}

type recipeStep struct {
	attack         attackRecipe
	chancePct      int
	repeat         int
	chanceGroup    int
	finalDamagePct int
	stopOnFail     bool
	effectsOnce    bool
	selfCritPct    int
	fx             secondaryEffects
}

type abilityRecipe struct {
	hit                    attackRecipe
	fx                     secondaryEffects
	effectPct              int
	effect                 attackRecipe
	followupPct            int
	followupCount          int
	followupDamageStepPct  int
	followupFinalDamagePct int
	followup               attackRecipe
	generated              bool
	steps                  []recipeStep
}

type rawAttack struct {
	Target         string `json:"target"`
	DamagePct      int    `json:"damage_pct"`
	StunPct        int    `json:"stun_pct"`
	SlowPct        int    `json:"slow_pct"`
	SlowAmt        int    `json:"slow_amt"`
	DrainPct       int    `json:"drain_pct"`
	DrainChance    int    `json:"drain_chance"`
	CursePowerPct  int    `json:"curse_power_pct"`
	CurseHealthPct int    `json:"curse_health_pct"`
	CurseTurns     int    `json:"curse_turns"`
	LevyPct        int    `json:"levy_pct"`
	AssassinatePct int    `json:"assassinate_pct"`
	MarkPct        int    `json:"mark_pct"`
	SelfHealPct    int    `json:"self_heal_pct"`
	SelfCritPct    int    `json:"self_crit_pct"`
	StatusOnly     bool   `json:"status_only"`
}

func (raw rawAttack) parse() attackRecipe {
	return attackRecipe{
		dmgPct: raw.DamagePct, stunPct: raw.StunPct, slowPct: raw.SlowPct, slowAmt: raw.SlowAmt,
		drainPct: raw.DrainPct, drainChance: raw.DrainChance, cursePowerPct: raw.CursePowerPct,
		curseHealthPct: raw.CurseHealthPct, curseTurns: raw.CurseTurns, levyPct: raw.LevyPct,
		assassinatePct: raw.AssassinatePct, markPct: raw.MarkPct, selfHealPct: raw.SelfHealPct,
		selfCritPct: raw.SelfCritPct, target: recipeTargetNames[raw.Target], statusOnly: raw.StatusOnly,
	}
}

type rawEffects struct {
	SelfPowerPct          int `json:"self_power_pct"`
	SelfPowerChance       int `json:"self_power_chance"`
	SelfHealPct           int `json:"self_heal_pct"`
	SelfHealChance        int `json:"self_heal_chance"`
	SelfCritPct           int `json:"self_crit_pct"`
	SelfDamageBonusPct    int `json:"self_damage_bonus_pct"`
	SelfDamagePct         int `json:"self_damage_pct"`
	AllyHealPct           int `json:"ally_heal_pct"`
	AllyHealChance        int `json:"ally_heal_chance"`
	AllyPowerPct          int `json:"ally_power_pct"`
	AllyPowerChance       int `json:"ally_power_chance"`
	AllyPowerFromActorPct int `json:"ally_power_from_actor_pct"`
	AllySlowAmt           int `json:"ally_slow_amt"`
	AllyCritPct           int `json:"ally_crit_pct"`
	AllyDamageBonusPct    int `json:"ally_damage_bonus_pct"`
	AllyDamagePct         int `json:"ally_damage_pct"`
}

func (raw rawEffects) parse() secondaryEffects {
	return secondaryEffects{
		selfPowerPct: raw.SelfPowerPct, selfPowerChance: raw.SelfPowerChance, selfHealPct: raw.SelfHealPct,
		selfHealChance: raw.SelfHealChance, selfDamageBonusPct: raw.SelfDamageBonusPct,
		selfDamagePct: raw.SelfDamagePct, allyHealPct: raw.AllyHealPct, allyHealChance: raw.AllyHealChance,
		allyPowerPct: raw.AllyPowerPct, allyPowerChance: raw.AllyPowerChance,
		allyPowerFromActorPct: raw.AllyPowerFromActorPct, allySlowAmt: raw.AllySlowAmt,
		allyCritPct: raw.AllyCritPct, allyDamageBonusPct: raw.AllyDamageBonusPct, allyDamagePct: raw.AllyDamagePct,
	}
}

const (
	supportedRecipeVersion = 1
	maxGeneratedRecipes    = 512
	maxRecipeSteps         = 8
	alwaysPct              = 100
)

func loadBuiltinRecipes(data []byte) (map[string]*abilityRecipe, error) {
	var root struct {
		Version int `json:"version"`
		Recipes []struct {
			Key                    string     `json:"key"`
			Hit                    rawAttack  `json:"hit"`
			Effects                rawEffects `json:"effects"`
			EffectPct              int        `json:"effect_pct"`
			Effect                 rawAttack  `json:"effect"`
			FollowupPct            int        `json:"followup_pct"`
			FollowupCount          int        `json:"followup_count"`
			FollowupDamageStepPct  int        `json:"followup_damage_step_pct"`
			FollowupFinalDamagePct int        `json:"followup_final_damage_pct"`
			Followup               rawAttack  `json:"followup"`
		} `json:"recipes"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("battle recipes: %w", err)
	}
	if root.Version != supportedRecipeVersion {
		return nil, fmt.Errorf("battle recipes: version %d", root.Version)
	}
	recipes := map[string]*abilityRecipe{}
	for _, raw := range root.Recipes {
		if _, seen := recipes[raw.Key]; seen {
			continue
		}
		recipes[raw.Key] = &abilityRecipe{
			hit: raw.Hit.parse(), fx: raw.Effects.parse(), effectPct: raw.EffectPct, effect: raw.Effect.parse(),
			followupPct: raw.FollowupPct, followupCount: raw.FollowupCount,
			followupDamageStepPct: raw.FollowupDamageStepPct, followupFinalDamagePct: raw.FollowupFinalDamagePct,
			followup: raw.Followup.parse(),
		}
	}
	return recipes, nil
}

func normalizeChance(chance int) int {
	if chance == alwaysPct {
		return 0
	}
	return chance
}

func loadGeneratedRecipes(data []byte) (map[string]*abilityRecipe, error) {
	var root struct {
		Version int `json:"version"`
		Recipes []struct {
			Key   string `json:"key"`
			Steps []struct {
				ChancePct      int         `json:"chance_pct"`
				Repeat         int         `json:"repeat"`
				ChanceGroup    int         `json:"chance_group"`
				FinalDamagePct int         `json:"final_damage_pct"`
				StopOnFail     bool        `json:"stop_on_fail"`
				EffectsOnce    bool        `json:"effects_once"`
				Attack         rawAttack   `json:"attack"`
				Effects        *rawEffects `json:"effects"`
			} `json:"steps"`
		} `json:"recipes"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("ability recipes: %w", err)
	}
	if root.Version != supportedRecipeVersion || len(root.Recipes) == 0 || len(root.Recipes) > maxGeneratedRecipes {
		return nil, fmt.Errorf("ability recipes: invalid generated recipe data")
	}
	recipes := map[string]*abilityRecipe{}
	for _, raw := range root.Recipes {
		if len(raw.Steps) > maxRecipeSteps {
			return nil, fmt.Errorf("ability recipes: too many steps for %s", raw.Key)
		}
		recipe := &abilityRecipe{generated: true}
		for _, rawStep := range raw.Steps {
			step := recipeStep{
				attack: rawStep.Attack.parse(), chancePct: rawStep.ChancePct, repeat: rawStep.Repeat,
				chanceGroup: rawStep.ChanceGroup, finalDamagePct: rawStep.FinalDamagePct,
				stopOnFail: rawStep.StopOnFail, effectsOnce: rawStep.EffectsOnce,
			}
			if rawStep.Effects != nil {
				step.fx = rawStep.Effects.parse()
				step.selfCritPct = rawStep.Effects.SelfCritPct
				step.fx.selfPowerChance = normalizeChance(step.fx.selfPowerChance)
				step.fx.selfHealChance = normalizeChance(step.fx.selfHealChance)
				step.fx.allyHealChance = normalizeChance(step.fx.allyHealChance)
				step.fx.allyPowerChance = normalizeChance(step.fx.allyPowerChance)
			}
			recipe.steps = append(recipe.steps, step)
		}
		if _, seen := recipes[raw.Key]; !seen {
			recipes[raw.Key] = recipe
		}
	}
	return recipes, nil
}

func inferredRecipe(mechanics uint32) *abilityRecipe {
	r := &abilityRecipe{}
	has := func(mechanic uint32) bool { return mechanics&mechanic != 0 }
	if has(mechanicPowerDrain) {
		r.hit.drainPct = 25
	}
	if has(mechanicStun) {
		r.hit.stunPct = 25
	}
	if has(mechanicSlow) {
		r.hit.slowPct = 100
		r.hit.slowAmt = -50
	}
	if has(mechanicCursePower) {
		r.hit.cursePowerPct = 100
	}
	if has(mechanicCurseHealth) {
		r.hit.curseHealthPct = 100
	}
	if has(mechanicCurseTwoRounds) {
		r.hit.curseTurns = 2
	}
	if has(mechanicLevy) {
		r.hit.levyPct = 15
	}
	if has(mechanicMark) {
		r.hit.markPct = 20
	}
	if has(mechanicAssassinate) {
		r.hit.assassinatePct = 5
	}
	if has(mechanicHealAllies) {
		r.fx.allyHealPct = 25
	}
	if has(mechanicHealSelf) {
		r.fx.selfHealPct = 25
	}
	if has(mechanicGainPowerAllies) {
		r.fx.allyPowerPct = 25
	}
	if has(mechanicGainPowerSelf) {
		r.fx.selfPowerPct = 25
	}
	if has(mechanicAttackFriendly) {
		r.fx.allyDamagePct = -5
	}
	if has(mechanicDamageSelf) {
		r.fx.selfDamagePct = -5
	}
	if has(mechanicLosePowerSelf) {
		r.fx.selfPowerPct = -10
	}
	if has(mechanicSlowAllies) {
		r.fx.allySlowAmt = -50
	}
	if has(mechanicGainCritAllies) {
		r.fx.allyCritPct = 5
	}
	if has(mechanicGainDamageAllies) {
		r.fx.allyDamageBonusPct = 5
	}
	if has(mechanicGainDamageSelf) {
		r.fx.selfDamageBonusPct = 5
	}
	if has(mechanicMultiattack) {
		r.followupPct = 25
		r.followupCount = 1
		r.followup.target = recipeTargetAbility
	}
	return r
}

func (d *Data) recipeFor(key string, mechanics uint32) *abilityRecipe {
	if recipe, ok := d.builtinRecipes[key]; ok {
		return recipe
	}
	if recipe, ok := d.generatedRecipes[key]; ok {
		return recipe
	}
	return inferredRecipe(mechanics)
}
