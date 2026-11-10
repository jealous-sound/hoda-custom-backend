package economy

import (
	"slices"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
)

const (
	RunePrestigeGems   = 100
	PlayerMaxSquads    = 4
	TutorialGiant      = "giant"
	TutorialGemPack    = "gem_pack"
	TutorialPartingGft = "parting_gift"
)

type GearRequest struct {
	Gear       string `json:"gear"`
	InstanceID int64  `json:"instance_id"`
}

func (p *player) gearSection() (document, error) {
	return ensureGearSection(p.doc)
}

func (p *player) SellGear(request GearRequest) (BattleResult, error) {
	index, ok := p.catalog.GearIndex(request.Gear)
	if !ok {
		return BattleResult{}, refuse(Unavailable, "no gear %q", request.Gear)
	}
	gear, err := p.doc.section("gear")
	if err != nil {
		return BattleResult{}, refuse(BadProfile, "%v", err)
	}
	inventory, err := gear.section("inventory")
	if err != nil {
		return BattleResult{}, refuse(BadProfile, "%v", err)
	}
	count, _ := inventory.optionalInt(request.Gear)
	if count <= 0 {
		return BattleResult{}, refuse(Unavailable, "no %s in the inventory", request.Gear)
	}
	if count == 1 {
		delete(inventory, request.Gear)
	} else {
		inventory.setInt(request.Gear, int64(count-1))
	}
	result := BattleResult{Gold: int64(p.catalog.GearSellValue(p.catalog.Gear[index]))}
	p.addGold(result.Gold)
	return result, nil
}

func (p *player) rosterHero(instance int64) (map[string]any, bool) {
	roster, _ := p.doc.optionalSection("roster")
	for _, list := range []string{"starters", "additions"} {
		entries, _ := roster.array(list)
		for _, entry := range entries {
			object, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			if id, err := numberValue(object["instance_id"], "instance_id"); err == nil && id == instance {
				return object, true
			}
		}
	}
	return nil, false
}

func (p *player) gearHero(instance int64) (map[string]any, error) {
	gear, err := p.gearSection()
	if err != nil {
		return nil, err
	}
	heroes, _ := gear.array("heroes")
	for _, entry := range heroes {
		object, ok := entry.(map[string]any)
		if id, err := numberValue(object["instance_id"], "instance_id"); ok && err == nil && id == instance {
			return object, nil
		}
	}
	created := map[string]any{
		"instance_id": numberArray([]int64{instance})[0], "ascended": false, "prestige_unlocked": false,
		"slots": []any{},
	}
	gear["heroes"] = append(heroes, created)
	return created, nil
}

func (p *player) UnlockGearPrestige(request GearRequest) (BattleResult, error) {
	if _, ok := p.rosterHero(request.InstanceID); !ok {
		return BattleResult{}, refuse(Unavailable, "no hero %d", request.InstanceID)
	}
	entry, err := p.gearHero(request.InstanceID)
	if err != nil {
		return BattleResult{}, err
	}
	if unlocked, _ := entry["prestige_unlocked"].(bool); unlocked {
		return BattleResult{}, refuse(AlreadyFull, "hero %d already has the prestige slot", request.InstanceID)
	}
	if err := p.account.spendGems(int64(p.catalog.GearPrestigeGems)); err != nil {
		return BattleResult{}, err
	}
	entry["prestige_unlocked"] = true
	return BattleResult{}, nil
}

func (p *player) takesTome(definition catalog.Hero) bool {
	return definition.Rarity < catalog.RarityMythic && p.catalog.Battle.HasPrestigeGearSlot(definition.ID)
}

func (p *player) AscendHero(request GearRequest) (BattleResult, error) {
	hero, ok := p.rosterHero(request.InstanceID)
	if !ok {
		return BattleResult{}, refuse(Unavailable, "no hero %d", request.InstanceID)
	}
	name, _ := hero["id"].(string)
	definition, known := p.catalog.Hero(name)
	if ascended, _ := hero["ascended"].(bool); ascended || !known || !p.takesTome(definition) {
		return BattleResult{}, refuse(Unavailable, "hero %d cannot ascend", request.InstanceID)
	}
	gear, err := p.gearSection()
	if err != nil {
		return BattleResult{}, err
	}
	tomes, _ := gear.optionalInt("tomes")
	if tomes <= 0 {
		return BattleResult{}, refuse(CantAfford, "ascending needs a tome")
	}
	entry, err := p.gearHero(request.InstanceID)
	if err != nil {
		return BattleResult{}, err
	}
	gear.setInt("tomes", int64(tomes-1))
	entry["ascended"] = true
	hero["ascended"] = true
	return BattleResult{}, nil
}

