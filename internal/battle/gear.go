package battle

import (
	"encoding/json"
	"fmt"
)

type gearStat int

const (
	gearStatNone gearStat = iota
	gearStatPower
	gearStatHealth
	gearStatCrit
	gearStatBonusDamage
	gearStatDamageResist
	gearStatRawDamageResist
	gearStatStunResist
	gearStatSlowResist
	gearStatDrainResist
	gearStatCursePowerResist
	gearStatCurseHealthResist
	gearStatDefender
	gearStatStealth
	gearStatPerceptive
	gearStatHaste
	gearStatFlankReduction
	gearStatTargetHighestPower
	gearStatTargetLowestHealth
	gearStatTargetBackRow
	gearStatTargetQuick
	gearStatTargetSlowest
	gearStatTargetMatch
	gearStatDamage
	gearStatDrain
)

var gearStatNames = map[string]gearStat{
	"power": gearStatPower, "health": gearStatHealth, "crit": gearStatCrit, "bonus_damage": gearStatBonusDamage,
	"damage_resist": gearStatDamageResist, "raw_damage_resist": gearStatRawDamageResist,
	"stun_resist": gearStatStunResist, "slow_resist": gearStatSlowResist, "drain_resist": gearStatDrainResist,
	"curse_power_resist": gearStatCursePowerResist, "curse_health_resist": gearStatCurseHealthResist,
	"defender": gearStatDefender, "stealth": gearStatStealth, "perceptive": gearStatPerceptive,
	"haste": gearStatHaste, "flank_reduction": gearStatFlankReduction,
	"target_highest_power": gearStatTargetHighestPower, "target_lowest_health": gearStatTargetLowestHealth,
	"target_back_row": gearStatTargetBackRow, "target_quick": gearStatTargetQuick,
	"target_slowest": gearStatTargetSlowest, "target_match": gearStatTargetMatch, "damage": gearStatDamage,
	"drain": gearStatDrain,
}

type gearTrigger int

const (
	onBattle gearTrigger = iota
	onSelfHit
	onAllyHit
	onEnemyHit
	onSelfAttack
	onAllyAttack
	onEnemyDeath
	onAllyDeath
	onSelfDeath
)

var gearTriggerNames = map[string]gearTrigger{
	"battle": onBattle, "self_hit": onSelfHit, "ally_hit": onAllyHit, "enemy_hit": onEnemyHit,
	"self_attack": onSelfAttack, "ally_attack": onAllyAttack, "enemy_death": onEnemyDeath,
	"ally_death": onAllyDeath, "self_death": onSelfDeath,
}

type gearEffect struct {
	stat        gearStat
	on          gearTrigger
	damageBasis bool
	value       int
	recipient   filter
	wearer      filter
	versus      filter
}

type gearDef struct {
	id      string
	effects []gearEffect
}

const (
	gearSlotCapacity = 5
	heroAuraSource   = gearSlotCapacity
	gearSourceCount  = gearSlotCapacity + 1
)

func gearTerms(terms [][]string, each bool, f *filter) {
	if len(terms) == 0 {
		return
	}
	f.hasMatch = true
	f.each = each
	for _, term := range terms {
		var kind, value string
		if len(term) > 0 {
			kind = term[0]
		}
		if len(term) > 1 {
			value = term[1]
		}
		f.terms = append(f.terms, matchTerm{kind: parseTermKind(kind), value: value})
	}
}

func loadGear(data []byte) (map[string]*gearDef, error) {
	return loadGearItems("gear", data)
}

const heroAuraVersion = 1

func loadHeroAuras(data []byte) (map[string]*gearDef, error) {
	var root struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("hero auras: %w", err)
	}
	if root.Version != heroAuraVersion {
		return nil, fmt.Errorf("hero auras: version %d, want %d", root.Version, heroAuraVersion)
	}
	return loadGearItems("hero auras", data)
}

func loadGearItems(table string, data []byte) (map[string]*gearDef, error) {
	var root struct {
		Items []struct {
			ID      string `json:"id"`
			Effects []struct {
				Stat   string     `json:"stat"`
				On     string     `json:"on"`
				Value  int        `json:"value"`
				Basis  string     `json:"basis"`
				Each   bool       `json:"each"`
				Scope  string     `json:"scope"`
				Who    [][]string `json:"who"`
				Wearer [][]string `json:"wearer"`
				Vs     [][]string `json:"vs"`
			} `json:"effects"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("%s: %w", table, err)
	}
	gear := map[string]*gearDef{}
	for _, item := range root.Items {
		def := &gearDef{id: item.ID}
		for _, raw := range item.Effects {
			stat, known := gearStatNames[raw.Stat]
			on, triggered := gearTriggerNames[raw.On]
			if !known || !triggered {
				return nil, fmt.Errorf("%s: %s has an unknown effect", table, item.ID)
			}
			effect := gearEffect{stat: stat, on: on, damageBasis: raw.Basis == "damage", value: raw.Value}
			effect.recipient = filter{scope: parseScope(raw.Scope), includeSource: true}
			gearTerms(raw.Who, raw.Each, &effect.recipient)
			gearTerms(raw.Wearer, false, &effect.wearer)
			gearTerms(raw.Vs, raw.Each, &effect.versus)
			def.effects = append(def.effects, effect)
		}
		if _, seen := gear[item.ID]; !seen {
			gear[item.ID] = def
		}
	}
	return gear, nil
}
