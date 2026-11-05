package economy

import (
	"encoding/json"
	"slices"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
)

const runInitialStep = 1

var starterRunes = []string{"rune-power-1", "rune-health-1", "rune-knowledge-1", "rune-greed-1"}

func zeros(count int) []any {
	return numberArray(make([]int64, count))
}

func zeroGrid(rows, columns int) []any {
	grid := make([]any, rows)
	for i := range grid {
		grid[i] = zeros(columns)
	}
	return grid
}

func newRunNodes() []any {
	nodes := make([]any, catalog.NexusMaxNodes)
	for i := range nodes {
		nodes[i] = map[string]any{"wins": json.Number("0"), "losses": json.Number("0"), "previous_wins": json.Number("0")}
	}
	return nodes
}

func newPlayerEconomy(c *catalog.Catalog, upload document) document {
	account := document{}
	account.setInt("level", 1)
	account.setInt("experience", 0)
	account.setInt("experience_min", xpForCurrentLevel(1))
	account.setInt("experience_max", xpForNextLevel(1))
	account.setInt("energy", StartEnergyMax)
	account.setInt("energy_max", StartEnergyMax)
	account.setInt("stamina", StartStaminaMax)
	account.setInt("stamina_max", StartStaminaMax)
	account.setFloat(energyRegenTimer, 0)
	account.setFloat(staminaRegenTimer, 0)
	account.setInt("gold", StartGold)
	account.setInt("gems", StartGems)

	store := document{
		"opened_by_pack": zeros(catalog.StoreCounterCount),
		"vouchers":       zeros(catalog.VoucherCapacity),
	}
	store.setInt("opened_total", 0)
	store.setInt("champion_since_legendary", 0)

	run := document{
		"committed": false, "reset": false, "escrow_units": zeros(int(catalog.RarityMythic) + 1),
		"nodes": newRunNodes(),
	}
	run.setInt("current_node_step", runInitialStep)
	for _, field := range []string{"escrow_gold", "escrow_xp", "escrow_potions"} {
		run.setInt(field, 0)
	}
	nexus := document{
		"keys": zeros(catalog.NexusKeyCount),
		"run":  map[string]any(run),
		"events": map[string]any{
			"completions":    zeroGrid(catalog.NexusMaxEvents, catalog.NexusMaxDifficulties),
			"unlocked_at":    zeros(catalog.NexusMaxEvents),
			"remaining_runs": zeros(catalog.NexusMaxEvents),
		},
	}
	nexus.setInt("potions", StartPotions)
	nexus.setInt("potion_max", StartPotionMax)

	return document{
		"account": map[string]any(account),
		"store":   map[string]any(store),
		"nexus":   map[string]any(nexus),
		"campaign": map[string]any{
			"completions": zeroGrid(catalog.CampaignMaxMaps, catalog.CampaignMaxNodes),
			"node_losses": zeros(catalog.CampaignMaxMaps * catalog.CampaignMaxNodes),
		},
		"missions": map[string]any{"claimed": []any{}},
		"roster":   map[string]any(starterRoster(c, upload)),
		"gear":     map[string]any{"inventory": map[string]any{}, "heroes": []any{}, "tomes": json.Number("0")},
		"runes":    map[string]any(starterRuneSet(upload)),
	}
}

func starterRoster(c *catalog.Catalog, upload document) document {
	allowed := slices.Clone(c.Tutorial.StarterHeroes)
	roster, _ := upload.optionalSection("roster")
	kept := []any{}
	next := int64(firstInstanceID)
	for _, list := range []string{"starters", "additions"} {
		entries, _ := roster.array(list)
		for _, entry := range entries {
			object, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			id, err := numberValue(object["instance_id"], "instance_id")
			name, _ := object["id"].(string)
			at := slices.Index(allowed, name)
			if err != nil || id < firstInstanceID || at < 0 {
				continue
			}
			allowed = slices.Delete(allowed, at, at+1)
			kept = append(kept, map[string]any{"id": name, "instance_id": object["instance_id"], "ascended": false})
			next = max(next, id+1)
		}
	}
	result := document{"starters": []any{}, "additions": kept}
	result.setInt("next_instance_id", next)
	return result
}

func starterRuneSet(upload document) document {
	allowed := slices.Clone(starterRunes)
	runes, _ := upload.optionalSection("runes")
	items, _ := runes.array("items")
	kept := []any{}
	next := int64(firstRuneUUID)
	for _, entry := range items {
		object, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		uuid, err := numberValue(object["uuid"], "uuid")
		def, _ := object["def"].(string)
		at := slices.Index(allowed, def)
		if err != nil || uuid < firstRuneUUID || at < 0 {
			continue
		}
		allowed = slices.Delete(allowed, at, at+1)
		kept = append(kept, map[string]any{
			"uuid": object["uuid"], "def": def, "expire_at": json.Number("0"),
			"inventory_expire_at": object["inventory_expire_at"],
		})
		next = max(next, uuid+1)
	}
	result := document{"items": kept, "squads": []any{}}
	result.setInt("next_uuid", next)
	return result
}
