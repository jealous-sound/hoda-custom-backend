package economy

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
)

type rosterWriter struct {
	roster       document
	additions    []any
	starterCount int
	nextID       int64
}

func newRosterWriter(doc document) (*rosterWriter, error) {
	roster, err := doc.section("roster")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	additions, err := roster.array("additions")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	starters, err := roster.array("starters")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	starterCount, err := roster.optionalInt("starter_count")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	next, err := roster.int64Field("next_instance_id")
	if err != nil || next < firstInstanceID {
		next = firstInstanceID
	}
	for _, list := range [][]any{starters, additions} {
		for _, entry := range list {
			object, ok := entry.(map[string]any)
			if !ok {
				return nil, refuse(BadProfile, "roster entry is not an object")
			}
			id, err := numberValue(object["instance_id"], "instance_id")
			if err != nil {
				return nil, refuse(BadProfile, "%v", err)
			}
			if id >= next {
				next = id + 1
			}
		}
	}
	return &rosterWriter{roster: roster, additions: additions, starterCount: starterCount, nextID: next}, nil
}

func (r *rosterWriter) hasRoom(count int) bool {
	return r.starterCount+len(r.additions)+count <= RosterCapacity
}

func (r *rosterWriter) add(hero catalog.Hero) int64 {
	id, _ := r.addEntry(hero)
	return id
}

func (r *rosterWriter) addEntry(hero catalog.Hero) (int64, map[string]any) {
	id := r.nextID
	r.nextID++
	entry := map[string]any{
		"id":          hero.ID,
		"instance_id": json.Number(strconv.FormatInt(id, 10)),
		"tier":        hero.InitialTier,
		"level":       json.Number(strconv.Itoa(newHeroLevel)),
		"xp":          json.Number("0"),
		"ascended":    false,
		"consume_xp":  json.Number("0"),
	}
	r.additions = append(r.additions, entry)
	return id, entry
}

func (r *rosterWriter) store() {
	r.roster["additions"] = r.additions
	r.roster.setInt("next_instance_id", r.nextID)
}

type runeWriter struct {
	runes   document
	items   []any
	nextID  int64
	expires int64
}

func newRuneWriter(doc document, now time.Time) (*runeWriter, error) {
	if _, ok := doc["runes"]; !ok {
		return nil, refuse(BadProfile, "profile has no runes section")
	}
	runes, err := doc.section("runes")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	items, err := runes.array("items")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	next, err := runes.int64Field("next_uuid")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	if next < firstRuneUUID {
		next = firstRuneUUID
	}
	expires := now.Add(RuneInventoryLifetime).Unix()
	return &runeWriter{runes: runes, items: items, nextID: next, expires: expires}, nil
}

func (r *runeWriter) hasRoom(count int) bool {
	return RuneInventoryCapacity-len(r.items) >= count
}

func (r *runeWriter) grant(id string) bool {
	if !r.hasRoom(1) {
		return false
	}
	r.items = append(r.items, map[string]any{
		"uuid":                json.Number(strconv.FormatInt(r.nextID, 10)),
		"def":                 id,
		"expire_at":           json.Number("0"),
		"inventory_expire_at": json.Number(strconv.FormatInt(r.expires, 10)),
	})
	r.nextID++
	return true
}

func (r *runeWriter) store() {
	r.runes["items"] = r.items
	r.runes.setInt("next_uuid", r.nextID)
}

func ensureGearSection(doc document) (document, error) {
	_, existed := doc["gear"]
	gear, err := doc.section("gear")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	if !existed {
		gear["inventory"] = map[string]any{}
		gear["heroes"] = []any{}
		gear.setInt("tomes", 0)
	}
	return gear, nil
}

func grantGear(doc document, id string, tome bool) error {
	gear, err := ensureGearSection(doc)
	if err != nil {
		return err
	}
	if tome {
		tomes, err := gear.optionalInt("tomes")
		if err != nil {
			return refuse(BadProfile, "%v", err)
		}
		gear.setInt("tomes", int64(tomes)+1)
		return nil
	}
	inventory, err := gear.section("inventory")
	if err != nil {
		return refuse(BadProfile, "%v", err)
	}
	count, err := inventory.optionalInt(id)
	if err != nil {
		return refuse(BadProfile, "%v", err)
	}
	inventory.setInt(id, int64(count)+1)
	return nil
}

func addMissionCounter(doc document, stat string, amount int64) error {
	missions, err := doc.section("missions")
	if err != nil {
		return refuse(BadProfile, "%v", err)
	}
	counters, err := missions.section("counters")
	if err != nil {
		return refuse(BadProfile, "%v", err)
	}
	value := int64(0)
	if _, ok := counters[stat]; ok {
		value, err = counters.int64Field(stat)
		if err != nil {
			return refuse(BadProfile, "%v", err)
		}
	}
	counters.setInt(stat, value+amount)
	return nil
}
