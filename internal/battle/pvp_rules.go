package battle

import (
	"cmp"
	"fmt"
	"slices"
)

const (
	maxRuleSides = 4
	maxRuleNames = 8
	anySide      = -1
)

const (
	ViolationEmpty     = "empty"
	ViolationRarity    = "rarity"
	ViolationMember    = "member"
	ViolationDuplicate = "duplicate"
	ViolationSide      = "side"
)

type rawHeroFilter struct {
	Factions []string `json:"factions"`
	Groups   []string `json:"groups"`
}

type rawSquadRules struct {
	MaxRarity    string          `json:"max_rarity"`
	Rarities     []string        `json:"rarities"`
	UniqueHeroes bool            `json:"unique_heroes"`
	Members      rawHeroFilter   `json:"members"`
	Sides        []rawHeroFilter `json:"sides"`
}

type heroFilter struct {
	factions uint32
	groups   []string
}

type squadRules struct {
	maxRarity    rarity
	rarities     uint32
	uniqueHeroes bool
	members      heroFilter
	sides        []heroFilter
}

type SquadViolation struct {
	Kind string
	Slot int
	Hero string
	Side int
}

func (v *SquadViolation) Error() string {
	return fmt.Sprintf("%s in slot %d breaks the event's %s rule", v.Hero, v.Slot, v.Kind)
}

func rarityBit(r rarity) uint32 { return 1 << uint(r) }

func parseRuleRarity(name string) (rarity, error) {
	if name == "Common" {
		return rarityCommon, nil
	}
	if r, ok := rarityNames[name]; ok {
		return r, nil
	}
	return 0, fmt.Errorf("unknown rarity %q", name)
}

func parseHeroFilter(raw rawHeroFilter) (heroFilter, error) {
	if len(raw.Factions)+len(raw.Groups) > maxRuleNames {
		return heroFilter{}, fmt.Errorf("a squad rule names more than %d factions and groups", maxRuleNames)
	}
	filter := heroFilter{groups: append([]string{}, raw.Groups...)}
	for _, name := range raw.Factions {
		mask := factionMask(parseFaction(name))
		if mask == 0 {
			return heroFilter{}, fmt.Errorf("unknown faction %q", name)
		}
		filter.factions |= mask
	}
	return filter, nil
}

func parseSquadRules(raw *rawSquadRules) (*squadRules, error) {
	if raw == nil {
		return nil, nil
	}
	rules := &squadRules{maxRarity: rarityMythic, uniqueHeroes: raw.UniqueHeroes}
	if raw.MaxRarity != "" {
		r, err := parseRuleRarity(raw.MaxRarity)
		if err != nil {
			return nil, err
		}
		rules.maxRarity = r
	}
	for _, name := range raw.Rarities {
		r, err := parseRuleRarity(name)
		if err != nil {
			return nil, err
		}
		rules.rarities |= rarityBit(r)
	}
	var err error
	if rules.members, err = parseHeroFilter(raw.Members); err != nil {
		return nil, err
	}
	if len(raw.Sides) == 1 || len(raw.Sides) > maxRuleSides {
		return nil, fmt.Errorf("a side event needs 2-%d sides, not %d", maxRuleSides, len(raw.Sides))
	}
	for _, rawSide := range raw.Sides {
		side, err := parseHeroFilter(rawSide)
		if err != nil {
			return nil, err
		}
		rules.sides = append(rules.sides, side)
	}
	return rules, nil
}

func (f heroFilter) passes(c *character) bool {
	if f.factions == 0 && len(f.groups) == 0 {
		return true
	}
	if f.factions != 0 && (c.giant || factionMask(c.faction)&f.factions != 0) {
		return true
	}
	return slices.ContainsFunc(f.groups, c.hasGroup)
}

func (r *squadRules) rarityAllowed(h fieldedHero) bool {
	shown := h.shownRarity()
	return shown <= r.maxRarity && (r.rarities == 0 || r.rarities&rarityBit(shown) != 0)
}

