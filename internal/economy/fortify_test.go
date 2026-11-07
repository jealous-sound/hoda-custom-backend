package economy

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

func rosterEntry(id string, instance int, tier string) map[string]any {
	return map[string]any{
		"id": id, "instance_id": instance, "tier": tier, "level": 1, "xp": 0, "ascended": false, "consume_xp": 0,
	}
}

func fortifyEntry(slot int, id, tier string, ascended bool) map[string]any {
	entry := map[string]any{"slot": slot, "id": id, "tier": tier}
	if ascended {
		entry["ascended"] = true
	}
	return entry
}

func fortifyProfile(t *testing.T, parentFortify []any, edit func(doc map[string]any), extra ...map[string]any) json.RawMessage {
	t.Helper()
	return gameProfile(t, func(doc map[string]any) {
		roster := section(doc, "roster")
		for _, entry := range extra {
			roster["additions"] = append(roster["additions"].([]any), entry)
		}
		if parentFortify != nil {
			roster["additions"].([]any)[0].(map[string]any)["fortify"] = parentFortify
		}
		if edit != nil {
			edit(doc)
		}
	})
}

func fortifyJSON(t *testing.T, doc map[string]any, instance float64) string {
	t.Helper()
	value, ok := rosterHero(t, doc, instance)["fortify"]
	if !ok {
		return ""
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func hasHero(doc map[string]any, instance float64) bool {
	for _, list := range rosterLists {
		entries, _ := section(doc, "roster")[list].([]any)
		for _, entry := range entries {
			if entry.(map[string]any)["instance_id"].(float64) == instance {
				return true
			}
		}
	}
	return false
}

func TestFortifySlotsOpenWithTheConsumeLevel(t *testing.T) {
	c := mustCatalog(t)
	def, _ := c.Hero("UNIT_ELVENMAGE")
	table := def.Consume.ConsumeTable
	for _, test := range []struct {
		consumeXP int64
		want      int
	}{
		{0, 2},
		{xpForLevel(table, 4) - 1, 2},
		{xpForLevel(table, 4), 3},
		{xpForLevel(table, 9), 4},
		{xpForLevel(table, int64(len(table))), catalog.FortifySlotCount},
	} {
		entry := map[string]any{"id": "UNIT_ELVENMAGE", "instance_id": json.Number("1"), "tier": "I",
			"consume_xp": json.Number(strconv.FormatInt(test.consumeXP, 10))}
		h, ok := readHero(c, entry)
		if !ok {
			t.Fatal("hero not read")
		}
		if got := h.unlockedFortifySlots(); got != test.want {
			t.Errorf("consume_xp %d opens %d slots, want %d", test.consumeXP, got, test.want)
		}
	}
}

func TestFortifyStoresTheChildrenAndConsumesThem(t *testing.T) {
	raw := fortifyProfile(t, nil, func(doc map[string]any) {
		section(doc, "gear")["heroes"] = []any{map[string]any{"instance_id": 3, "ascended": false,
			"prestige_unlocked": false, "slots": []any{"templar_longsword"}}}
	}, rosterEntry("UNIT_REDIRONMAGE", 3, "II"))
	doc, _ := mustAct(t, raw, &profile.ServerState{}, ActionFortify,
		`{"parent":1,"slots":[{"index":1,"unit":3},{"index":0,"unit":2}]}`)
	want := `[{"id":"UNIT_ARIQUN","slot":0,"tier":"I"},{"id":"UNIT_REDIRONMAGE","slot":1,"tier":"II"}]`
	if got := fortifyJSON(t, doc, 1); got != want {
		t.Errorf("fortify = %s, want %s", got, want)
	}
	if hasHero(doc, 2) || hasHero(doc, 3) {
		t.Errorf("the sacrificed heroes stayed in the roster: %v", doc["roster"])
	}
	if heroes := section(doc, "gear")["heroes"].([]any); len(heroes) != 0 {
		t.Errorf("the sacrificed hero kept its gear: %v", heroes)
	}
}

func TestFortifyCombinesTheSameHeroIntoAHigherTier(t *testing.T) {
	ascended := rosterEntry("UNIT_ARIQUN", 3, "I")
	ascended["ascended"] = true
	for _, test := range []struct {
		name     string
		existing map[string]any
		child    map[string]any
		want     string
	}{
		{"I and I", fortifyEntry(0, "UNIT_ELVENMAGE", "I", false), rosterEntry("UNIT_ELVENMAGE", 3, "I"),
			`[{"id":"UNIT_ELVENMAGE","slot":0,"tier":"II"}]`},
		{"II and I", fortifyEntry(0, "UNIT_ELVENMAGE", "II", false), rosterEntry("UNIT_ELVENMAGE", 3, "I"),
			`[{"id":"UNIT_ELVENMAGE","slot":0,"tier":"III"}]`},
		{"IV and an ascended I", fortifyEntry(0, "UNIT_ARIQUN", "IV", false), ascended,
			`[{"ascended":true,"id":"UNIT_ARIQUN","slot":0,"tier":"IV"}]`},
		{"another hero replaces a full slot", fortifyEntry(0, "UNIT_ELVENMAGE", "IV", false),
			rosterEntry("UNIT_REDIRONMAGE", 3, "I"), `[{"id":"UNIT_REDIRONMAGE","slot":0,"tier":"I"}]`},
	} {
		raw := fortifyProfile(t, []any{test.existing}, nil, test.child)
		doc, _ := mustAct(t, raw, &profile.ServerState{}, ActionFortify, `{"parent":1,"slots":[{"index":0,"unit":3}]}`)
		if got := fortifyJSON(t, doc, 1); got != test.want {
			t.Errorf("%s: fortify = %s, want %s", test.name, got, test.want)
		}
	}
}

func TestFortifyCombinesOnlyIntoPresentTiers(t *testing.T) {
	gapped := catalog.Hero{ID: "UNIT_GAPPED", Rarity: catalog.RarityRare, MaxTier: 2}
	gapped.Tiers[0].Present, gapped.Tiers[2].Present = true, true
	merged, ok := fortifyInto(&fortifyChild{def: gapped}, &fortifyChild{def: gapped})
	if !ok || merged.tier != 2 {
		t.Errorf("I and I of a hero without tier II = %+v", merged)
	}
	merged, ok = fortifyInto(&fortifyChild{def: gapped, tier: 2}, &fortifyChild{def: gapped, ascended: true})
	if !ok || merged.tier != 2 || !merged.ascended {
		t.Errorf("an ascended I into the top tier = %+v", merged)
	}
}

func TestFortifyRefusesASlotThatHoldsTheBestCopy(t *testing.T) {
	ascendedFour := rosterEntry("UNIT_ARIQUN", 3, "IV")
	ascendedFour["ascended"] = true
	for name, test := range map[string]struct {
		existing map[string]any
		child    map[string]any
	}{
		"top tier":                      {fortifyEntry(0, "UNIT_ELVENMAGE", "IV", false), rosterEntry("UNIT_ELVENMAGE", 3, "I")},
		"ascended top tier":             {fortifyEntry(0, "UNIT_ARIQUN", "IV", true), ascendedFour},
		"ascended top tier, plain copy": {fortifyEntry(0, "UNIT_ARIQUN", "IV", true), rosterEntry("UNIT_ARIQUN", 3, "IV")},
	} {
		raw := fortifyProfile(t, []any{test.existing}, nil, test.child)
		_, _, err := act(t, raw, &profile.ServerState{}, ActionFortify, `{"parent":1,"slots":[{"index":0,"unit":3}]}`)
		if refusalCode(err) != Unavailable {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestFortifyRefusesLockedSlotsSquadMembersAndBadChildren(t *testing.T) {
	raw := fortifyProfile(t, nil, withSquad(4), rosterEntry("UNIT_REDIRONMAGE", 3, "I"),
		rosterEntry("UNIT_ELVENMAGE", 4, "I"))
	for _, body := range []string{
		`{"parent":1,"slots":[{"index":0,"unit":4}]}`,
		`{"parent":1,"slots":[{"index":2,"unit":3}]}`,
		`{"parent":1,"slots":[{"index":-1,"unit":3}]}`,
		`{"parent":1,"slots":[{"index":0,"unit":1}]}`,
		`{"parent":1,"slots":[{"index":0,"unit":9}]}`,
		`{"parent":1,"slots":[{"index":0,"unit":3},{"index":1,"unit":3}]}`,
		`{"parent":1,"slots":[{"index":0,"unit":3},{"index":0,"unit":2}]}`,
		`{"parent":9,"slots":[{"index":0,"unit":3}]}`,
		`{"parent":1,"slots":[]}`,
		`{"parent":1,"slots":[{"index":0,"unit":2},{"index":1,"unit":3},{"index":2,"unit":3},{"index":3,"unit":3},
			{"index":4,"unit":3},{"index":5,"unit":3},{"index":6,"unit":3},{"index":7,"unit":3},{"index":8,"unit":3},
			{"index":9,"unit":3},{"index":9,"unit":3}]}`,
	} {
		if _, _, err := act(t, raw, &profile.ServerState{}, ActionFortify, body); refusalCode(err) != Unavailable {
			t.Errorf("%s: %v", body, err)
		}
	}
	opened := fortifyProfile(t, nil, func(doc map[string]any) {
		def, _ := mustCatalog(t).Hero("UNIT_ELVENMAGE")
		parent := section(doc, "roster")["additions"].([]any)[0].(map[string]any)
		parent["consume_xp"] = xpForLevel(def.Consume.ConsumeTable, 4)
	}, rosterEntry("UNIT_REDIRONMAGE", 3, "I"))
	doc, _ := mustAct(t, opened, &profile.ServerState{}, ActionFortify, `{"parent":1,"slots":[{"index":2,"unit":3}]}`)
	if got := fortifyJSON(t, doc, 1); got != `[{"id":"UNIT_REDIRONMAGE","slot":2,"tier":"I"}]` {
		t.Errorf("a slot opened by consume levels = %s", got)
	}
}

func removableProfile(t *testing.T, gems int) json.RawMessage {
	t.Helper()
	return fortifyProfile(t, []any{
		fortifyEntry(0, "UNIT_REDIRONMAGE", "II", false),
		fortifyEntry(3, "UNIT_ARIQUN", "IV", true),
		fortifyEntry(5, "UNIT_ARIQUN", "I", false),
	}, func(doc map[string]any) { section(doc, "account")["gems"] = gems })
}

func TestRemoveFortifyChargesGemsAndReturnsTheHero(t *testing.T) {
	c := mustCatalog(t)
	doc, result := mustAct(t, removableProfile(t, 200), &profile.ServerState{}, ActionRemoveFortify,
		`{"parent":1,"index":3}`)
	if gems := number(t, doc, "account", "gems"); gems != 170 {
		t.Errorf("gems = %v, want 170", gems)
	}
	want := `[{"id":"UNIT_REDIRONMAGE","slot":0,"tier":"II"},{"id":"UNIT_ARIQUN","slot":5,"tier":"I"}]`
	if got := fortifyJSON(t, doc, 1); got != want {
		t.Errorf("fortify = %s, want %s", got, want)
	}
	returned := rosterHero(t, doc, 3)
	def, _ := c.Hero("UNIT_ARIQUN")
	floor := (&hero{def: def}).progressionKeyOfTier(3)
	if returned["id"] != "UNIT_ARIQUN" || returned["tier"] != "IV" || returned["ascended"] != true ||
		returned["level"].(float64) != 1 || returned["xp"].(float64) != float64(xpForLevel(def.Consume.LevelTable, 1)) ||
		returned["consume_xp"].(float64) != float64(xpForLevel(def.Consume.ConsumeTable, int64(floor)+1)) {
		t.Errorf("returned hero = %v", returned)
	}
	if number(t, doc, "roster", "next_instance_id") != 4 {
		t.Errorf("next_instance_id = %v", section(doc, "roster")["next_instance_id"])
	}
	if len(result.Granted.Heroes) != 1 || result.Granted.Heroes[0] != (GrantedHero{ID: "UNIT_ARIQUN", InstanceID: 3}) {
		t.Errorf("granted = %+v", result.Granted.Heroes)
	}
	gear := section(doc, "gear")["heroes"].([]any)
	if len(gear) != 1 || gear[0].(map[string]any)["instance_id"].(float64) != 3 ||
		gear[0].(map[string]any)["ascended"] != true {
		t.Errorf("the returned ascended hero has gear %v", gear)
	}
}

func TestRemoveEveryFortifySlotReturnsTheHeroesInSlotOrder(t *testing.T) {
	doc, result := mustAct(t, removableProfile(t, 200), &profile.ServerState{}, ActionRemoveFortify,
		`{"parent":1,"index":-1}`)
	if gems := number(t, doc, "account", "gems"); gems != 200-5-30-15 {
		t.Errorf("gems = %v, want %d", gems, 200-5-30-15)
	}
	if _, kept := rosterHero(t, doc, 1)["fortify"]; kept {
		t.Errorf("an emptied hero kept its fortify key: %v", rosterHero(t, doc, 1))
	}
	for instance, want := range map[float64][2]string{3: {"UNIT_REDIRONMAGE", "II"}, 4: {"UNIT_ARIQUN", "IV"},
		5: {"UNIT_ARIQUN", "I"}} {
		if hero := rosterHero(t, doc, instance); hero["id"] != want[0] || hero["tier"] != want[1] {
			t.Errorf("hero %v = %v, want %v", instance, hero, want)
		}
	}
	if len(result.Granted.Heroes) != 3 || result.Granted.Heroes[0].InstanceID != 3 ||
		result.Granted.Heroes[2].InstanceID != 5 {
		t.Errorf("granted = %+v", result.Granted.Heroes)
	}
}

func TestRemoveFortifyRefusesEmptySlotsAndThePoor(t *testing.T) {
	raw := removableProfile(t, 200)
	for _, body := range []string{
		`{"parent":1,"index":1}`, `{"parent":1,"index":10}`, `{"parent":1,"index":-2}`, `{"parent":1}`,
		`{"parent":9,"index":0}`,
	} {
		if _, _, err := act(t, raw, &profile.ServerState{}, ActionRemoveFortify, body); refusalCode(err) != Unavailable {
			t.Errorf("%s: %v", body, err)
		}
	}
	if _, _, err := act(t, gameProfile(t, nil), &profile.ServerState{}, ActionRemoveFortify, `{"parent":1,"index":-1}`); refusalCode(err) != Unavailable {
		t.Errorf("nothing to remove: %v", err)
	}
	if _, _, err := act(t, removableProfile(t, 29), &profile.ServerState{}, ActionRemoveFortify, `{"parent":1,"index":3}`); refusalCode(err) != CantAfford {
		t.Errorf("too few gems: %v", err)
	}
	if _, _, err := act(t, removableProfile(t, 49), &profile.ServerState{}, ActionRemoveFortify, `{"parent":1,"index":-1}`); refusalCode(err) != CantAfford {
		t.Errorf("too few gems for every slot: %v", err)
	}
}

func TestUploadCannotAddOrAlterFortify(t *testing.T) {
	stored := removableProfile(t, 200)
	forged := gameProfile(t, func(doc map[string]any) {
		additions := section(doc, "roster")["additions"].([]any)
		additions[0].(map[string]any)["fortify"] = []any{fortifyEntry(0, "UNIT_ARIQUN", "IV", true)}
		additions[1].(map[string]any)["fortify"] = []any{fortifyEntry(0, "UNIT_ARIQUN", "IV", true)}
	})
	doc := merge(t, stored, forged, &profile.ServerState{})
	want := `[{"id":"UNIT_REDIRONMAGE","slot":0,"tier":"II"},{"ascended":true,"id":"UNIT_ARIQUN","slot":3,"tier":"IV"},` +
		`{"id":"UNIT_ARIQUN","slot":5,"tier":"I"}]`
	if got := fortifyJSON(t, doc, 1); got != want {
		t.Errorf("fortify = %s, want %s", got, want)
	}
	if _, added := rosterHero(t, doc, 2)["fortify"]; added {
		t.Errorf("an upload fortified hero 2: %v", rosterHero(t, doc, 2))
	}
	cleared := gameProfile(t, nil)
	doc = merge(t, stored, cleared, &profile.ServerState{})
	if got := fortifyJSON(t, doc, 1); got != want {
		t.Errorf("an upload without fortify cleared it: %s", got)
	}
}

func TestBattleStartFieldsTheStoredFortify(t *testing.T) {
	raw := fortifyProfile(t, []any{fortifyEntry(0, "UNIT_REDIRONMAGE", "II", false),
		fortifyEntry(1, "UNIT_ARIQUN", "IV", true)}, nil)
	stored := []battle.FortifyChild{
		{Slot: 0, ID: "UNIT_REDIRONMAGE", Tier: "II"}, {Slot: 1, ID: "UNIT_ARIQUN", Tier: "IV", Ascended: true},
	}
	request := campaignStart(t, 0, 0)
	request.Squad.Units[0].Fortify = stored
	server := &profile.ServerState{}
	if _, _, err := startBattle(t, raw, server, request, 1); err != nil {
		t.Fatalf("a fortified squad: %v", err)
	}
	if fielded := server.Battle.Replay.Player.Units[0].Fortify; !sameJSON(fielded, stored) {
		t.Errorf("the replay fields %+v", fielded)
	}
	for name, claimed := range map[string][]battle.FortifyChild{
		"none":         nil,
		"higher tier":  {{Slot: 0, ID: "UNIT_REDIRONMAGE", Tier: "III"}, stored[1]},
		"extra child":  append(append([]battle.FortifyChild{}, stored...), battle.FortifyChild{Slot: 2, ID: "UNIT_OGRE", Tier: "I"}),
		"not ascended": {stored[0], {Slot: 1, ID: "UNIT_ARIQUN", Tier: "IV"}},
	} {
		forged := campaignStart(t, 0, 0)
		forged.Squad.Units[0].Fortify = claimed
		if _, _, err := startBattle(t, raw, &profile.ServerState{}, forged, 1); refusalCode(err) != SquadChanged {
			t.Errorf("%s: %v", name, err)
		}
	}
	unfortified := campaignStart(t, 0, 0)
	unfortified.Squad.Units[1].Fortify = stored
	if _, _, err := startBattle(t, raw, &profile.ServerState{}, unfortified, 1); refusalCode(err) != SquadChanged {
		t.Errorf("fortify claimed for an unfortified hero: %v", err)
	}
}
