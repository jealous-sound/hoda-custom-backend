package economy

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const runeActivationSlack = time.Minute

var serverOwnedSections = map[string][]string{
	"account": {
		"level", "experience", "experience_min", "experience_max", "energy", "energy_max", "stamina", "stamina_max",
		energyRegenTimer, staminaRegenTimer, "gold", "gems",
	},
	"store":    {"opened_total", "opened_by_pack", "champion_since_legendary", "vouchers"},
	"nexus":    {"potions", "potion_max", "keys", "events"},
	"campaign": {"completions", "node_losses"},
	"missions": {"claimed", "counters"},
}

var serverOwnedHeroFields = []string{"tier", "level", "xp", "consume_xp", "ascended", fortifyField}

var serverOwnedRunFields = []string{
	"committed", "reset", "current_node_step", "escrow_gold", "escrow_xp", "escrow_potions", "escrow_units", "nodes",
}

var runSelectionFields = []string{"event_index", "difficulty_index", "map_id"}

func MergeUpload(c *catalog.Catalog, current profile.Record, server *profile.ServerState, upload json.RawMessage,
	now time.Time,
) (json.RawMessage, error) {
	next, err := parseDocument(upload)
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	stored := document{}
	if len(current.Profile) > 0 {
		if stored, err = parseDocument(current.Profile); err != nil {
			return nil, err
		}
	} else {
		stored = newPlayerEconomy(c, next)
		server.RegenAt = now.Unix()
	}
	if err := regenerate(stored, regenSince(server), now); err != nil {
		return nil, err
	}
	server.RegenAt = now.Unix()
	for name, fields := range serverOwnedSections {
		keepFields(next, stored, name, fields)
	}
	keepNexusRun(next, stored)
	restored := keepRoster(c, next, stored)
	keepGear(c, next, stored, restored)
	if activations := keepRunes(c, next, stored, now); activations > 0 {
		if err := addMissionCounter(next, statRunesUsed, activations); err != nil {
			return nil, err
		}
	}
	if err := keepPvp(next, stored); err != nil {
		return nil, err
	}
	if err := syncMissions(c, next, server, now); err != nil {
		return nil, err
	}
	return next.encode()
}

func keepFields(next, stored document, section string, fields []string) {
	from, _ := stored.optionalSection(section)
	to, err := next.section(section)
	if err != nil {
		to = document{}
		next[section] = map[string]any(to)
	}
	for _, field := range fields {
		if value, ok := from[field]; ok {
			to[field] = value
		} else {
			delete(to, field)
		}
	}
}

func keepNexusRun(next, stored document) {
	storedNexus, _ := stored.optionalSection("nexus")
	storedRun, _ := storedNexus.optionalSection("run")
	nexus, err := next.section("nexus")
	if err != nil {
		return
	}
	run, err := nexus.section("run")
	if err != nil {
		run = document{}
		nexus["run"] = map[string]any(run)
	}
	fields := serverOwnedRunFields
	if storedRun.boolField("committed") {
		fields = append(append([]string(nil), fields...), runSelectionFields...)
	}
	for _, field := range fields {
		if value, ok := storedRun[field]; ok {
			run[field] = value
		} else {
			delete(run, field)
		}
	}
}

type storedRoster struct {
	heroes map[int64]map[string]any
	order  []int64
}

func pinnedHero(c *catalog.Catalog, entry map[string]any) map[string]any {
	pinned := map[string]any{}
	for name, value := range entry {
		pinned[name] = value
	}
	if h, known := readHero(c, pinned); known {
		h.store()
	}
	return pinned
}

func storedHeroes(c *catalog.Catalog, stored document) storedRoster {
	owned := storedRoster{heroes: map[int64]map[string]any{}}
	roster, _ := stored.optionalSection("roster")
	for _, list := range rosterLists {
		entries, _ := roster.array(list)
		for _, entry := range entries {
			object, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			id, err := numberValue(object["instance_id"], "instance_id")
			if err != nil || owned.heroes[id] != nil {
				continue
			}
			owned.heroes[id] = pinnedHero(c, object)
			owned.order = append(owned.order, id)
		}
	}
	return owned
}

