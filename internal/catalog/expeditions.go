package catalog

import (
	"fmt"
	"slices"
	"time"
)

const (
	ExpeditionsFile          = "expeditions.json"
	ExpeditionPartyCapacity  = 5
	ExpeditionSlotCapacity   = 5
	ExpeditionDropGear       = "gear"
	ExpeditionDropRune       = "rune"
	ExpeditionDropHero       = "hero"
	supportedExpeditions     = 1
	expeditionPercent        = 100
	expeditionRangeEndpoints = 2
)

type ExpeditionTrait struct {
	Group   string
	Faction Faction
	Rarity  Rarity
	Giant   bool
	Count   int
}

func (t ExpeditionTrait) Matches(hero Hero, rarity Rarity) bool {
	switch {
	case t.Group != "":
		return slices.Contains(hero.Groups, t.Group)
	case t.Faction != 0:
		return !hero.Giant && hero.Factions&t.Faction != 0
	case t.Rarity != 0:
		return rarity >= t.Rarity
	}
	return t.Giant && hero.Giant
}

type ExpeditionDrop struct {
	Kind        string
	Chance      int
	MinRarity   int
	MaxRarity   int
	HeroWeights [RarityLegendary + 1]int
}

type Expedition struct {
	ID           string
	Level        int
	Duration     time.Duration
	Party        int
	Requirements []ExpeditionTrait
	Bonus        ExpeditionTrait
	Gold         [expeditionRangeEndpoints]int64
	Gems         [expeditionRangeEndpoints]int64
	HeroXP       int64
	Drops        []ExpeditionDrop
}

func (e Expedition) HeroDropLimit() int {
	heroes := 0
	for _, drop := range e.Drops {
		if drop.Kind == ExpeditionDropHero {
			heroes++
		}
	}
	return heroes
}

type ExpeditionRules struct {
	SlotLevels      []int
	GreatSuccessPct int64
	List            []Expedition
	index           map[string]int
}

func (r ExpeditionRules) Slots(level int) int {
	slots := 0
	for _, unlock := range r.SlotLevels {
		if level >= unlock {
			slots++
		}
	}
	return slots
}

func (r ExpeditionRules) GreatSuccess(amount int64) int64 {
	return amount * (expeditionPercent + r.GreatSuccessPct) / expeditionPercent
}

func (c *Catalog) Expedition(id string) (Expedition, bool) {
	index, ok := c.Expeditions.index[id]
	if !ok {
		return Expedition{}, false
	}
	return c.Expeditions.List[index], true
}

type rawExpeditionTrait struct {
	Group   string `json:"group"`
	Faction string `json:"faction"`
	Rarity  string `json:"rarity"`
	Giant   bool   `json:"giant"`
	Count   int    `json:"count"`
}

type rawExpeditionDrop struct {
	Kind      string         `json:"kind"`
	Chance    int            `json:"chance"`
	MinRarity int            `json:"min_rarity"`
	MaxRarity int            `json:"max_rarity"`
	Weights   map[string]int `json:"weights"`
}

type rawExpedition struct {
	ID           string               `json:"id"`
	Level        int                  `json:"level"`
	Minutes      int                  `json:"minutes"`
	Party        int                  `json:"party"`
	Requirements []rawExpeditionTrait `json:"requirements"`
	Bonus        rawExpeditionTrait   `json:"bonus"`
	Gold         []int64              `json:"gold"`
	Gems         []int64              `json:"gems"`
	HeroXP       int64                `json:"hero_xp"`
	Drops        []rawExpeditionDrop  `json:"drops"`
}

func (c *Catalog) parseExpeditionTrait(raw rawExpeditionTrait) (ExpeditionTrait, error) {
	trait := ExpeditionTrait{Group: raw.Group, Giant: raw.Giant, Count: raw.Count}
	if raw.Faction != "" {
		trait.Faction = parseFactions(raw.Faction)
	}
	if raw.Rarity != "" {
		trait.Rarity = ParseRarity(raw.Rarity)
	}
	if raw.Count < 1 || raw.Count > ExpeditionPartyCapacity {
		return trait, fmt.Errorf("trait count %d", raw.Count)
	}
	matches := 0
	for _, hero := range c.Heroes {
		if heroSellable(hero) && trait.Matches(hero, hero.Rarity) {
			matches++
		}
	}
	if matches < raw.Count {
		return trait, fmt.Errorf("only %d heroes match %+v", matches, raw)
	}
	return trait, nil
}

