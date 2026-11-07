package economy

import (
	"math"
	"slices"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
)

const (
	ascensionBonusLevels              = 5
	consumeSameFactionBonus   float32 = 0.2
	consumeHeroXPShareOfWorth float32 = 0.1
	consumedMissionStat               = "consumed"
	combinedMissionStat               = "combined"
	squadSlotCount                    = 5
	noProgressionKey                  = -1
)

var (
	consumeWorthStandard = [catalog.RarityMythic]float32{100, 200, 600, 2000, 5000, 10000}
	consumeWorthGiant    = [catalog.RarityMythic]float32{150, 300, 900, 3000, 7500, 15000}
	rosterLists          = []string{"starters", "additions"}
)

type hero struct {
	entry     map[string]any
	def       catalog.Hero
	instance  int64
	tier      int
	level     int64
	xp        int64
	consumeXP int64
	ascended  bool
}

func xpForLevel(table []int64, level int64) int64 {
	return table[min(max(level, 1), int64(len(table)))-1]
}

func levelForXP(table []int64, xp int64) int64 {
	for index, threshold := range table {
		if xp < threshold {
			return int64(index)
		}
	}
	return int64(len(table))
}

func readHero(c *catalog.Catalog, entry map[string]any) (*hero, bool) {
	id, _ := entry["id"].(string)
	def, known := c.Hero(id)
	instance, err := numberValue(entry["instance_id"], "instance_id")
	if !known || err != nil {
		return nil, false
	}
	h := &hero{entry: entry, def: def, instance: instance, level: newHeroLevel}
	h.tier, _ = catalog.TierIndex(def.InitialTier)
	if name, ok := entry["tier"].(string); ok {
		if tier, valid := catalog.TierIndex(name); valid && def.Tiers[tier].Present {
			h.tier = tier
		}
	}
	if level, err := numberValue(entry["level"], "level"); err == nil {
		h.level = max(level, newHeroLevel)
	}
	h.xp, _ = numberValue(entry["xp"], "xp")
	h.consumeXP, _ = numberValue(entry["consume_xp"], "consume_xp")
	h.ascended, _ = entry["ascended"].(bool)
	h.normalize()
	return h, true
}

func (h *hero) store() {
	h.entry["tier"] = catalog.TierNames[h.tier]
	document(h.entry).setInt("level", h.level)
	document(h.entry).setInt("xp", h.xp)
	document(h.entry).setInt("consume_xp", h.consumeXP)
	h.entry["ascended"] = h.ascended
}

func (h *hero) profile() *catalog.ConsumeProfile {
	return h.def.Consume
}

func (h *hero) rarity() catalog.Rarity {
	if h.ascended {
		return catalog.RarityMythic
	}
	return h.def.Rarity
}

func (h *hero) levelCap(tier int) int64 {
	levelLimit := int64(h.def.Tiers[tier].MaxLevel)
	if h.ascended {
		levelLimit += ascensionBonusLevels
	}
	return min(levelLimit, int64(len(h.profile().LevelTable)))
}

func (h *hero) progressionKeyOfTier(tier int) int {
	for key := range h.profile().Progression {
		if h.profile().PromotionAt(key) == tier+1 {
			return key
		}
	}
	return noProgressionKey
}

func (h *hero) unlockedTotal(consumeLevel int64, unlockAt func(key int) int) int {
	total := 0
	for key := 0; int64(key) < consumeLevel && key < len(h.profile().Progression); key++ {
		total = max(total, unlockAt(key))
	}
	return total
}

func (h *hero) unlockedPromotion(consumeLevel int64) int {
	return h.unlockedTotal(consumeLevel, h.profile().PromotionAt)
}

func (h *hero) unlockedFortifySlots() int {
	consumeLevel := levelForXP(h.profile().ConsumeTable, h.consumeXP)
	return min(h.unlockedTotal(consumeLevel, h.profile().FortifySlotsAt), catalog.FortifySlotCount)
}

func (h *hero) reachableTier(wanted int) int {
	for tier := min(wanted, h.def.MaxTier); tier > h.tier; tier-- {
		if h.def.Tiers[tier].Present {
			return tier
		}
	}
	return h.tier
}

