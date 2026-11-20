package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

func challengeDropsTable(t *testing.T, edit func(table map[string]any)) func(name string) ([]byte, error) {
	t.Helper()
	shipped, err := embedded.ReadFile("data/" + ChallengeDropsFile)
	if err != nil {
		t.Fatal(err)
	}
	var table map[string]any
	if err := json.Unmarshal(shipped, &table); err != nil {
		t.Fatal(err)
	}
	edit(table)
	edited, err := json.Marshal(table)
	if err != nil {
		t.Fatal(err)
	}
	return func(string) ([]byte, error) { return edited, nil }
}

func TestChallengeDropsCoverEveryChallengeNodeWithResearchRates(t *testing.T) {
	c := mustDefault(t)
	drops := c.ChallengeDrops
	if drops.RollTotal != 100000 {
		t.Fatalf("roll total = %d", drops.RollTotal)
	}
	for _, m := range c.Campaign {
		listed := len(drops.Maps[m.ID]) > 0
		if hasGrind := m.Nodes[len(m.Nodes)-1].Grind; listed != hasGrind {
			t.Errorf("%s: challenge node %v, drops listed %v", m.ID, hasGrind, listed)
		}
	}
	bear, ok := drops.Pick("PVE_MAP_02", 40738)
	if !ok || bear.Kind != ChallengeDropUnit || bear.ID != "UNIT_BEAR" || bear.Weight != 9601 {
		t.Errorf("roll 40738 on map 2 = %+v, %v", bear, ok)
	}
	if nothing, ok := drops.Pick("PVE_MAP_02", 70369); ok {
		t.Errorf("roll 70369 on map 2 dropped %+v", nothing)
	}
	if _, ok := drops.Pick("PVE_MAP_01", 0); ok {
		t.Error("map 1 dropped something")
	}
}

func TestABrokenChallengeDropTableIsRejected(t *testing.T) {
	first := func(table map[string]any) map[string]any { return table["maps"].([]any)[0].(map[string]any) }
	firstDrop := func(table map[string]any) map[string]any {
		return first(table)["drops"].([]any)[0].(map[string]any)
	}
	broken := []struct {
		name, refusedFor string
		edit             func(table map[string]any)
	}{
		{"another version", "version 2", func(table map[string]any) { table["version"] = 2 }},
		{"no roll total", "roll_total", func(table map[string]any) { table["roll_total"] = 0 }},
		{"a map without a challenge node", "PVE_MAP_01, which has no challenge node", func(table map[string]any) {
			first(table)["map"] = "PVE_MAP_01"
		}},
		{"an unknown hero", "unknown or unobtainable hero UNIT_NOBODY", func(table map[string]any) {
			firstDrop(table)["kind"], firstDrop(table)["id"] = "unit", "UNIT_NOBODY"
		}},
		{"an unknown rune", "unknown rune rune-luck-1", func(table map[string]any) {
			firstDrop(table)["kind"], firstDrop(table)["id"] = "rune", "rune-luck-1"
		}},
		{"an unknown kind", "unknown kind \"gear\"", func(table map[string]any) { firstDrop(table)["kind"] = "gear" }},
		{"a weightless drop", "weighs 0", func(table map[string]any) { firstDrop(table)["weight"] = 0 }},
		{"a table that always drops", "leaving no empty roll", func(table map[string]any) {
			firstDrop(table)["weight"] = 100000
		}},
		{"a map listed twice", "twice", func(table map[string]any) {
			table["maps"] = append(table["maps"].([]any), first(table))
		}},
	}
	c := mustDefault(t)
	for _, b := range broken {
		err := c.loadChallengeDrops(challengeDropsTable(t, b.edit))
		if err == nil || !strings.Contains(err.Error(), b.refusedFor) {
			t.Errorf("%s: %v", b.name, err)
		}
	}
	if err := c.loadChallengeDrops(challengeDropsTable(t, func(map[string]any) {})); err != nil {
		t.Errorf("the shipped table: %v", err)
	}
}