type RuneRequest struct {
	Squad int        `json:"squad"`
	Pairs [][2]int64 `json:"pairs"`
}

func (p *player) UnlockRunePrestige(request RuneRequest) (BattleResult, error) {
	if request.Squad < 0 || request.Squad >= PlayerMaxSquads {
		return BattleResult{}, refuse(Unavailable, "no squad %d", request.Squad)
	}
	runes, err := p.doc.section("runes")
	if err != nil {
		return BattleResult{}, refuse(BadProfile, "%v", err)
	}
	squads, _ := runes.array("squads")
	for len(squads) < PlayerMaxSquads {
		squads = append(squads, map[string]any{"prestige_unlocked": false, "slots": zeros(runeSlotsPerSquad)})
	}
	runes["squads"] = squads
	squad, ok := squads[request.Squad].(map[string]any)
	if !ok {
		return BattleResult{}, refuse(BadProfile, "rune squad %d is not an object", request.Squad)
	}
	if unlocked, _ := squad["prestige_unlocked"].(bool); unlocked {
		return BattleResult{}, refuse(AlreadyFull, "squad %d already has the prestige rune slot", request.Squad)
	}
	if err := p.account.spendGems(RunePrestigeGems); err != nil {
		return BattleResult{}, err
	}
	squad["prestige_unlocked"] = true
	return BattleResult{}, nil
}

func (p *player) CraftRunes(request RuneRequest) (BattleResult, error) {
	if len(request.Pairs) == 0 || len(request.Pairs) > RuneInventoryCapacity/2 {
		return BattleResult{}, refuse(Unavailable, "craft needs 1-%d pairs", RuneInventoryCapacity/2)
	}
	writer, err := newRuneWriter(p.doc, p.now)
	if err != nil {
		return BattleResult{}, err
	}
	find := func(uuid int64) int {
		return slices.IndexFunc(writer.items, func(entry any) bool {
			object, ok := entry.(map[string]any)
			id, err := numberValue(object["uuid"], "uuid")
			return ok && err == nil && id == uuid
		})
	}
	for _, pair := range request.Pairs {
		first, second := find(pair[0]), find(pair[1])
		if first < 0 || second < 0 || first == second {
			return BattleResult{}, refuse(Unavailable, "runes %d and %d are not both owned", pair[0], pair[1])
		}
		a, b := writer.items[first].(map[string]any), writer.items[second].(map[string]any)
		def, _ := a["def"].(string)
		rune, known := p.catalog.RuneByID(def)
		if !known || a["def"] != b["def"] || rune.CraftTo == "" || !p.runeIdle(a) || !p.runeIdle(b) {
			return BattleResult{}, refuse(Unavailable, "runes %d and %d cannot be crafted", pair[0], pair[1])
		}
		writer.items = slices.Delete(writer.items, max(first, second), max(first, second)+1)
		writer.items = slices.Delete(writer.items, min(first, second), min(first, second)+1)
		writer.grant(rune.CraftTo)
		p.granted.Runes = append(p.granted.Runes, rune.CraftTo)
	}
	writer.store()
	result := BattleResult{}
	result.Granted = p.granted
	return result, nil
}

func (p *player) runeIdle(rune map[string]any) bool {
	expiry, err := numberValue(rune["expire_at"], "expire_at")
	inventory, inventoryErr := numberValue(rune["inventory_expire_at"], "inventory_expire_at")
	return err == nil && inventoryErr == nil && expiry == 0 && inventory >= p.now.Unix()
}

type TutorialRequest struct {
	Reward string `json:"reward"`
}

func (p *player) ClaimTutorialReward(request TutorialRequest) (BattleResult, error) {
	if slices.Contains(p.server.TutorialRewards, request.Reward) {
		return BattleResult{}, refuse(Unavailable, "tutorial reward %s was already given", request.Reward)
	}
	rewards := p.catalog.Tutorial
	result := BattleResult{}
	switch request.Reward {
	case TutorialGiant:
		if err := p.grantHeroes([]string{rewards.Giant}); err != nil {
			return BattleResult{}, err
		}
	case TutorialGemPack:
		result.Gems = int64(rewards.GemPackGems)
		p.addGems(result.Gems)
	case TutorialPartingGft:
		if err := p.grantHeroes(rewards.GiftHeroes); err != nil {
			return BattleResult{}, err
		}
		if err := p.grantRunes(rewards.GiftRunes); err != nil {
			return BattleResult{}, err
		}
	default:
		return BattleResult{}, refuse(Unavailable, "no tutorial reward %q", request.Reward)
	}
	p.server.TutorialRewards = append(p.server.TutorialRewards, request.Reward)
	result.Granted = p.granted
	return result, nil
}