func (h *hero) normalize() {
	consume := h.profile().ConsumeTable
	if key := h.progressionKeyOfTier(h.tier); key != noProgressionKey {
		h.consumeXP = max(h.consumeXP, xpForLevel(consume, int64(key)+1))
	}
	h.consumeXP = min(h.consumeXP, xpForLevel(consume, int64(len(consume))))
	levelLimit := h.levelCap(h.tier)
	h.level = min(h.level, levelLimit)
	levels := h.profile().LevelTable
	low := xpForLevel(levels, h.level)
	high := low
	if h.level < levelLimit {
		high = xpForLevel(levels, h.level+1) - 1
	}
	h.xp = min(max(h.xp, low), high)
}

func addCapped(value, amount, limit int64) int64 {
	if value > limit-amount {
		return limit
	}
	return value + amount
}

func (h *hero) awardXP(xp int64) {
	if xp <= 0 {
		return
	}
	levelLimit := h.levelCap(h.tier)
	h.xp = addCapped(h.xp, xp, xpForLevel(h.profile().LevelTable, levelLimit))
	h.level = max(h.level, min(levelForXP(h.profile().LevelTable, h.xp), levelLimit))
}

func roundToEven(value float32) float32 {
	return float32(math.RoundToEven(float64(value)))
}

func (h *hero) baseWorth() float32 {
	table := consumeWorthStandard
	if h.def.Giant {
		table = consumeWorthGiant
	}
	index := min(max(int(h.rarity())-1, 0), len(table)-1)
	return roundToEven(table[index] * h.profile().Worth[h.tier])
}

func (h *hero) heroXPWorth() int64 {
	return h.xp + int64(roundToEven(float32(h.baseWorth()*consumeHeroXPShareOfWorth)))
}

func sharesFaction(a, b *hero) bool {
	if a.def.Giant && b.def.Giant {
		return true
	}
	return a.def.Factions&b.def.Factions != 0
}

func (h *hero) consumeWorth(target *hero) int64 {
	worth := h.baseWorth()
	bonus := float32(0)
	if sharesFaction(h, target) {
		bonus = consumeSameFactionBonus
	}
	return int64(roundToEven(worth + float32(worth*bonus)))
}

func (h *hero) combineGain(seed *hero, tierCursor *int) int64 {
	if seed.def.ID != h.def.ID {
		return 0
	}
	tiers := min(seed.tier+1, h.def.MaxTier-*tierCursor)
	if tiers <= 0 {
		return 0
	}
	from, to := h.progressionKeyOfTier(*tierCursor), h.progressionKeyOfTier(*tierCursor+tiers)
	*tierCursor += tiers
	if from == noProgressionKey || to == noProgressionKey {
		return 0
	}
	consume := h.profile().ConsumeTable
	return xpForLevel(consume, int64(to)+1) - xpForLevel(consume, int64(from)+1)
}

func (h *hero) consume(seeds []*hero) {
	consumeGain, xpGain, tierCursor := int64(0), int64(0), h.tier
	for _, seed := range seeds {
		consumeGain += h.combineGain(seed, &tierCursor)
		consumeGain += seed.consumeWorth(h)
		xpGain += seed.heroXPWorth()
	}
	consume := h.profile().ConsumeTable
	h.consumeXP = addCapped(h.consumeXP, consumeGain, xpForLevel(consume, int64(len(consume))))
	h.tier = h.reachableTier(h.unlockedPromotion(levelForXP(consume, h.consumeXP)) - 1)
	levelLimit := h.levelCap(h.tier)
	levels := h.profile().LevelTable
	h.xp = addCapped(h.xp, xpGain, xpForLevel(levels, levelLimit))
	h.level = min(levelForXP(levels, h.xp), levelLimit)
}

type roster struct {
	section document
	heroes  []*hero
}

func loadRoster(c *catalog.Catalog, doc document) (*roster, error) {
	section, err := doc.section("roster")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	r := &roster{section: section}
	for _, list := range rosterLists {
		entries, _ := section.array(list)
		for _, entry := range entries {
			object, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			if h, known := readHero(c, object); known {
				r.heroes = append(r.heroes, h)
			}
		}
	}
	return r, nil
}

