package battle

import (
	"encoding/json"
	"fmt"
)

type Tier int

const (
	TierI Tier = iota
	TierII
	TierIII
	TierIV
	tierCount
)

var tierNames = [tierCount]string{"I", "II", "III", "IV"}

func ParseTier(name string) Tier {
	for t, tierName := range tierNames {
		if tierName == name {
			return Tier(t)
		}
	}
	return TierI
}

func (t Tier) String() string {
	return tierNames[t]
}

type rarity int

const (
	rarityCommon rarity = iota + 1
	rarityUncommon
	rarityRare
	rarityEpic
	rarityLegendary
	rarityMythic
)

var rarityNames = map[string]rarity{
	"Uncommon": rarityUncommon, "Rare": rarityRare, "Epic": rarityEpic, "Legendary": rarityLegendary,
	"Mythic": rarityMythic,
}

func parseRarity(name string) rarity {
	if r, ok := rarityNames[name]; ok {
		return r
	}
	return rarityCommon
}

type speed int

const (
	speedSlow speed = iota
	speedNormal
	speedQuick
)

func parseSpeed(name string) speed {
	switch name {
	case "Slow":
		return speedSlow
	case "Quick":
		return speedQuick
	}
	return speedNormal
}

type faction int

const (
	factionLarge faction = iota
	factionWhite
	factionRed
	factionBlue
	factionBlack
	factionWhiteRed
	factionWhiteBlue
	factionRedBlue
	factionWhiteBlack
	factionRedBlack
	factionBlueBlack
	factionAll
)

var factionNames = map[string]faction{
	"White": factionWhite, "Red": factionRed, "Blue": factionBlue, "Black": factionBlack,
	"White/Red": factionWhiteRed, "White/Blue": factionWhiteBlue, "Red/Blue": factionRedBlue,
	"White/Black": factionWhiteBlack, "Red/Black": factionRedBlack, "Blue/Black": factionBlueBlack, "All": factionAll,
}

func parseFaction(name string) faction {
	return factionNames[name]
}

const (
	maskBlack uint32 = 1
	maskWhite uint32 = 2
	maskRed   uint32 = 4
	maskBlue  uint32 = 8
	maskAll          = maskBlack | maskWhite | maskRed | maskBlue
)

func factionMask(f faction) uint32 {
	switch f {
	case factionWhite:
		return maskWhite
	case factionRed:
		return maskRed
	case factionBlue:
		return maskBlue
	case factionBlack:
		return maskBlack
	case factionWhiteRed:
		return maskWhite | maskRed
	case factionWhiteBlue:
		return maskWhite | maskBlue
	case factionRedBlue:
		return maskRed | maskBlue
	case factionWhiteBlack:
		return maskWhite | maskBlack
	case factionRedBlack:
		return maskRed | maskBlack
	case factionBlueBlack:
		return maskBlue | maskBlack
	case factionAll:
		return maskAll
	}
	return 0
}

type targetPattern int

const (
	patternOne targetPattern = iota
	patternRow
	patternColumn
	patternAll
)

func parseTargetPattern(name string) targetPattern {
	switch name {
	case "Row":
		return patternRow
	case "Column":
		return patternColumn
	case "All":
		return patternAll
	}
	return patternOne
}

const (
	mechanicAttack uint32 = 1 << iota
	mechanicAttackNone
	mechanicPowerDrain
	mechanicStun
	mechanicSlow
	mechanicCursePower
	mechanicCurseHealth
	mechanicHealAllies
	mechanicHealSelf
	mechanicGainPowerAllies
	mechanicGainPowerSelf
	mechanicMultiattack
	mechanicMark
	mechanicLevy
	mechanicAssassinate
	mechanicAttackFriendly
	mechanicDamageSelf
	mechanicLosePowerSelf
	mechanicSlowAllies
	mechanicGainCritAllies
	mechanicCurseTwoRounds
	mechanicGainDamageAllies
	mechanicGainDamageSelf
)

var mechanicNames = map[string]uint32{
	"ATTACK": mechanicAttack, "ATTACK_NONE": mechanicAttackNone, "POWER_DRAIN": mechanicPowerDrain,
	"STUN": mechanicStun, "SLOW": mechanicSlow, "CURSE_POWER": mechanicCursePower, "CURSE_HEALTH": mechanicCurseHealth,
	"HEAL_ALLIES": mechanicHealAllies, "HEAL_SELF": mechanicHealSelf, "GAIN_POWER_ALLIES": mechanicGainPowerAllies,
	"GAIN_POWER_SELF": mechanicGainPowerSelf, "MULTIATTACK": mechanicMultiattack, "MARK": mechanicMark,
	"LEVY": mechanicLevy, "ASSASSINATE": mechanicAssassinate, "ATTACK_FRIENDLY": mechanicAttackFriendly,
	"DAMAGE_SELF": mechanicDamageSelf, "LOSE_POWER_SELF": mechanicLosePowerSelf, "SLOW_ALLIES": mechanicSlowAllies,
	"GAIN_CRIT_ALLIES": mechanicGainCritAllies, "CURSE_TWO_ROUNDS": mechanicCurseTwoRounds,
	"GAIN_DAMAGE_ALLIES": mechanicGainDamageAllies, "GAIN_DAMAGE_SELF": mechanicGainDamageSelf,
}

