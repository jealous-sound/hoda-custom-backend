package battle

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

type runeBuff int

const (
	buffPower runeBuff = iota
	buffHealth
	buffInitiative
	buffDamageShield
	buffDamageResist
	buffRetaliation
	buffStunResist
	buffSlowResist
	buffDrainResist
	buffCursePowerImmune
	buffStealth
	buffPerceptive
	buffGold
	buffXP
	buffTargetHighestPower
	buffTargetHighestSpeed
	buffTargetLowestSpeed
	buffTargetLowestHealth
)

var runeBuffNames = map[string]runeBuff{
	"power": buffPower, "health": buffHealth, "initiative": buffInitiative, "damage": buffDamageShield,
	"damage-resist-percent": buffDamageResist, "damage-retaliation-percent": buffRetaliation,
	"stunned": buffStunResist, "slow-resist": buffSlowResist, "power-drain-resist": buffDrainResist,
	"power-cursed": buffCursePowerImmune, "stealth": buffStealth, "perceptive": buffPerceptive,
	"gold": buffGold, "xp": buffXP, "highest-power-unit": buffTargetHighestPower,
	"highest-speed-unit": buffTargetHighestSpeed, "lowest-speed-unit": buffTargetLowestSpeed,
	"lowest-percent-health": buffTargetLowestHealth,
}

const (
	RuneSlotCount         = 3
	runeResistMaxPct      = 100
	overchargedRuneIDMark = "-6-"
	specialRuneIDMark     = "Special"
)

type runeDef struct {
	id              string
	buff            runeBuff
	value           int
	durationMinutes int
	rarity          int
}

func (r *runeDef) rollable() bool {
	return r.durationMinutes > 0 && !strings.Contains(r.id, overchargedRuneIDMark) &&
		!strings.Contains(r.id, specialRuneIDMark)
}

type runeTarget int

const (
	runeTargetHighestPower runeTarget = iota
	runeTargetHighestSpeed
	runeTargetLowestSpeed
	runeTargetLowestHealth
	runeTargetCount
)

type runeBonus struct {
	powerPct, healthPct                          int
	initiativeSteps                              int
	damageShield, damageResistPct, retaliation   int
	stunResistPct, slowResistPct, drainResistPct int
	targetLevel                                  [runeTargetCount]int
	cursePowerImmune, stealth, perceptive        bool
}

func (b *runeBonus) add(def *runeDef) {
	switch def.buff {
	case buffPower:
		b.powerPct += def.value
	case buffHealth:
		b.healthPct += def.value
	case buffInitiative:
		b.initiativeSteps += def.value
	case buffDamageShield:
		b.damageShield += def.value
	case buffDamageResist:
		b.damageResistPct += def.value
	case buffRetaliation:
		b.retaliation += def.value
	case buffStunResist:
		b.stunResistPct += def.value
	case buffSlowResist:
		b.slowResistPct += def.value
	case buffDrainResist:
		b.drainResistPct += def.value
	case buffCursePowerImmune:
		b.cursePowerImmune = true
	case buffStealth:
		b.stealth = true
	case buffPerceptive:
		b.perceptive = true
	case buffTargetHighestPower:
		b.targetLevel[runeTargetHighestPower] += def.value
	case buffTargetHighestSpeed:
		b.targetLevel[runeTargetHighestSpeed] += def.value
	case buffTargetLowestSpeed:
		b.targetLevel[runeTargetLowestSpeed] += def.value
	case buffTargetLowestHealth:
		b.targetLevel[runeTargetLowestHealth] += def.value
	}
}

func runesBonus(defs []*runeDef) runeBonus {
	var bonus runeBonus
	for _, def := range defs {
		bonus.add(def)
	}
	return bonus
}

func runeApplyPct(value, pct int) int {
	if value <= 0 || pct <= 0 {
		return value
	}
	scaled := float64(value) * (100.0 + float64(pct)) / 100.0
	if scaled >= math.MaxInt32 {
		return math.MaxInt32
	}
	return int(math.Round(scaled))
}

func loadRunes(data []byte) ([]*runeDef, map[string]*runeDef, error) {
	var root struct {
		Runes []struct {
			ID              string `json:"id"`
			BuffType        string `json:"buff_type"`
			BuffValue       int    `json:"buff_value"`
			DurationMinutes int    `json:"duration_minutes"`
			Rarity          int    `json:"rarity"`
		} `json:"runes"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, nil, fmt.Errorf("runes: %w", err)
	}
	list := make([]*runeDef, 0, len(root.Runes))
	byID := map[string]*runeDef{}
	for _, raw := range root.Runes {
		buff, ok := runeBuffNames[raw.BuffType]
		if !ok {
			return nil, nil, fmt.Errorf("runes: unknown buff type %s", raw.BuffType)
		}
		def := &runeDef{
			id: raw.ID, buff: buff, value: raw.BuffValue, durationMinutes: raw.DurationMinutes, rarity: raw.Rarity,
		}
		list = append(list, def)
		if _, seen := byID[raw.ID]; !seen {
			byID[raw.ID] = def
		}
	}
	return list, byID, nil
}
