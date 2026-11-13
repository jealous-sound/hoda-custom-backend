package economy

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
)

const (
	SquadChanged    RefusalCode = "squad_changed"
	OpponentChanged RefusalCode = "opponent_changed"
	OutcomeMismatch RefusalCode = "outcome_mismatch"

	TutorialVictory      = "victory"
	TutorialDefeat       = "defeat"
	tutorialSeedAttempts = 1024
	tutorialCampaignMaps = 2
	gearSlotsPerHero     = 5
)

type SeedSource func() (uint32, error)

func RandomSeeds() (uint32, error) {
	for {
		var bytes [4]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return 0, fmt.Errorf("battle seed: %w", err)
		}
		if seed := binary.LittleEndian.Uint32(bytes[:]); seed != 0 {
			return seed, nil
		}
	}
}

type storedHero struct {
	ID         string                `json:"id"`
	InstanceID int64                 `json:"instance_id"`
	Tier       string                `json:"tier"`
	Level      int                   `json:"level"`
	ConsumeXP  int                   `json:"consume_xp"`
	Ascended   bool                  `json:"ascended"`
	Fortify    []battle.FortifyChild `json:"fortify"`
}

type storedGear struct {
	InstanceID int64    `json:"instance_id"`
	Ascended   bool     `json:"ascended"`
	Slots      []string `json:"slots"`
}

type storedBattleProfile struct {
	Roster struct {
		Starters  []storedHero `json:"starters"`
		Additions []storedHero `json:"additions"`
	} `json:"roster"`
	Gear struct {
		Heroes []storedGear `json:"heroes"`
	} `json:"gear"`
	Runes struct {
		Items []struct {
			UUID     int64  `json:"uuid"`
			Def      string `json:"def"`
			ExpireAt int64  `json:"expire_at"`
		} `json:"items"`
	} `json:"runes"`
}

func (p *player) storedBattleProfile() (*storedBattleProfile, error) {
	raw, err := json.Marshal(p.doc)
	if err != nil {
		return nil, err
	}
	var stored storedBattleProfile
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	return &stored, nil
}

func bySlot(units []battle.Unit) []battle.Unit {
	sorted := append([]battle.Unit{}, units...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Slot < sorted[j].Slot })
	return sorted
}

func canonicalSide(side battle.Side) battle.Side {
	canonical := battle.Side{
		Units: bySlot(side.Units), Runes: append([]string{}, side.Runes...), HeroBuilds: side.HeroBuilds,
	}
	for i := range canonical.Units {
		unit := &canonical.Units[i]
		if unit.Gear != nil {
			unit.Gear = append([]string{}, unit.Gear...)
		}
		if unit.Fortify != nil {
			unit.Fortify = append([]battle.FortifyChild{}, unit.Fortify...)
			sort.SliceStable(unit.Fortify, func(a, b int) bool { return unit.Fortify[a].Slot < unit.Fortify[b].Slot })
		}
	}
	return canonical
}

func canonicalChanger(changer *battle.Changer) *battle.Changer {
	if changer == nil || len(changer.Effects) == 0 {
		return nil
	}
	return changer
}

func sameJSON(a, b any) bool {
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	return leftErr == nil && rightErr == nil && string(left) == string(right)
}

func wornOnce(worn []string) []string {
	slots := make([]string, gearSlotsPerHero)
	counted := map[string]bool{}
	for slot, id := range worn {
		if slot == len(slots) {
			break
		}
		if id != "" && !counted[id] {
			slots[slot] = id
			counted[id] = true
		}
	}
	return slots
}

