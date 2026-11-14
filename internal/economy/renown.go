package economy

import (
	"math"

	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
)

const (
	renownSection     = "renown"
	renownPerksField  = "perks"
	experienceCeiling = math.MaxInt32
)

func virtualLevel(xp int64) int {
	xp = min(xp, experienceCeiling)
	level := 1
	for cumulativeXPThroughLevel(level) <= xp {
		level++
	}
	return level
}

func RenownRank(rules *catalog.RenownRules, xp int64) int {
	return virtualLevel(xp) / rules.LevelsPerRank
}

type renown struct {
	rules *catalog.RenownRules
	ranks map[string]int
}

func newRenown(rules *catalog.RenownRules) *renown {
	return &renown{rules: rules, ranks: map[string]int{}}
}

func loadRenown(rules *catalog.RenownRules, doc document) *renown {
	section, _ := doc.optionalSection(renownSection)
	return readRenown(rules, section)
}

func readRenown(rules *catalog.RenownRules, section document) *renown {
	r := newRenown(rules)
	perks, _ := section.optionalSection(renownPerksField)
	for _, perk := range catalog.RenownPerks {
		if rank, err := perks.intField(perk); err == nil {
			r.ranks[perk] = rules.ClampPerkRank(rank)
		}
	}
	return r
}

func (r *renown) withinRank(xp int64) *renown {
	if r.spent() > r.rank(xp) {
		return newRenown(r.rules)
	}
	return r
}

func (r *renown) store(doc document) {
	perks := document{}
	for perk, rank := range r.ranks {
		perks.setInt(perk, int64(rank))
	}
	section, kept := doc.optionalSection(renownSection)
	if !kept {
		section = document{}
		doc[renownSection] = map[string]any(section)
	}
	section[renownPerksField] = map[string]any(perks)
}

func (r *renown) value(perk string) int {
	return r.rules.PerkValue(perk, r.ranks[perk])
}

func (r *renown) spent() int {
	spent := 0
	for _, rank := range r.ranks {
		spent += rank
	}
	return spent
}

func (r *renown) rank(xp int64) int {
	return RenownRank(r.rules, xp)
}

func (r *renown) freePoints(xp int64) int {
	return r.rank(xp) - r.spent()
}

func (r *renown) rankExperienceBounds(xp int64) (low, high int64) {
	rank, levels := r.rank(xp), r.rules.LevelsPerRank
	return cumulativeXPThroughLevel(levels*rank - 1), cumulativeXPThroughLevel(levels*(rank+1) - 1)
}

func (r *renown) sideBonus() *battle.SideRenown {
	return battle.NewSideRenown(
		r.value(catalog.RenownPowerPct), r.value(catalog.RenownHealthPct), r.value(catalog.RenownCritPermille),
	)
}

func energyMaxWith(level int, perks *renown) int64 {
	return energyMaxForLevel(level) + int64(perks.value(catalog.RenownEnergyMax))
}

func staminaMaxWith(perks *renown) int64 {
	return StartStaminaMax + int64(perks.value(catalog.RenownStaminaMax))
}

type RenownRequest struct {
	Perk string `json:"perk"`
}

func (p *player) UpgradeRenown(request RenownRequest) (BattleResult, error) {
	perks := p.account.renown
	if !perks.rules.KnowsPerk(request.Perk) {
		return BattleResult{}, refuse(Unavailable, "no renown perk %q", request.Perk)
	}
	if perks.ranks[request.Perk] >= perks.rules.PerkMaxRank {
		return BattleResult{}, refuse(AlreadyFull, "renown perk %s is already at rank %d", request.Perk,
			perks.rules.PerkMaxRank)
	}
	if perks.freePoints(p.account.Experience) <= 0 {
		return BattleResult{}, refuse(CantAfford, "renown perk %s needs a free renown point", request.Perk)
	}
	perks.ranks[request.Perk]++
	perks.store(p.doc)
	p.account.EnergyMax = energyMaxWith(p.account.Level, perks)
	p.account.StaminaMax = staminaMaxWith(perks)
	return BattleResult{}, nil
}
