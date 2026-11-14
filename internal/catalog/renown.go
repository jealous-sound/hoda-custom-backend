package catalog

import (
	"fmt"
	"slices"
)

const (
	RenownFile      = "renown.json"
	supportedRenown = 1

	RenownPowerPct     = "power_pct"
	RenownHealthPct    = "health_pct"
	RenownCritPermille = "crit_permille"
	RenownGoldPct      = "gold_pct"
	RenownVoucherPct   = "voucher_pct"
	RenownKeyPct       = "key_pct"
	RenownHeroXPPct    = "hero_xp_pct"
	RenownEnergyMax    = "energy_max"
	RenownStaminaMax   = "stamina_max"
)

var RenownPerks = []string{
	RenownPowerPct, RenownHealthPct, RenownCritPermille, RenownGoldPct, RenownVoucherPct, RenownKeyPct,
	RenownHeroXPPct, RenownEnergyMax, RenownStaminaMax,
}

type RenownRules struct {
	LevelsPerRank int
	RankGems      int
	PerkMaxRank   int
	PerkPerRank   map[string]int
}

func (r RenownRules) KnowsPerk(perk string) bool {
	_, known := r.PerkPerRank[perk]
	return known
}

func (r RenownRules) ClampPerkRank(rank int) int {
	return min(max(rank, 0), r.PerkMaxRank)
}

func (r RenownRules) PerkValue(perk string, rank int) int {
	return r.ClampPerkRank(rank) * r.PerkPerRank[perk]
}

func (c *Catalog) loadRenown(read func(name string) ([]byte, error)) error {
	var raw struct {
		Version       int            `json:"version"`
		LevelsPerRank int            `json:"levels_per_rank"`
		RankGems      int            `json:"rank_gems"`
		PerkMaxRank   int            `json:"perk_max_rank"`
		Perks         map[string]int `json:"perks"`
	}
	if err := decodeFile(read, RenownFile, &raw); err != nil {
		return err
	}
	if raw.Version != supportedRenown {
		return fmt.Errorf("%s version %d, want %d", RenownFile, raw.Version, supportedRenown)
	}
	if raw.LevelsPerRank <= 0 || raw.RankGems <= 0 || raw.PerkMaxRank <= 0 {
		return fmt.Errorf("%s needs positive levels_per_rank, rank_gems and perk_max_rank", RenownFile)
	}
	rules := RenownRules{
		LevelsPerRank: raw.LevelsPerRank, RankGems: raw.RankGems, PerkMaxRank: raw.PerkMaxRank,
		PerkPerRank: map[string]int{},
	}
	for _, perk := range RenownPerks {
		perRank, listed := raw.Perks[perk]
		if !listed {
			return fmt.Errorf("%s is missing perk %s", RenownFile, perk)
		}
		if perRank <= 0 {
			return fmt.Errorf("%s perk %s gives %d per rank, want a positive number", RenownFile, perk, perRank)
		}
		rules.PerkPerRank[perk] = perRank
	}
	for perk := range raw.Perks {
		if !slices.Contains(RenownPerks, perk) {
			return fmt.Errorf("%s names unknown perk %s", RenownFile, perk)
		}
	}
	c.Renown = rules
	return nil
}
