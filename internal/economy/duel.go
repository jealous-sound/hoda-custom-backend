package economy

import (
	"encoding/json"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
)

const (
	squadCells   = 5
	defenceSquad = 0
)

type DuelUnit struct {
	Slot      int                   `json:"slot"`
	ID        string                `json:"id"`
	Tier      string                `json:"tier"`
	Level     int                   `json:"level"`
	ConsumeXP int                   `json:"consume_xp"`
	Ascended  bool                  `json:"ascended"`
	Gear      []string              `json:"gear"`
	Fortify   []battle.FortifyChild `json:"fortify"`
}

type DuelOpponent struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Level      int                `json:"level"`
	Trophies   int                `json:"trophies"`
	RenownRank int                `json:"renown_rank,omitempty"`
	VaultTier  int                `json:"vault_tier,omitempty"`
	Units      []DuelUnit         `json:"units"`
	Runes      []string           `json:"runes"`
	HeroBuilds bool               `json:"hero_builds"`
	Renown     *battle.SideRenown `json:"renown,omitempty"`
}

type duelProfile struct {
	Account struct {
		Name       string `json:"name"`
		Level      int    `json:"level"`
		Experience int64  `json:"experience"`
	} `json:"account"`
	Squads [][]*int64 `json:"squads"`
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
		Squads []struct {
			Slots []int64 `json:"slots"`
		} `json:"squads"`
	} `json:"runes"`
	Pvp struct {
		Trophies int `json:"trophies"`
	} `json:"pvp"`
	Renown json.RawMessage `json:"renown"`
	Vault  lockedVault     `json:"vault"`
}

func (d *duelProfile) sideRenown(rules *catalog.RenownRules) *battle.SideRenown {
	section, _ := parseDocument(d.Renown)
	return readRenown(rules, section).withinRank(d.Account.Experience).sideBonus()
}

func DuelOpponentFrom(c *catalog.Catalog, playerID string, raw json.RawMessage, now time.Time) (
	DuelOpponent, error,
) {
	var stored duelProfile
	if err := json.Unmarshal(raw, &stored); err != nil {
		return DuelOpponent{}, refuse(BadProfile, "%v", err)
	}
	opponent := DuelOpponent{
		ID: playerID, Name: stored.Account.Name, Level: max(stored.Account.Level, 1), Trophies: stored.Pvp.Trophies,
		RenownRank: RenownRank(&c.Renown, stored.Account.Experience), VaultTier: VaultTier(c, stored.Vault.Locked),
		Units: []DuelUnit{}, Runes: []string{}, HeroBuilds: true, Renown: stored.sideRenown(&c.Renown),
	}
	if opponent.Name == "" {
		opponent.Name = playerID
	}
	squad := defenceSquad
	if squad >= len(stored.Squads) {
		return opponent, nil
	}
	units := map[int64]storedHero{}
	for _, list := range [][]storedHero{stored.Roster.Starters, stored.Roster.Additions} {
		for _, entry := range list {
			units[entry.InstanceID] = entry
		}
	}
	gear := map[int64]storedGear{}
	for _, entry := range stored.Gear.Heroes {
		gear[entry.InstanceID] = entry
	}
	for slot, cell := range stored.Squads[squad] {
		if slot >= squadCells || cell == nil {
			continue
		}
		if unit, found := units[*cell]; found {
			loadout := gear[unit.InstanceID]
			opponent.Units = append(opponent.Units, DuelUnit{
				Slot: slot, ID: unit.ID, Tier: unit.Tier, Level: max(unit.Level, 1),
				ConsumeXP: unit.ConsumeXP, Ascended: unit.Ascended || loadout.Ascended,
				Gear: wornOnce(loadout.Slots), Fortify: append([]battle.FortifyChild{}, unit.Fortify...),
			})
		}
	}
	if squad >= len(stored.Runes.Squads) {
		return opponent, nil
	}
	for i, uuid := range stored.Runes.Squads[squad].Slots {
		if i >= runeSlotsPerSquad || uuid == 0 {
			continue
		}
		for _, item := range stored.Runes.Items {
			if item.UUID == uuid && item.ExpireAt != 0 && item.ExpireAt >= now.Unix() {
				opponent.Runes = append(opponent.Runes, item.Def)
			}
		}
	}
	return opponent, nil
}