type fieldedHero struct {
	slot     int
	hero     *character
	ascended bool
}

func (h fieldedHero) shownRarity() rarity {
	if h.ascended {
		return rarityMythic
	}
	return h.hero.originalRarity
}

func (r *squadRules) sideMisfit(heroes []fieldedHero, side int) int {
	for i, fielded := range heroes {
		if !r.sides[side].passes(fielded.hero) {
			return i
		}
	}
	return -1
}

func (r *squadRules) sideFitCount(heroes []fieldedHero, side int) int {
	fitting := 0
	for _, fielded := range heroes {
		if r.sides[side].passes(fielded.hero) {
			fitting++
		}
	}
	return fitting
}

func violation(kind string, heroes []fieldedHero, index, side int) *SquadViolation {
	v := &SquadViolation{Kind: kind, Slot: -1, Side: side}
	if index >= 0 && index < len(heroes) {
		v.Slot, v.Hero = heroes[index].slot, heroes[index].hero.id
	}
	return v
}

func (r *squadRules) judgeSide(heroes []fieldedHero, side int) (int, *SquadViolation) {
	if side != anySide {
		if side < 0 || side >= len(r.sides) {
			return side, violation(ViolationSide, heroes, 0, side)
		}
		if misfit := r.sideMisfit(heroes, side); misfit >= 0 {
			return side, violation(ViolationSide, heroes, misfit, side)
		}
		return side, nil
	}
	closest := 0
	for candidate := range r.sides {
		if r.sideMisfit(heroes, candidate) < 0 {
			return candidate, nil
		}
		if r.sideFitCount(heroes, candidate) > r.sideFitCount(heroes, closest) {
			closest = candidate
		}
	}
	return closest, violation(ViolationSide, heroes, r.sideMisfit(heroes, closest), closest)
}

func (r *squadRules) judge(heroes []fieldedHero, side int) (int, *SquadViolation) {
	if len(heroes) == 0 {
		return 0, violation(ViolationEmpty, heroes, -1, 0)
	}
	for i, fielded := range heroes {
		if !r.rarityAllowed(fielded) {
			return 0, violation(ViolationRarity, heroes, i, 0)
		}
		if !r.members.passes(fielded.hero) {
			return 0, violation(ViolationMember, heroes, i, 0)
		}
		if r.uniqueHeroes && slices.ContainsFunc(heroes[:i], func(earlier fieldedHero) bool {
			return earlier.hero.id == fielded.hero.id
		}) {
			return 0, violation(ViolationDuplicate, heroes, i, 0)
		}
	}
	if len(r.sides) == 0 {
		return 0, nil
	}
	return r.judgeSide(heroes, side)
}

func (d *Data) fieldedHeroes(units []Unit) ([]fieldedHero, error) {
	heroes := make([]fieldedHero, 0, len(units))
	for _, unit := range units {
		c, ok := d.characters[unit.ID]
		if !ok {
			return nil, fmt.Errorf("unit %s is not in the catalog", unit.ID)
		}
		heroes = append(heroes, fieldedHero{slot: unit.Slot, hero: c, ascended: unit.Ascended})
	}
	slices.SortStableFunc(heroes, func(a, b fieldedHero) int { return cmp.Compare(a.slot, b.slot) })
	return heroes, nil
}

func (r *squadRules) fieldsAgainst(heroes []fieldedHero, playerSide int) bool {
	if len(r.sides) == 0 {
		_, v := r.judge(heroes, anySide)
		return v == nil
	}
	if _, v := r.judge(heroes, playerSide); v == nil {
		return false
	}
	for side := range r.sides {
		if side == playerSide {
			continue
		}
		if _, v := r.judge(heroes, side); v == nil {
			return true
		}
	}
	return false
}

func (r *squadRules) poolCount() int {
	return max(len(r.sides), 1)
}