func keepRoster(c *catalog.Catalog, next, stored document) map[int64]bool {
	owned := storedHeroes(c, stored)
	storedSection, _ := stored.optionalSection("roster")
	roster, err := next.section("roster")
	if err != nil {
		roster = document{}
		next["roster"] = map[string]any(roster)
	}
	seen := map[int64]bool{}
	for _, list := range rosterLists {
		entries, _ := roster.array(list)
		kept := []any{}
		for _, entry := range entries {
			object, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			id, err := numberValue(object["instance_id"], "instance_id")
			hero, known := owned.heroes[id]
			if err != nil || !known || seen[id] || object["id"] != hero["id"] {
				continue
			}
			seen[id] = true
			for _, field := range serverOwnedHeroFields {
				if value, stored := hero[field]; stored {
					object[field] = value
				} else {
					delete(object, field)
				}
			}
			kept = append(kept, object)
		}
		roster[list] = kept
	}
	restored := map[int64]bool{}
	for _, id := range owned.order {
		if !seen[id] {
			roster["additions"] = append(roster["additions"].([]any), owned.heroes[id])
			restored[id] = true
		}
	}
	if value, ok := storedSection["next_instance_id"]; ok {
		roster["next_instance_id"] = value
	}
	roster.setInt("starter_count", int64(len(roster["starters"].([]any))))
	return restored
}

func rosterInstances(c *catalog.Catalog, doc document) map[int64]bool {
	instances := map[int64]bool{}
	for _, id := range storedHeroes(c, doc).order {
		instances[id] = true
	}
	return instances
}

type gearHero struct {
	ascended, prestige bool
}

func gearTotals(gear document, instances map[int64]bool) map[string]int64 {
	totals := map[string]int64{}
	inventory, _ := gear.optionalSection("inventory")
	for id, value := range inventory {
		if count, err := numberValue(value, id); err == nil && count > 0 {
			totals[id] += count
		}
	}
	heroes, _ := gear.array("heroes")
	for _, entry := range heroes {
		object, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if instances != nil {
			if id, err := numberValue(object["instance_id"], "instance_id"); err != nil || !instances[id] {
				continue
			}
		}
		slots, _ := object["slots"].([]any)
		for _, slot := range slots {
			if id, ok := slot.(string); ok && id != "" {
				totals[id]++
			}
		}
	}
	return totals
}

func gearHeroFlags(gear document) map[int64]gearHero {
	flags := map[int64]gearHero{}
	heroes, _ := gear.array("heroes")
	for _, entry := range heroes {
		object, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		id, err := numberValue(object["instance_id"], "instance_id")
		if err != nil {
			continue
		}
		ascended, _ := object["ascended"].(bool)
		prestige, _ := object["prestige_unlocked"].(bool)
		flags[id] = gearHero{ascended: ascended, prestige: prestige}
	}
	return flags
}

func restoreGearOf(gear, storedGear document, restored map[int64]bool) {
	heroes, _ := gear.array("heroes")
	present := map[int64]bool{}
	for _, entry := range heroes {
		object, _ := entry.(map[string]any)
		if id, err := numberValue(object["instance_id"], "instance_id"); err == nil {
			present[id] = true
		}
	}
	storedEntries, _ := storedGear.array("heroes")
	for _, entry := range storedEntries {
		object, _ := entry.(map[string]any)
		id, err := numberValue(object["instance_id"], "instance_id")
		if err == nil && restored[id] && !present[id] {
			heroes = append(heroes, object)
			present[id] = true
		}
	}
	gear["heroes"] = heroes
}

func keepGear(c *catalog.Catalog, next, stored document, restored map[int64]bool) {
	storedGear, _ := stored.optionalSection("gear")
	gear, err := next.section("gear")
	if err != nil {
		gear = document{}
		next["gear"] = map[string]any(gear)
	}
	if len(restored) > 0 {
		restoreGearOf(gear, storedGear, restored)
	}
	instances := rosterInstances(c, next)
	allowed := gearTotals(storedGear, nil)
	uploaded := gearTotals(gear, instances)
	forged := false
	for id, total := range uploaded {
		if _, known := c.GearIndex(id); !known || total > allowed[id] {
			copied := document{}
			for key, value := range storedGear {
				copied[key] = value
			}
			next["gear"] = map[string]any(copied)
			gear = copied
			forged = true
			break
		}
	}
	if !forged {
		returnUnsoldGear(gear, gearTotals(storedGear, instances), uploaded)
	}
	if value, ok := storedGear["tomes"]; ok {
		gear["tomes"] = value
	} else {
		gear.setInt("tomes", 0)
	}
	flags := gearHeroFlags(storedGear)
	heroes, _ := gear.array("heroes")
	for _, entry := range heroes {
		object, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		id, _ := numberValue(object["instance_id"], "instance_id")
		object["ascended"] = flags[id].ascended
		object["prestige_unlocked"] = flags[id].prestige
	}
}

