package battle

import (
	"encoding/json"
	"fmt"
)

const (
	heroLevelTable          = "unit-level-Default"
	ascendedLevelCap        = 100
	ascensionBonusLevels    = 5
	ascensionBonusGearSlots = 1
	baseCritChance          = 1
	unlockPromotion         = 1
	unlockCrit              = 3
)

type consumeProfile struct {
	levelTable   []int
	consumeTable []int
	progression  [][3]int
}

func extendXPTable(table []int, rows int) []int {
	if len(table) < 2 {
		return table
	}
	step := table[len(table)-1] - table[len(table)-2]
	for len(table) < rows {
		table = append(table, table[len(table)-1]+step)
	}
	return table
}

func loadConsume(data []byte) (map[string]*consumeProfile, error) {
	var root struct {
		XPTables     map[string][]int   `json:"xp_tables"`
		Progressions map[string][][]int `json:"progressions"`
		Characters   map[string]struct {
			LevelTable   string `json:"level_table"`
			ConsumeTable string `json:"consume_table"`
			Progression  string `json:"progression"`
		} `json:"characters"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("consume tables: %w", err)
	}
	if table, ok := root.XPTables[heroLevelTable]; ok {
		root.XPTables[heroLevelTable] = extendXPTable(table, ascendedLevelCap)
	}
	progressions := map[string][][3]int{}
	for name, rows := range root.Progressions {
		totals := make([][3]int, 0, len(rows))
		for _, row := range rows {
			if len(row) < 3 {
				return nil, fmt.Errorf("consume tables: progression %s has a short row", name)
			}
			totals = append(totals, [3]int{row[0], row[1], row[2]})
		}
		progressions[name] = totals
	}
	profiles := map[string]*consumeProfile{}
	for id, raw := range root.Characters {
		levels, levelsOK := root.XPTables[raw.LevelTable]
		consume, consumeOK := root.XPTables[raw.ConsumeTable]
		progression, progressionOK := progressions[raw.Progression]
		if !levelsOK || !consumeOK || !progressionOK {
			return nil, fmt.Errorf("consume tables: %s names a missing table", id)
		}
		profiles[id] = &consumeProfile{levelTable: levels, consumeTable: consume, progression: progression}
	}
	return profiles, nil
}

func xpForLevel(table []int, level int) int {
	level = min(max(level, 1), len(table))
	return table[level-1]
}

func levelForXP(table []int, xp int) int {
	for i, threshold := range table {
		if xp < threshold {
			return i
		}
	}
	return len(table)
}

func (p *consumeProfile) unlockValue(level, kind int) int {
	if level < 0 || level >= len(p.progression) {
		return 0
	}
	return p.progression[level][kind-1]
}

func (p *consumeProfile) unlockedTotal(level, kind int) int {
	total := 0
	for key := 0; key < level && key < len(p.progression); key++ {
		total = max(total, p.unlockValue(key, kind))
	}
	return total
}

func (p *consumeProfile) progressionLevelOfTier(tier Tier) int {
	for key := range p.progression {
		if p.unlockValue(key, unlockPromotion) == int(tier)+1 {
			return key
		}
	}
	return -1
}

func (p *consumeProfile) normalize(h *hero) {
	if key := p.progressionLevelOfTier(h.tier); key >= 0 {
		h.consumeXP = max(h.consumeXP, xpForLevel(p.consumeTable, key+1))
	}
	h.consumeXP = min(h.consumeXP, xpForLevel(p.consumeTable, len(p.consumeTable)))
	h.critChance = p.unlockedTotal(levelForXP(p.consumeTable, h.consumeXP), unlockCrit)
	h.level = min(h.level, min(h.stats[h.tier].maxLevel, len(p.levelTable)))
}
