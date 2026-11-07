package economy

import (
	"encoding/json"
	"slices"
	"strconv"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
)

const (
	fortifyField           = "fortify"
	removeEveryFortifySlot = -1
)

type fortifyChild struct {
	def      catalog.Hero
	tier     int
	ascended bool
}

func (f *fortifyChild) rarity() catalog.Rarity {
	if f.ascended {
		return catalog.RarityMythic
	}
	return f.def.Rarity
}

func (h *hero) asFortifyChild() *fortifyChild {
	return &fortifyChild{def: h.def, tier: h.tier, ascended: h.ascended}
}

type fortifySlots [catalog.FortifySlotCount]*fortifyChild

func readFortify(c *catalog.Catalog, entry map[string]any) fortifySlots {
	var slots fortifySlots
	stored, _ := entry[fortifyField].([]any)
	for _, item := range stored {
		object, _ := item.(map[string]any)
		slot, err := numberValue(object["slot"], "slot")
		id, _ := object["id"].(string)
		def, known := c.Hero(id)
		tierName, _ := object["tier"].(string)
		tier, tierKnown := catalog.TierIndex(tierName)
		if err != nil || slot < 0 || slot >= catalog.FortifySlotCount || slots[slot] != nil || !known ||
			!tierKnown || !def.Tiers[tier].Present {
			continue
		}
		ascended, _ := object["ascended"].(bool)
		slots[slot] = &fortifyChild{def: def, tier: tier, ascended: ascended}
	}
	return slots
}

func (s *fortifySlots) store(entry map[string]any) {
	stored := []any{}
	for slot, child := range s {
		if child == nil {
			continue
		}
		object := map[string]any{
			"slot": json.Number(strconv.Itoa(slot)), "id": child.def.ID, "tier": catalog.TierNames[child.tier],
		}
		if child.ascended {
			object["ascended"] = true
		}
		stored = append(stored, object)
	}
	if len(stored) == 0 {
		delete(entry, fortifyField)
		return
	}
	entry[fortifyField] = stored
}

func nextPresentTier(def catalog.Hero, tier int) int {
	for next := tier + 1; next < catalog.TierCount; next++ {
		if def.Tiers[next].Present {
			return next
		}
	}
	return tier
}

func fortifyInto(existing, sacrificed *fortifyChild) (*fortifyChild, bool) {
	if existing == nil || existing.def.ID != sacrificed.def.ID {
		return sacrificed, true
	}
	if existing.tier == existing.def.MaxTier && existing.rarity() >= sacrificed.rarity() {
		return nil, false
	}
	combinedTiers := min(max(sacrificed.tier+existing.tier+1, 1), catalog.TierCount)
	base := existing
	if sacrificed.rarity() > existing.rarity() {
		base = sacrificed
	}
	tier := base.tier
	for range combinedTiers - base.tier {
		tier = nextPresentTier(base.def, tier)
	}
	return &fortifyChild{def: base.def, tier: tier, ascended: base.ascended}, true
}

type FortifySlotRequest struct {
	Index int   `json:"index"`
	Unit  int64 `json:"unit"`
}

type FortifyRequest struct {
	Parent int64                `json:"parent"`
	Slots  []FortifySlotRequest `json:"slots"`
}

