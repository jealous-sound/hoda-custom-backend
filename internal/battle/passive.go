package battle

import (
	"encoding/json"
	"fmt"
	"strconv"
)

type passiveKind int

const (
	passiveNone passiveKind = iota
	passiveStatPercent
	passiveStatPercentPerMatchingAlly
	passiveIncomingTargetWeight
	passiveOutgoingTargetPreference
	passiveHealPercentMaxHealth
	passiveBonusDamagePercent
)

var passiveKindNames = map[string]passiveKind{
	"stat_percent":                   passiveStatPercent,
	"stat_percent_per_matching_ally": passiveStatPercentPerMatchingAlly,
	"incoming_target_weight":         passiveIncomingTargetWeight,
	"outgoing_target_preference":     passiveOutgoingTargetPreference,
	"heal_percent_max_health":        passiveHealPercentMaxHealth,
	"bonus_damage_percent":           passiveBonusDamagePercent,
}

type passiveTrigger int

const (
	triggerNone passiveTrigger = iota
	triggerBattleStart
	triggerTargetSelection
	triggerEnemyDeath
	triggerBeforeDamage
)

var passiveTriggerNames = map[string]passiveTrigger{
	"battle_start": triggerBattleStart, "target_selection": triggerTargetSelection,
	"enemy_death": triggerEnemyDeath, "before_damage": triggerBeforeDamage,
}

type scope int

const (
	scopeSelf scope = iota
	scopeAllies
	scopeEnemies
)

func parseScope(name string) scope {
	switch name {
	case "enemies":
		return scopeEnemies
	case "allies":
		return scopeAllies
	}
	return scopeSelf
}

type termKind int

const (
	termGroup termKind = iota
	termFaction
	termCharacterID
	termSpecialty
	termRarity
	termSpeed
	termTag
	termUnknown
)

var termKindNames = map[string]termKind{
	"group": termGroup, "faction": termFaction, "character_id": termCharacterID, "specialty": termSpecialty,
	"rarity": termRarity, "speed": termSpeed, "tag": termTag,
}

func parseTermKind(name string) termKind {
	if kind, ok := termKindNames[name]; ok {
		return kind
	}
	return termUnknown
}

type matchTerm struct {
	kind  termKind
	value string
}

type filter struct {
	scope         scope
	includeSource bool
	hasMatch      bool
	each          bool
	terms         []matchTerm
}

const (
	statHealth uint32 = 1 << iota
	statPower
)

const selectorLowestHealthPercent = "lowest_health_percent"

type passiveEffect struct {
	kind            passiveKind
	trigger         passiveTrigger
	stats           uint32
	valuePct        float32
	valueMultiplier float32
	lowestHealth    bool
	recipient       filter
	count           filter
	hasCount        bool
	target          filter
	hasTarget       bool
	additive        bool
}

type rawFilter struct {
	Scope         string `json:"scope"`
	IncludeSource *bool  `json:"include_source"`
	Match         *struct {
		Mode  string `json:"mode"`
		Terms []struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
		} `json:"terms"`
	} `json:"match"`
}

func (raw *rawFilter) parse() filter {
	if raw == nil {
		return filter{}
	}
	f := filter{scope: parseScope(raw.Scope), includeSource: raw.IncludeSource == nil || *raw.IncludeSource}
	if raw.Match == nil {
		return f
	}
	f.hasMatch = true
	f.each = raw.Match.Mode == "each"
	for _, term := range raw.Match.Terms {
		f.terms = append(f.terms, matchTerm{kind: parseTermKind(term.Kind), value: term.Value})
	}
	return f
}

func (f *filter) valid() bool {
	if !f.hasMatch {
		return true
	}
	if len(f.terms) == 0 {
		return false
	}
	for _, term := range f.terms {
		if term.kind == termUnknown || term.value == "" {
			return false
		}
	}
	return true
}

type rawPassiveEffect struct {
	ID              string     `json:"id"`
	Kind            string     `json:"kind"`
	Trigger         string     `json:"trigger"`
	ValuePct        float64    `json:"value_pct"`
	ValueMultiplier float64    `json:"value_multiplier"`
	Stacking        string     `json:"stacking"`
	Selector        string     `json:"selector"`
	Stats           []string   `json:"stats"`
	Recipient       *rawFilter `json:"recipient"`
	Count           *rawFilter `json:"count"`
	Target          *rawFilter `json:"target"`
}