func (p *player) battleSquad(claimed battle.Side, runeIDs []int64) (battle.Side, error) {
	if len(claimed.Units) == 0 || len(claimed.Units) > battle.SlotCount {
		return battle.Side{}, refuse(Unavailable, "a squad needs 1-%d heroes", battle.SlotCount)
	}
	if len(runeIDs) > battle.RuneSlotCount {
		return battle.Side{}, refuse(Unavailable, "a squad has %d rune slots", battle.RuneSlotCount)
	}
	stored, err := p.storedBattleProfile()
	if err != nil {
		return battle.Side{}, err
	}
	heroes := map[int64]storedHero{}
	for _, list := range [][]storedHero{stored.Roster.Starters, stored.Roster.Additions} {
		for _, hero := range list {
			heroes[hero.InstanceID] = hero
		}
	}
	gear := map[int64]storedGear{}
	for _, entry := range stored.Gear.Heroes {
		gear[entry.InstanceID] = entry
	}
	squad := battle.Side{Units: []battle.Unit{}, Runes: []string{}}
	seen := map[int64]bool{}
	for _, unit := range claimed.Units {
		if err := refuseAway(p.doc, unit.InstanceID); err != nil {
			return battle.Side{}, err
		}
		hero, ok := heroes[unit.InstanceID]
		if !ok || unit.InstanceID <= 0 || seen[unit.InstanceID] {
			return battle.Side{}, refuse(SquadChanged, "hero %d is not in the roster", unit.InstanceID)
		}
		seen[unit.InstanceID] = true
		loadout := gear[unit.InstanceID]
		slots := wornOnce(loadout.Slots)
		squad.Units = append(squad.Units, battle.Unit{
			Slot: unit.Slot, InstanceID: hero.InstanceID, ID: hero.ID, Tier: hero.Tier, Level: hero.Level,
			ConsumeXP: hero.ConsumeXP, Ascended: hero.Ascended || loadout.Ascended, Gear: slots,
			Fortify: hero.Fortify,
		})
	}
	used := map[int64]bool{}
	for _, uuid := range runeIDs {
		found := false
		for _, item := range stored.Runes.Items {
			if item.UUID != uuid || used[uuid] || item.ExpireAt == 0 || item.ExpireAt < p.now.Unix() ||
				!p.catalog.Battle.RuneKnown(item.Def) {
				continue
			}
			used[uuid] = true
			squad.Runes = append(squad.Runes, item.Def)
			found = true
			break
		}
		if !found {
			return battle.Side{}, refuse(SquadChanged, "rune %d is not active", uuid)
		}
	}
	squad = canonicalSide(squad)
	if !sameJSON(squad, canonicalSide(claimed)) {
		return battle.Side{}, refuse(SquadChanged, "the squad differs from the server's copy")
	}
	return squad, nil
}

type PvpOpponentRequest struct {
	Kind string `json:"kind"`
	Seed uint32 `json:"seed"`
}

const OpponentRevenge = "revenge"

func (s *pvpStats) lostDefenseAt(at int64, seed uint32) *pvpBattle {
	for i := range s.History {
		entry := &s.History[i]
		if entry.Defense && !entry.Won && !entry.Revenged && entry.At == at && entry.Seed == nonZeroSeed(seed) {
			return entry
		}
	}
	return nil
}

func rivalOf(opponent battle.PvpOpponent) pvpOpponent {
	return pvpOpponent{
		bot: opponent.Bot, squad: opponent.Squad, level: opponent.Level, trophies: opponent.Trophies,
		seed: opponent.Seed,
	}
}

func (p *player) pvpRival(request BattleStartRequest, stats *pvpStats) (
	pvpOpponent, battle.Side, *battle.Changer, error,
) {
	exclusive := 0
	for _, chosen := range []bool{request.Event, request.Duel != "", request.RevengeAt != 0} {
		if chosen {
			exclusive++
		}
	}
	if exclusive > 1 {
		return pvpOpponent{}, battle.Side{}, nil, refuse(Unavailable, "a battle is one of an event battle, a duel or a revenge")
	}
	data := p.catalog.Battle
	o := request.Opponent
	switch {
	case request.Duel != "":
		match, err := p.duels.DuelOpponent(request.Duel)
		if err != nil {
			return pvpOpponent{}, battle.Side{}, nil, err
		}
		enemy, err := p.duelEnemy(request.Duel)
		if err != nil {
			return pvpOpponent{}, battle.Side{}, nil, err
		}
		rival := pvpOpponent{
			level: min(max(match.Level, 1), pvpMaxLevel), trophies: max(match.Trophies, 0),
			seed: nonZeroSeed(o.Seed), name: match.Name,
		}
		return rival, enemy, nil, nil
	case request.RevengeAt != 0:
		target := stats.lostDefenseAt(request.RevengeAt, o.Seed)
		if target == nil {
			return pvpOpponent{}, battle.Side{}, nil, refuse(Unavailable, "no lost defense at %d to revenge",
				request.RevengeAt)
		}
		target.Revenged = true
		opponent := battle.PvpOpponent{
			Bot: target.Bot, Squad: target.Squad, Level: min(max(target.Level, 1), pvpMaxLevel),
			Trophies: target.OpponentTrophies, Seed: target.Seed,
		}
		enemy, err := data.PvpRoster(opponent)
		if err != nil {
			return pvpOpponent{}, battle.Side{}, nil, refuse(Unavailable, "%v", err)
		}
		return rivalOf(opponent), enemy, nil, nil
	}
	return p.matchedRival(request)
}

