package battle

import (
	"encoding/json"
	"fmt"
	"math"
)

const (
	FortifySlotCount       = 10
	supportedFortifyTables = 1
	fortifyStatHealth      = "health"
	fortifyStatPower       = "power"
)

type FortifyChild struct {
	Slot     int    `json:"slot"`
	ID       string `json:"id"`
	Tier     string `json:"tier"`
	Ascended bool   `json:"ascended,omitempty"`
}

type fortifySlot struct {
	power bool
	cap   float32
}

type fortifyArchetype [FortifySlotCount]fortifySlot

type fortifyTables struct {
	sameFactionMultiplier float32
	rarityTierBonus       [rarityMythic][tierCount]float32
	archetypes            map[string]*fortifyArchetype
}

func loadFortify(data []byte) (*fortifyTables, error) {
	var root struct {
		Version         int         `json:"version"`
		FactionBonus    float64     `json:"faction_bonus"`
		RarityTierBonus [][]float64 `json:"rarity_tier_bonus"`
		Archetypes      map[string][]struct {
			Stat string  `json:"stat"`
			Cap  float64 `json:"cap"`
		} `json:"archetypes"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("fortify tables: %w", err)
	}
	if root.Version != supportedFortifyTables {
		return nil, fmt.Errorf("fortify tables: version %d, want %d", root.Version, supportedFortifyTables)
	}
	if len(root.RarityTierBonus) != int(rarityMythic) {
		return nil, fmt.Errorf("fortify tables: %d rarity rows, want %d", len(root.RarityTierBonus), rarityMythic)
	}
	factionBonus := float32(root.FactionBonus)
	tables := &fortifyTables{
		sameFactionMultiplier: float32(float32(1)+factionBonus) - 1,
		archetypes:            map[string]*fortifyArchetype{},
	}
	for r, row := range root.RarityTierBonus {
		if len(row) != int(tierCount) {
			return nil, fmt.Errorf("fortify tables: rarity row %d has %d tiers", r, len(row))
		}
		for t, bonus := range row {
			tables.rarityTierBonus[r][t] = float32(bonus)
		}
	}
	for name, slots := range root.Archetypes {
		if len(slots) != FortifySlotCount {
			return nil, fmt.Errorf("fortify tables: archetype %s has %d slots", name, len(slots))
		}
		archetype := &fortifyArchetype{}
		for i, slot := range slots {
			if slot.Stat != fortifyStatHealth && slot.Stat != fortifyStatPower {
				return nil, fmt.Errorf("fortify tables: archetype %s slot %d fortifies %q", name, i, slot.Stat)
			}
			archetype[i] = fortifySlot{power: slot.Stat == fortifyStatPower, cap: float32(slot.Cap)}
		}
		tables.archetypes[name] = archetype
	}
	return tables, nil
}

type fortifyBonus struct {
	power                 bool
	rarity, faction, tier int
}

func (b fortifyBonus) total() int {
	return b.rarity + b.faction + b.tier
}

func roundHalfEven(value float32) int {
	return int(math.RoundToEven(float64(value)))
}

func fortifySharesFaction(parent, child *character) bool {
	if parent.giant && child.giant {
		return true
	}
	return factionMask(parent.faction)&factionMask(child.faction) != 0
}

func (h *hero) maxLevelStat(power bool) int {
	st := &h.stats[h.tier]
	if power {
		return st.basePower + (st.maxLevel-1)*st.powerPerLevel
	}
	return st.baseHealth + (st.maxLevel-1)*st.healthPerLevel
}

func (d *Data) fortifyBonus(parent *hero, archetype *fortifyArchetype, child FortifyChild) (fortifyBonus, error) {
	if child.Slot < 0 || child.Slot >= FortifySlotCount {
		return fortifyBonus{}, fmt.Errorf("fortify slot %d", child.Slot)
	}
	def, ok := d.characters[child.ID]
	if !ok {
		return fortifyBonus{}, fmt.Errorf("unknown fortify child %s", child.ID)
	}
	tier := ParseTier(child.Tier)
	if tier.String() != child.Tier || !def.hasTier[tier] {
		return fortifyBonus{}, fmt.Errorf("fortify child %s has no tier %q", child.ID, child.Tier)
	}
	childRarity := def.rarity
	if child.Ascended {
		childRarity = rarityMythic
	}
	slot := archetype[child.Slot]
	base := mul(float32(parent.maxLevelStat(slot.power)), slot.cap)
	row := &d.fortify.rarityTierBonus[childRarity-1]
	bonus := fortifyBonus{power: slot.power}
	bonus.rarity = roundHalfEven(mul(base, row[TierI]))
	bonus.tier = roundHalfEven(mul(base, float32(row[tier]-row[TierI])))
	if fortifySharesFaction(parent.def, def) {
		bonus.faction = roundHalfEven(mul(float32(bonus.rarity+bonus.tier), d.fortify.sameFactionMultiplier))
	}
	return bonus, nil
}

func (d *Data) fortifyHero(h *hero, archetype *fortifyArchetype, children []FortifyChild) error {
	if len(children) > FortifySlotCount {
		return fmt.Errorf("unit %s has %d fortified slots", h.def.id, len(children))
	}
	var filled [FortifySlotCount]bool
	for _, child := range children {
		bonus, err := d.fortifyBonus(h, archetype, child)
		if err != nil {
			return fmt.Errorf("unit %s: %w", h.def.id, err)
		}
		if filled[child.Slot] {
			return fmt.Errorf("unit %s fortifies slot %d twice", h.def.id, child.Slot)
		}
		filled[child.Slot] = true
		if bonus.power {
			h.fortifyPower += bonus.total()
		} else {
			h.fortifyHealth += bonus.total()
		}
	}
	return nil
}