func (r *roster) find(instance int64) *hero {
	for _, h := range r.heroes {
		if h.instance == instance {
			return h
		}
	}
	return nil
}

func (r *roster) remove(instances []int64) {
	for _, list := range rosterLists {
		entries, err := r.section.array(list)
		if entries == nil || err != nil {
			continue
		}
		r.section[list] = slices.DeleteFunc(slices.Clone(entries), func(entry any) bool {
			object, _ := entry.(map[string]any)
			id, err := numberValue(object["instance_id"], "instance_id")
			return err == nil && slices.Contains(instances, id)
		})
	}
	starters, _ := r.section.array("starters")
	r.section.setInt("starter_count", int64(len(starters)))
	r.heroes = slices.DeleteFunc(r.heroes, func(h *hero) bool { return slices.Contains(instances, h.instance) })
}

func squadInstances(doc document, squad int) []int64 {
	squads, _ := doc.array("squads")
	if squad < 0 || squad >= len(squads) {
		return nil
	}
	cells, _ := squads[squad].([]any)
	var instances []int64
	for slot, cell := range cells {
		if slot >= squadSlotCount {
			break
		}
		if id, err := numberValue(cell, "squad slot"); err == nil && !slices.Contains(instances, id) {
			instances = append(instances, id)
		}
	}
	return instances
}

func currentSquad(doc document) []int64 {
	squad, _ := doc.intField("current_squad")
	return squadInstances(doc, squad)
}

func inAnySquad(doc document, instance int64) bool {
	for squad := range PlayerMaxSquads {
		if slices.Contains(squadInstances(doc, squad), instance) {
			return true
		}
	}
	return false
}

func (p *player) awardSquadXP(xp int64) error {
	if xp <= 0 {
		return nil
	}
	r, err := loadRoster(p.catalog, p.doc)
	if err != nil {
		return err
	}
	fighters := p.fighters
	if fighters == nil {
		fighters = currentSquad(p.doc)
	}
	for _, instance := range fighters {
		if h := r.find(instance); h != nil {
			h.awardXP(xp)
			h.store()
		}
	}
	return nil
}

type ConsumeRequest struct {
	Target int64   `json:"target"`
	Seeds  []int64 `json:"seeds"`
}

func (p *player) ConsumeHeroes(request ConsumeRequest) (BattleResult, error) {
	if len(request.Seeds) == 0 || len(request.Seeds) > RosterCapacity {
		return BattleResult{}, refuse(Unavailable, "consuming needs 1-%d heroes", RosterCapacity)
	}
	r, err := loadRoster(p.catalog, p.doc)
	if err != nil {
		return BattleResult{}, err
	}
	target := r.find(request.Target)
	if target == nil {
		return BattleResult{}, refuse(Unavailable, "no hero %d", request.Target)
	}
	seeds := make([]*hero, 0, len(request.Seeds))
	combined := int64(0)
	for index, instance := range request.Seeds {
		seed := r.find(instance)
		if seed == nil || instance == request.Target || slices.Contains(request.Seeds[:index], instance) ||
			inAnySquad(p.doc, instance) {
			return BattleResult{}, refuse(Unavailable, "hero %d cannot be consumed", instance)
		}
		if seed.def.ID == target.def.ID {
			combined++
		}
		seeds = append(seeds, seed)
	}
	target.consume(seeds)
	target.store()
	r.remove(request.Seeds)
	if err := removeGearOf(p.doc, request.Seeds); err != nil {
		return BattleResult{}, err
	}
	p.count(consumedMissionStat, int64(len(seeds)))
	p.count(combinedMissionStat, combined)
	return BattleResult{}, nil
}

func removeGearOf(doc document, instances []int64) error {
	gear, err := ensureGearSection(doc)
	if err != nil {
		return err
	}
	heroes, _ := gear.array("heroes")
	gear["heroes"] = slices.DeleteFunc(slices.Clone(heroes), func(entry any) bool {
		object, _ := entry.(map[string]any)
		id, err := numberValue(object["instance_id"], "instance_id")
		return err == nil && slices.Contains(instances, id)
	})
	return nil
}