type specialty int

const (
	specPowerGain specialty = iota
	specAttackSingle
	specHeal
	specAttackAll
	specStun
	specPowerDrain
	specAttackRow
	specMultiattack
	specSlow
	specAttackColumn
	specCursePower
	specTaunt
	specDefender
	specElusive
	specCurseHealth
	specLevy
	specFlank
	specRetaliate
	specAttackFriendly
	specStealth
	specAssassin
	specMark
	specAttackNone
	specPerceptive
)

var specialtyNames = []string{
	"POWERGAIN", "ATTACKSINGLE", "HEAL", "ATTACKALL", "STUN", "POWERDRAIN", "ATTACKROW", "MULTIATTACK",
	"SLOW", "ATTACKCOLUMN", "CURSEPOWER", "TAUNT", "DEFENDER", "ELUSIVE", "CURSEHEALTH", "LEVY",
	"FLANK", "RETALIATE", "ATTACKFRIENDLY", "STEALTH", "ASSASSIN", "MARK", "ATTACKNONE", "PERCEPTIVE",
}

func parseSpecialty(name string) (specialty, bool) {
	for i, specialtyName := range specialtyNames {
		if specialtyName == name {
			return specialty(i), true
		}
	}
	return 0, false
}

const (
	maxGroups      = 8
	maxSpecialties = 16
)

type ability struct {
	key       string
	target    targetPattern
	mechanics uint32
}

type tierStats struct {
	basePower      int
	baseHealth     int
	maxLevel       int
	powerPerLevel  int
	healthPerLevel int
}

type character struct {
	id          string
	basedOn     string
	rarity      rarity
	stars       int
	giant       bool
	speed       speed
	faction     faction
	groups      []string
	specialties []specialty
	ability     ability
	gearSlots   int
	hasTier     [tierCount]bool
	stats       [tierCount]tierStats
}

func (c *character) dataID() string {
	if c.basedOn != "" {
		return c.basedOn
	}
	return c.id
}

type rawCharacter struct {
	ID          string   `json:"id"`
	BasedOn     string   `json:"based_on"`
	Rarity      string   `json:"rarity"`
	Stars       int      `json:"stars"`
	Giant       bool     `json:"giant"`
	Speed       string   `json:"speed"`
	Faction     string   `json:"faction"`
	Groups      []string `json:"groups"`
	Specialties []string `json:"specialties"`
	GearSlots   int      `json:"gear_slots"`
	Ability     struct {
		Key       string   `json:"key"`
		Target    string   `json:"target"`
		Mechanics []string `json:"mechanics"`
	} `json:"ability"`
	Tiers map[string]struct {
		BasePower      int `json:"base_power"`
		BaseHealth     int `json:"base_health"`
		MaxLevel       int `json:"max_level"`
		PowerPerLevel  int `json:"power_per_level"`
		HealthPerLevel int `json:"health_per_level"`
	} `json:"tiers"`
}

func (raw rawCharacter) parse() *character {
	c := &character{
		id: raw.ID, basedOn: raw.BasedOn, rarity: parseRarity(raw.Rarity), stars: raw.Stars, giant: raw.Giant,
		speed: parseSpeed(raw.Speed), faction: parseFaction(raw.Faction), gearSlots: raw.GearSlots,
	}
	for _, group := range raw.Groups {
		if len(c.groups) == maxGroups {
			break
		}
		c.groups = append(c.groups, group)
	}
	for _, name := range raw.Specialties {
		if spec, ok := parseSpecialty(name); ok && len(c.specialties) < maxSpecialties {
			c.specialties = append(c.specialties, spec)
		}
	}
	c.ability = ability{key: raw.Ability.Key, target: parseTargetPattern(raw.Ability.Target)}
	for _, name := range raw.Ability.Mechanics {
		c.ability.mechanics |= mechanicNames[name]
	}
	for name, stats := range raw.Tiers {
		t := ParseTier(name)
		c.hasTier[t] = true
		c.stats[t] = tierStats{
			basePower: stats.BasePower, baseHealth: stats.BaseHealth, maxLevel: stats.MaxLevel,
			powerPerLevel: stats.PowerPerLevel, healthPerLevel: stats.HealthPerLevel,
		}
	}
	return c
}

func (c *character) hasGroup(group string) bool {
	for _, g := range c.groups {
		if g == group {
			return true
		}
	}
	return false
}

func (c *character) hasSpecialty(spec specialty) bool {
	for _, s := range c.specialties {
		if s == spec {
			return true
		}
	}
	return false
}

func loadCharacters(read func(string) ([]byte, error), files ...string) (map[string]*character, error) {
	characters := map[string]*character{}
	for _, file := range files {
		data, err := read(file)
		if err != nil {
			return nil, err
		}
		var root struct {
			Characters []rawCharacter `json:"characters"`
		}
		if err := json.Unmarshal(data, &root); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		for _, raw := range root.Characters {
			if _, seen := characters[raw.ID]; !seen {
				characters[raw.ID] = raw.parse()
			}
		}
	}
	return characters, nil
}
