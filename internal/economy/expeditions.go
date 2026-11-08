package economy

import (
	"encoding/json"
	"slices"
	"strconv"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
)

const (
	expeditionsSection = "expeditions"
	expeditionsActive  = "active"
)

type activeExpedition struct {
	ID        string
	Heroes    []int64
	StartedAt int64
	EndsAt    int64
}

func readExpeditions(doc document) []activeExpedition {
	section, _ := doc.optionalSection(expeditionsSection)
	entries, _ := section.array(expeditionsActive)
	var active []activeExpedition
	for _, entry := range entries {
		object, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		id, _ := object["id"].(string)
		heroes, _ := object["heroes"].([]any)
		instances, err := intArray(heroes, "heroes")
		started, startErr := numberValue(object["started_at"], "started_at")
		ends, endErr := numberValue(object["ends_at"], "ends_at")
		if id == "" || err != nil || startErr != nil || endErr != nil {
			continue
		}
		active = append(active, activeExpedition{ID: id, Heroes: instances, StartedAt: started, EndsAt: ends})
	}
	return active
}

func storeExpeditions(doc document, active []activeExpedition) error {
	section, err := doc.section(expeditionsSection)
	if err != nil {
		return refuse(BadProfile, "%v", err)
	}
	entries := make([]any, 0, len(active))
	for _, expedition := range active {
		entries = append(entries, map[string]any{
			"id":         expedition.ID,
			"heroes":     numberArray(expedition.Heroes),
			"started_at": json.Number(strconv.FormatInt(expedition.StartedAt, 10)),
			"ends_at":    json.Number(strconv.FormatInt(expedition.EndsAt, 10)),
		})
	}
	section[expeditionsActive] = entries
	return nil
}

func awayHeroes(doc document) map[int64]bool {
	away := map[int64]bool{}
	for _, expedition := range readExpeditions(doc) {
		for _, instance := range expedition.Heroes {
			away[instance] = true
		}
	}
	return away
}

func refuseAway(doc document, instances ...int64) error {
	away := awayHeroes(doc)
	for _, instance := range instances {
		if away[instance] {
			return refuse(Unavailable, "hero %d is away on an expedition", instance)
		}
	}
	return nil
}

func traitMet(trait catalog.ExpeditionTrait, party []*hero) bool {
	matches := 0
	for _, member := range party {
		if trait.Matches(member.def, member.rarity()) {
			matches++
		}
	}
	return matches >= trait.Count
}

type ExpeditionRequest struct {
	Expedition string  `json:"expedition"`
	Heroes     []int64 `json:"heroes"`
}

func (p *player) expeditionParty(expedition catalog.Expedition, instances []int64) ([]*hero, error) {
	if len(instances) != expedition.Party {
		return nil, refuse(Unavailable, "%s needs %d heroes", expedition.ID, expedition.Party)
	}
	r, err := loadRoster(p.catalog, p.doc)
	if err != nil {
		return nil, err
	}
	away := awayHeroes(p.doc)
	party := make([]*hero, 0, len(instances))
	for index, instance := range instances {
		member := r.find(instance)
		if member == nil || slices.Contains(instances[:index], instance) || away[instance] ||
			inAnySquad(p.doc, instance) {
			return nil, refuse(Unavailable, "hero %d cannot go on an expedition", instance)
		}
		party = append(party, member)
	}
	for _, requirement := range expedition.Requirements {
		if !traitMet(requirement, party) {
			return nil, refuse(Unavailable, "the party does not meet the needs of %s", expedition.ID)
		}
	}
	return party, nil
}

func (p *player) StartExpedition(request ExpeditionRequest) (BattleResult, error) {
	expedition, ok := p.catalog.Expedition(request.Expedition)
	rules := p.catalog.Expeditions
	if !ok || p.account.Level < expedition.Level || rules.Slots(p.account.Level) == 0 {
		return BattleResult{}, refuse(Unavailable, "expedition %q is not open", request.Expedition)
	}
	active := readExpeditions(p.doc)
	if len(active) >= rules.Slots(p.account.Level) {
		return BattleResult{}, refuse(AlreadyFull, "every expedition party is out")
	}
	if slices.ContainsFunc(active, func(a activeExpedition) bool { return a.ID == expedition.ID }) {
		return BattleResult{}, refuse(AlreadyFull, "%s is already under way", expedition.ID)
	}
	if _, err := p.expeditionParty(expedition, request.Heroes); err != nil {
		return BattleResult{}, err
	}
	active = append(active, activeExpedition{
		ID: expedition.ID, Heroes: slices.Clone(request.Heroes), StartedAt: p.now.Unix(),
		EndsAt: p.now.Add(expedition.Duration).Unix(),
	})
	if err := p.countQuests(questCount{catalog.QuestStatExpeditionsSent, 1}); err != nil {
		return BattleResult{}, err
	}
	return BattleResult{}, storeExpeditions(p.doc, active)
}