func parseExpeditionDrop(raw rawExpeditionDrop) (ExpeditionDrop, error) {
	drop := ExpeditionDrop{Kind: raw.Kind, Chance: raw.Chance, MinRarity: raw.MinRarity, MaxRarity: raw.MaxRarity}
	if raw.Chance <= 0 || raw.Chance > expeditionPercent {
		return drop, fmt.Errorf("drop chance %d", raw.Chance)
	}
	switch raw.Kind {
	case ExpeditionDropGear, ExpeditionDropRune:
		if raw.MinRarity < 1 || raw.MaxRarity < raw.MinRarity {
			return drop, fmt.Errorf("%s rarities %d-%d", raw.Kind, raw.MinRarity, raw.MaxRarity)
		}
	case ExpeditionDropHero:
		total := 0
		for name, weight := range raw.Weights {
			rarity := ParseRarity(name)
			if rarityNames[name] == 0 || rarity > RarityLegendary || weight < 0 {
				return drop, fmt.Errorf("hero weight %s=%d", name, weight)
			}
			drop.HeroWeights[rarity] = weight
			total += weight
		}
		if total == 0 {
			return drop, fmt.Errorf("hero drop has no weights")
		}
	default:
		return drop, fmt.Errorf("drop kind %q", raw.Kind)
	}
	return drop, nil
}

func (c *Catalog) parseExpedition(raw rawExpedition) (Expedition, error) {
	expedition := Expedition{
		ID: raw.ID, Level: raw.Level, Duration: time.Duration(raw.Minutes) * time.Minute, Party: raw.Party,
		HeroXP: raw.HeroXP,
	}
	if raw.ID == "" || raw.Minutes <= 0 || raw.Party < 1 || raw.Party > ExpeditionPartyCapacity || raw.HeroXP < 0 {
		return expedition, fmt.Errorf("bad shape")
	}
	for name, values := range map[string][]int64{"gold": raw.Gold, "gems": raw.Gems} {
		if len(values) != expeditionRangeEndpoints || values[0] < 0 || values[1] < values[0] {
			return expedition, fmt.Errorf("%s range %v", name, values)
		}
	}
	expedition.Gold = [expeditionRangeEndpoints]int64(raw.Gold)
	expedition.Gems = [expeditionRangeEndpoints]int64(raw.Gems)
	var err error
	if expedition.Bonus, err = c.parseExpeditionTrait(raw.Bonus); err != nil {
		return expedition, fmt.Errorf("bonus: %w", err)
	}
	for _, rawTrait := range raw.Requirements {
		trait, err := c.parseExpeditionTrait(rawTrait)
		if err != nil {
			return expedition, fmt.Errorf("requirement: %w", err)
		}
		expedition.Requirements = append(expedition.Requirements, trait)
	}
	for _, rawDrop := range raw.Drops {
		drop, err := parseExpeditionDrop(rawDrop)
		if err != nil {
			return expedition, err
		}
		expedition.Drops = append(expedition.Drops, drop)
	}
	return expedition, nil
}

func (c *Catalog) loadExpeditions(read func(name string) ([]byte, error)) error {
	var raw struct {
		Version         int             `json:"version"`
		SlotLevels      []int           `json:"slot_levels"`
		GreatSuccessPct int64           `json:"great_success_pct"`
		Expeditions     []rawExpedition `json:"expeditions"`
	}
	if err := decodeFile(read, ExpeditionsFile, &raw); err != nil {
		return err
	}
	if raw.Version != supportedExpeditions {
		return fmt.Errorf("%s version %d, want %d", ExpeditionsFile, raw.Version, supportedExpeditions)
	}
	if len(raw.SlotLevels) == 0 || len(raw.SlotLevels) > ExpeditionSlotCapacity || raw.GreatSuccessPct < 0 {
		return fmt.Errorf("%s has %d slot levels", ExpeditionsFile, len(raw.SlotLevels))
	}
	c.Expeditions = ExpeditionRules{
		SlotLevels: raw.SlotLevels, GreatSuccessPct: raw.GreatSuccessPct, index: map[string]int{},
	}
	for _, entry := range raw.Expeditions {
		expedition, err := c.parseExpedition(entry)
		if err != nil {
			return fmt.Errorf("%s expedition %q: %w", ExpeditionsFile, entry.ID, err)
		}
		if _, duplicate := c.Expeditions.index[expedition.ID]; duplicate {
			return fmt.Errorf("%s repeats expedition %s", ExpeditionsFile, expedition.ID)
		}
		c.Expeditions.index[expedition.ID] = len(c.Expeditions.List)
		c.Expeditions.List = append(c.Expeditions.List, expedition)
	}
	return nil
}