func returnUnsoldGear(gear document, kept, uploaded map[string]int64) {
	inventory, err := gear.section("inventory")
	if err != nil {
		inventory = document{}
		gear["inventory"] = map[string]any(inventory)
	}
	for id, total := range kept {
		if missing := total - uploaded[id]; missing > 0 {
			count, _ := inventory.optionalInt(id)
			inventory.setInt(id, int64(count)+missing)
		}
	}
}

type ownedRune struct {
	entry               map[string]any
	idle                bool
	def                 string
	expireAt            any
	inventoryExpireAt   any
	activated           bool
	maxActivationExpiry int64
}

func keepRunes(c *catalog.Catalog, next, stored document, now time.Time) int64 {
	storedRunes, _ := stored.optionalSection("runes")
	runes, err := next.section("runes")
	if err != nil {
		runes = document{}
		next["runes"] = map[string]any(runes)
	}
	owned := map[int64]ownedRune{}
	storedItems, _ := storedRunes.array("items")
	for _, entry := range storedItems {
		object, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		uuid, err := numberValue(object["uuid"], "uuid")
		if err != nil {
			continue
		}
		def, _ := object["def"].(string)
		expiry, _ := numberValue(object["expire_at"], "expire_at")
		rune, _ := c.RuneByID(def)
		inventoryExpiry, _ := numberValue(object["inventory_expire_at"], "inventory_expire_at")
		owned[uuid] = ownedRune{
			entry: object, idle: expiry == 0 && inventoryExpiry >= now.Unix(),
			def: def, expireAt: object["expire_at"], inventoryExpireAt: object["inventory_expire_at"],
			activated:           expiry != 0,
			maxActivationExpiry: now.Add(time.Duration(rune.DurationMinutes)*time.Minute + runeActivationSlack).Unix(),
		}
	}
	items, _ := runes.array("items")
	kept := []any{}
	seen := map[int64]bool{}
	activations := int64(0)
	for _, entry := range items {
		object, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		uuid, err := numberValue(object["uuid"], "uuid")
		rune, known := owned[uuid]
		if err != nil || !known || seen[uuid] || object["def"] != rune.def {
			continue
		}
		seen[uuid] = true
		object["inventory_expire_at"] = rune.inventoryExpireAt
		expiry, err := numberValue(object["expire_at"], "expire_at")
		switch {
		case rune.activated:
			object["expire_at"] = rune.expireAt
		case err != nil || expiry < 0:
			object["expire_at"] = json.Number("0")
		case expiry > rune.maxActivationExpiry:
			object["expire_at"] = numberArray([]int64{rune.maxActivationExpiry})[0]
			activations++
		case expiry > 0:
			activations++
		}
		kept = append(kept, object)
	}
	var returned []int64
	for uuid, rune := range owned {
		if !seen[uuid] && rune.idle {
			returned = append(returned, uuid)
		}
	}
	slices.Sort(returned)
	for _, uuid := range returned {
		kept = append(kept, owned[uuid].entry)
	}
	runes["items"] = kept
	if value, ok := storedRunes["next_uuid"]; ok {
		runes["next_uuid"] = value
	}
	keepPrestigeSquads(runes, storedRunes)
	return activations
}

func keepPrestigeSquads(runes, storedRunes document) {
	storedSquads, _ := storedRunes.array("squads")
	squads, _ := runes.array("squads")
	for index, entry := range squads {
		object, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		unlocked := false
		if index < len(storedSquads) {
			if storedSquad, ok := storedSquads[index].(map[string]any); ok {
				unlocked, _ = storedSquad["prestige_unlocked"].(bool)
			}
		}
		object["prestige_unlocked"] = unlocked
	}
}