func (p *player) RecallExpedition(request ExpeditionRequest) (BattleResult, error) {
	active := readExpeditions(p.doc)
	index := slices.IndexFunc(active, func(a activeExpedition) bool { return a.ID == request.Expedition })
	if index < 0 {
		return BattleResult{}, refuse(Unavailable, "no %q under way", request.Expedition)
	}
	return BattleResult{}, storeExpeditions(p.doc, slices.Delete(active, index, index+1))
}

type ExpeditionClaimRequest struct {
	Expeditions []string `json:"expeditions"`
}

func (p *player) rollBetween(span [2]int64) int64 {
	if span[1] <= span[0] {
		return span[0]
	}
	return span[0] + int64(p.rng.below(int(span[1]-span[0]+1)))
}

func (p *player) rollHeroRarity(weights [catalog.RarityLegendary + 1]int) catalog.Rarity {
	total := 0
	for _, weight := range weights {
		total += weight
	}
	roll := p.rng.below(total)
	for rarity := catalog.RarityCommon; rarity <= catalog.RarityLegendary; rarity++ {
		if roll < weights[rarity] {
			return rarity
		}
		roll -= weights[rarity]
	}
	return catalog.RarityLegendary
}

func (p *player) rollExpeditionDrop(drop catalog.ExpeditionDrop) error {
	switch drop.Kind {
	case catalog.ExpeditionDropGear:
		if index := rollGear(p.catalog, drop.MinRarity, drop.MaxRarity, &p.rng); index != noRoll {
			return p.grantGear(p.catalog.Gear[index].ID, 1)
		}
	case catalog.ExpeditionDropRune:
		slot := catalog.RuneSlot{MinRarity: drop.MinRarity, MaxRarity: drop.MaxRarity}
		if index := rollRune(p.catalog, slot, &p.rng); index != noRoll {
			return p.grantRunes([]string{p.catalog.Runes[index].ID})
		}
	case catalog.ExpeditionDropHero:
		pool := p.catalog.RarityPool(p.rollHeroRarity(drop.HeroWeights))
		if len(pool) > 0 {
			return p.grantHeroes([]string{p.catalog.Heroes[pool[p.rng.below(len(pool))]].ID})
		}
	}
	return nil
}

func (p *player) payExpedition(expedition catalog.Expedition, claimed activeExpedition, result *BattleResult) error {
	r, err := loadRoster(p.catalog, p.doc)
	if err != nil {
		return err
	}
	var party []*hero
	for _, instance := range claimed.Heroes {
		if member := r.find(instance); member != nil {
			party = append(party, member)
		}
	}
	great := traitMet(expedition.Bonus, party)
	boost := func(amount int64) int64 {
		if great {
			return p.catalog.Expeditions.GreatSuccess(amount)
		}
		return amount
	}
	gold := boost(p.rollBetween(expedition.Gold))
	gems := boost(p.rollBetween(expedition.Gems))
	heroXP := boost(expedition.HeroXP)
	p.addGold(gold)
	p.addGems(gems)
	for _, member := range party {
		member.awardXP(heroXP)
		member.store()
	}
	result.Gold = saturatingAdd(result.Gold, gold)
	result.Gems = saturatingAdd(result.Gems, gems)
	result.XP = max(result.XP, heroXP)
	for _, drop := range expedition.Drops {
		chance := drop.Chance
		if great {
			chance = percent
		}
		if p.rng.below(percent) < chance {
			if err := p.rollExpeditionDrop(drop); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *player) ClaimExpeditions(request ExpeditionClaimRequest) (BattleResult, error) {
	if len(request.Expeditions) == 0 || len(request.Expeditions) > catalog.ExpeditionSlotCapacity {
		return BattleResult{}, refuse(Unavailable, "claim 1-%d expeditions", catalog.ExpeditionSlotCapacity)
	}
	active := readExpeditions(p.doc)
	claimed := make([]activeExpedition, 0, len(request.Expeditions))
	heroRoom := 0
	for index, id := range request.Expeditions {
		at := slices.IndexFunc(active, func(a activeExpedition) bool { return a.ID == id })
		expedition, known := p.catalog.Expedition(id)
		if at < 0 || !known || slices.Contains(request.Expeditions[:index], id) || p.now.Unix() < active[at].EndsAt {
			return BattleResult{}, refuse(Unavailable, "%q is not back yet", id)
		}
		claimed = append(claimed, active[at])
		heroRoom += expedition.HeroDropLimit()
	}
	if heroRoom > p.rosterRoom() {
		return BattleResult{}, refuse(InventoryFull, "roster has no room for %d heroes", heroRoom)
	}
	result := BattleResult{}
	for _, entry := range claimed {
		expedition, _ := p.catalog.Expedition(entry.ID)
		if err := p.payExpedition(expedition, entry, &result); err != nil {
			return BattleResult{}, err
		}
	}
	remaining := slices.DeleteFunc(active, func(a activeExpedition) bool {
		return slices.Contains(request.Expeditions, a.ID)
	})
	if err := storeExpeditions(p.doc, remaining); err != nil {
		return BattleResult{}, err
	}
	if err := p.countQuests(questCount{catalog.QuestStatExpeditionsReturned, int64(len(claimed))}); err != nil {
		return BattleResult{}, err
	}
	result.Granted = p.granted
	return result, nil
}
