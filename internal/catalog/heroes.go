package catalog

import (
	"fmt"
	"strings"
)

const (
	TierCount               = 4
	supportedHeroPool       = 2
	supportedConsumeTables  = 1
	heroLevelTable          = "unit-level-Default"
	ascendedHeroLevelRows   = 100
	allFactionsName         = "All"
	dualFactionSeparator    = "/"
	progressionUnlockKinds  = 3
	promotionUnlock         = 0
	fortifyUnlock           = 1
	minimumExtendedRowCount = 2
)

var TierNames = [TierCount]string{"I", "II", "III", "IV"}

func TierIndex(name string) (int, bool) {
	for index, tier := range TierNames {
		if tier == name {
			return index, true
		}
	}
	return 0, false
}

type Faction uint8

const (
	FactionWhite Faction = 1 << iota
	FactionRed
	FactionBlue
	FactionBlack
)

var factionNames = map[string]Faction{
	"White": FactionWhite, "Red": FactionRed, "Blue": FactionBlue, "Black": FactionBlack,
}

func parseFactions(name string) Faction {
	if name == allFactionsName {
		return FactionWhite | FactionRed | FactionBlue | FactionBlack
	}
	var factions Faction
	for _, part := range strings.Split(name, dualFactionSeparator) {
		factions |= factionNames[part]
	}
	return factions
}

type HeroTier struct {
	Present  bool
	MaxLevel int
}

type ConsumeProfile struct {
	LevelTable   []int64
	ConsumeTable []int64
	Progression  [][progressionUnlockKinds]int
	Worth        [TierCount]float32
}

func (p *ConsumeProfile) unlockAt(key, kind int) int {
	if key < 0 || key >= len(p.Progression) {
		return 0
	}
	return p.Progression[key][kind]
}

func (p *ConsumeProfile) PromotionAt(key int) int {
	return p.unlockAt(key, promotionUnlock)
}

func (p *ConsumeProfile) FortifySlotsAt(key int) int {
	return p.unlockAt(key, fortifyUnlock)
}

type rawConsumeTables struct {
	Version      int                                      `json:"version"`
	XPTables     map[string][]int64                       `json:"xp_tables"`
	Progressions map[string][][progressionUnlockKinds]int `json:"progressions"`
	Characters   map[string]struct {
		LevelTable   string             `json:"level_table"`
		ConsumeTable string             `json:"consume_table"`
		Progression  string             `json:"progression"`
		Worth        [TierCount]float64 `json:"worth"`
	} `json:"characters"`
}

func extendLinearly(table []int64, rows int) []int64 {
	if len(table) < minimumExtendedRowCount {
		return table
	}
	extended := append([]int64(nil), table...)
	step := extended[len(extended)-1] - extended[len(extended)-2]
	for len(extended) < rows {
		extended = append(extended, extended[len(extended)-1]+step)
	}
	return extended
}

func (c *Catalog) loadConsumeTables(read func(name string) ([]byte, error)) error {
	var raw rawConsumeTables
	if err := decodeFile(read, "consume_tables.json", &raw); err != nil {
		return err
	}
	if raw.Version != supportedConsumeTables {
		return fmt.Errorf("consume_tables.json version %d, want %d", raw.Version, supportedConsumeTables)
	}
	tables := map[string][]int64{}
	for name, table := range raw.XPTables {
		if len(table) == 0 {
			return fmt.Errorf("consume_tables.json XP table %s is empty", name)
		}
		if name == heroLevelTable {
			table = extendLinearly(table, ascendedHeroLevelRows)
		}
		tables[name] = table
	}
	profiles := map[string]*ConsumeProfile{}
	for id, character := range raw.Characters {
		levels, levelsKnown := tables[character.LevelTable]
		consume, consumeKnown := tables[character.ConsumeTable]
		progression, progressionKnown := raw.Progressions[character.Progression]
		if !levelsKnown || !consumeKnown || !progressionKnown {
			return fmt.Errorf("consume profile %s names an unknown table", id)
		}
		profile := &ConsumeProfile{LevelTable: levels, ConsumeTable: consume, Progression: progression}
		for tier, worth := range character.Worth {
			profile.Worth[tier] = float32(worth)
		}
		profiles[id] = profile
	}
	for index := range c.Heroes {
		profile, ok := profiles[c.Heroes[index].ID]
		if !ok {
			return fmt.Errorf("hero %s has no consume profile", c.Heroes[index].ID)
		}
		c.Heroes[index].Consume = profile
	}
	return nil
}
