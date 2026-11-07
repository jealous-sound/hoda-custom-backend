package catalog

import (
	"fmt"

	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
)

const (
	FortifySlotCount       = battle.FortifySlotCount
	supportedFortifyTables = 1
)

type FortifyRules struct {
	RemoveGemCost [RarityMythic]int64
}

func (f FortifyRules) RemoveCost(rarity Rarity) int64 {
	index := min(max(int(rarity), int(RarityCommon)), int(RarityMythic)) - 1
	return f.RemoveGemCost[index]
}

func (c *Catalog) loadFortifyRules(read func(name string) ([]byte, error)) error {
	var raw struct {
		Version       int     `json:"version"`
		RemoveGemCost []int64 `json:"remove_gem_cost"`
	}
	if err := decodeFile(read, battle.FortifyFile, &raw); err != nil {
		return err
	}
	if raw.Version != supportedFortifyTables {
		return fmt.Errorf("%s version %d, want %d", battle.FortifyFile, raw.Version, supportedFortifyTables)
	}
	if len(raw.RemoveGemCost) != len(c.Fortify.RemoveGemCost) {
		return fmt.Errorf("%s has %d remove costs, want one per rarity", battle.FortifyFile, len(raw.RemoveGemCost))
	}
	for index, cost := range raw.RemoveGemCost {
		if cost < 0 {
			return fmt.Errorf("%s remove cost %d is negative", battle.FortifyFile, index)
		}
		c.Fortify.RemoveGemCost[index] = cost
	}
	return nil
}