func (p *player) FortifyHero(request FortifyRequest) (BattleResult, error) {
	if len(request.Slots) == 0 || len(request.Slots) > catalog.FortifySlotCount {
		return BattleResult{}, refuse(Unavailable, "fortifying needs 1-%d heroes", catalog.FortifySlotCount)
	}
	r, err := loadRoster(p.catalog, p.doc)
	if err != nil {
		return BattleResult{}, err
	}
	parent := r.find(request.Parent)
	if parent == nil {
		return BattleResult{}, refuse(Unavailable, "no hero %d", request.Parent)
	}
	unlocked := parent.unlockedFortifySlots()
	slots := readFortify(p.catalog, parent.entry)
	var chosen [catalog.FortifySlotCount]bool
	sacrificed := make([]int64, 0, len(request.Slots))
	for _, slot := range request.Slots {
		if slot.Index < 0 || slot.Index >= unlocked || chosen[slot.Index] {
			return BattleResult{}, refuse(Unavailable, "fortify slot %d is not open", slot.Index)
		}
		chosen[slot.Index] = true
		child := r.find(slot.Unit)
		if child == nil || slot.Unit == request.Parent || slices.Contains(sacrificed, slot.Unit) ||
			inAnySquad(p.doc, slot.Unit) {
			return BattleResult{}, refuse(Unavailable, "hero %d cannot fortify", slot.Unit)
		}
		merged, replaceable := fortifyInto(slots[slot.Index], child.asFortifyChild())
		if !replaceable {
			return BattleResult{}, refuse(Unavailable, "fortify slot %d already holds the best %s", slot.Index,
				child.def.ID)
		}
		slots[slot.Index] = merged
		sacrificed = append(sacrificed, slot.Unit)
	}
	slots.store(parent.entry)
	r.remove(sacrificed)
	if err := removeGearOf(p.doc, sacrificed); err != nil {
		return BattleResult{}, err
	}
	return BattleResult{}, nil
}

type RemoveFortifyRequest struct {
	Parent int64 `json:"parent"`
	Index  *int  `json:"index"`
}

func (p *player) RemoveFortify(request RemoveFortifyRequest) (BattleResult, error) {
	r, err := loadRoster(p.catalog, p.doc)
	if err != nil {
		return BattleResult{}, err
	}
	parent := r.find(request.Parent)
	if parent == nil {
		return BattleResult{}, refuse(Unavailable, "no hero %d", request.Parent)
	}
	if request.Index == nil ||
		(*request.Index != removeEveryFortifySlot && (*request.Index < 0 || *request.Index >= catalog.FortifySlotCount)) {
		return BattleResult{}, refuse(Unavailable, "no fortify slot to remove")
	}
	slots := readFortify(p.catalog, parent.entry)
	var removed []*fortifyChild
	cost := int64(0)
	for slot, child := range slots {
		if child == nil || (*request.Index != removeEveryFortifySlot && *request.Index != slot) {
			continue
		}
		removed = append(removed, child)
		cost += p.catalog.Fortify.RemoveCost(child.rarity())
		slots[slot] = nil
	}
	if len(removed) == 0 {
		return BattleResult{}, refuse(Unavailable, "hero %d has no fortified slot %d", request.Parent, *request.Index)
	}
	writer, err := newRosterWriter(p.doc)
	if err != nil {
		return BattleResult{}, err
	}
	if !writer.hasRoom(len(removed)) {
		return BattleResult{}, refuse(InventoryFull, "roster has no room for %d heroes", len(removed))
	}
	if err := p.account.spendGems(cost); err != nil {
		return BattleResult{}, err
	}
	slots.store(parent.entry)
	for _, child := range removed {
		instance, err := p.returnFortifyChild(writer, child)
		if err != nil {
			return BattleResult{}, err
		}
		p.granted.Heroes = append(p.granted.Heroes, GrantedHero{ID: child.def.ID, InstanceID: instance})
	}
	writer.store()
	return BattleResult{Granted: p.granted}, nil
}

func (p *player) returnFortifyChild(writer *rosterWriter, child *fortifyChild) (int64, error) {
	instance, entry := writer.addEntry(child.def)
	entry["tier"] = catalog.TierNames[child.tier]
	entry["ascended"] = child.ascended
	if returned, known := readHero(p.catalog, entry); known {
		returned.store()
	}
	if !child.ascended {
		return instance, nil
	}
	gear, err := p.gearHero(instance)
	if err != nil {
		return 0, err
	}
	gear["ascended"] = true
	return instance, nil
}