func (raw rawPassiveEffect) parse() passiveEffect {
	e := passiveEffect{
		kind: passiveKindNames[raw.Kind], trigger: passiveTriggerNames[raw.Trigger],
		valuePct: float32(raw.ValuePct), valueMultiplier: float32(raw.ValueMultiplier),
		additive: raw.Stacking == "additive", lowestHealth: raw.Selector == selectorLowestHealthPercent,
		recipient: raw.Recipient.parse(), count: raw.Count.parse(), hasCount: raw.Count != nil,
		target: raw.Target.parse(), hasTarget: raw.Target != nil,
	}
	for _, stat := range raw.Stats {
		switch stat {
		case "health":
			e.stats |= statHealth
		case "power":
			e.stats |= statPower
		}
	}
	return e
}

func (e *passiveEffect) valid(id string) bool {
	if id == "" || e.kind == passiveNone || e.trigger == triggerNone {
		return false
	}
	if !e.recipient.valid() || (e.hasCount && !e.count.valid()) || (e.hasTarget && !e.target.valid()) {
		return false
	}
	switch e.kind {
	case passiveStatPercent:
		return e.trigger == triggerBattleStart && e.stats != 0 && e.additive
	case passiveStatPercentPerMatchingAlly:
		return e.trigger == triggerBattleStart && e.stats != 0 && e.valuePct != 0 && e.recipient.scope == scopeSelf &&
			e.hasCount && e.count.scope == scopeAllies && e.count.hasMatch && e.additive
	case passiveIncomingTargetWeight, passiveOutgoingTargetPreference:
		return e.trigger == triggerTargetSelection && e.valueMultiplier > 0
	case passiveHealPercentMaxHealth:
		return e.trigger == triggerEnemyDeath && e.valuePct != 0
	}
	return e.kind == passiveBonusDamagePercent && e.trigger == triggerBeforeDamage && e.valuePct != 0
}

const supportedPassiveVersion = 1

func loadPassives(data []byte) (map[string][]passiveEffect, error) {
	var root struct {
		Version    int `json:"version"`
		Characters []struct {
			ID      string             `json:"id"`
			Effects []rawPassiveEffect `json:"effects"`
		} `json:"characters"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("intrinsic passives: %w", err)
	}
	if root.Version != supportedPassiveVersion {
		return nil, fmt.Errorf("intrinsic passives: version %d", root.Version)
	}
	passives := map[string][]passiveEffect{}
	previous := ""
	for i, row := range root.Characters {
		if row.ID == "" || (i > 0 && previous >= row.ID) {
			return nil, fmt.Errorf("intrinsic passives: ids must be nonempty, unique and sorted at %q", row.ID)
		}
		previous = row.ID
		effects := make([]passiveEffect, 0, len(row.Effects))
		for _, raw := range row.Effects {
			effect := raw.parse()
			if !effect.valid(raw.ID) {
				return nil, fmt.Errorf("intrinsic passives: invalid effect %s on %s", raw.ID, row.ID)
			}
			effects = append(effects, effect)
		}
		passives[row.ID] = effects
	}
	return passives, nil
}

func termMatches(term *matchTerm, h *hero) bool {
	c := h.def
	switch term.kind {
	case termGroup:
		return c.hasGroup(term.value)
	case termFaction:
		wanted := map[string]uint32{"Black": maskBlack, "White": maskWhite, "Red": maskRed, "Blue": maskBlue}[term.value]
		return wanted != 0 && factionMask(c.faction)&wanted != 0
	case termCharacterID:
		return c.id == term.value
	case termSpecialty:
		spec, ok := parseSpecialty(term.value)
		return ok && c.hasSpecialty(spec)
	case termRarity:
		r, ok := map[string]rarity{
			"Common": rarityCommon, "Uncommon": rarityUncommon, "Rare": rarityRare, "Epic": rarityEpic,
			"Legendary": rarityLegendary, "Mythic": rarityMythic,
		}[term.value]
		return ok && h.rarity == r
	case termSpeed:
		s, ok := map[string]speed{"Slow": speedSlow, "Normal": speedNormal, "Quick": speedQuick}[term.value]
		return ok && c.speed == s
	case termTag:
		if term.value == "GIANT" {
			return c.giant
		}
		return term.value == strconv.Itoa(c.stars)+"STAR"
	}
	return false
}