func (p *player) duelEnemy(member string) (battle.Side, error) {
	duel, err := p.duels.DuelSquad(member)
	if err != nil {
		return battle.Side{}, err
	}
	data := p.catalog.Battle
	enemy := battle.Side{Units: []battle.Unit{}, Runes: []string{}, HeroBuilds: duel.HeroBuilds}
	for _, unit := range duel.Units {
		if unit.Slot < 0 || unit.Slot >= battle.SlotCount || unit.ID == "" || !data.Knows(unit.ID) {
			continue
		}
		fielded := battle.Unit{
			Slot: unit.Slot, ID: unit.ID, Tier: battle.ParseTier(unit.Tier).String(), Level: max(unit.Level, 1),
		}
		if duel.HeroBuilds {
			fielded.ConsumeXP, fielded.Ascended = unit.ConsumeXP, unit.Ascended
			fielded.Gear, fielded.Fortify = unit.Gear, unit.Fortify
		}
		enemy.Units = append(enemy.Units, fielded)
	}
	for _, id := range duel.Runes {
		if data.RuneKnown(id) && len(enemy.Runes) < battle.RuneSlotCount {
			enemy.Runes = append(enemy.Runes, id)
		}
	}
	if len(enemy.Units) == 0 {
		return battle.Side{}, refuse(Unavailable, "%s has no squad to duel", member)
	}
	return canonicalSide(enemy), nil
}

func (p *player) checkEnemy(request BattleStartRequest, enemy battle.Side, changer *battle.Changer) error {
	if !sameJSON(canonicalSide(enemy), canonicalSide(request.Enemy)) ||
		!sameJSON(canonicalChanger(changer), canonicalChanger(request.Changer)) {
		return refuse(OpponentChanged, "the opponent differs from the server's")
	}
	return nil
}

func (p *player) battleSeed(request BattleStartRequest, replay battle.Replay) (uint32, error) {
	seed, err := p.seeds()
	if err != nil {
		return 0, err
	}
	if request.TutorialOutcome == "" {
		return seed, nil
	}
	finished, err := tutorialFinished(p.doc)
	if err != nil {
		return 0, err
	}
	victory := request.TutorialOutcome == TutorialVictory
	if finished || request.Mode != BattleCampaign || request.Map >= tutorialCampaignMaps ||
		(!victory && request.TutorialOutcome != TutorialDefeat) {
		return 0, refuse(Unavailable, "no scripted tutorial battle here")
	}
	for attempt := 0; attempt < tutorialSeedAttempts; attempt, seed = attempt+1, seed+1 {
		replay.Seed = seed
		outcome, err := p.catalog.Battle.Run(replay)
		if err != nil {
			return 0, refuse(Unavailable, "%v", err)
		}
		if outcome.Won == victory {
			return seed, nil
		}
	}
	return seed, nil
}

func campaignEnemy(c *catalog.Catalog, m catalog.CampaignMap, node int) (battle.Side, error) {
	enemy, ok := c.Battle.CampaignRoster(m.ID, node+1)
	if !ok {
		return battle.Side{}, refuse(Unavailable, "no roster for %s node %d", m.ID, node)
	}
	return enemy, nil
}

func nexusEnemy(c *catalog.Catalog, event *catalog.NexusEvent, difficulty *catalog.NexusDifficulty, step int) (
	battle.Side, *battle.Changer, error,
) {
	enemy, ok := c.Battle.NexusRoster(difficulty.ID, step)
	if !ok {
		return battle.Side{}, nil, refuse(Unavailable, "no roster for %s step %d", difficulty.ID, step)
	}
	return enemy, c.Battle.NexusChanger(event.ID), nil
}
