package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

func renownTable(t *testing.T, edit func(table map[string]any)) func(name string) ([]byte, error) {
	t.Helper()
	shipped, err := embedded.ReadFile("data/" + RenownFile)
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

func TestRenownTableLoadsEveryPerk(t *testing.T) {
	rules := mustDefault(t).Renown
	if rules.LevelsPerRank != 10 || rules.RankGems != 10 || rules.PerkMaxRank != 5 {
		t.Errorf("rules = %+v", rules)
	}
	if len(rules.PerkPerRank) != len(RenownPerks) {
		t.Errorf("%d perks loaded, want %d", len(rules.PerkPerRank), len(RenownPerks))
	}
	for _, perk := range RenownPerks {
		if !rules.KnowsPerk(perk) || rules.PerkPerRank[perk] <= 0 {
			t.Errorf("perk %s gives %d per rank", perk, rules.PerkPerRank[perk])
		}
	}
	if rules.KnowsPerk("tier_ranks") || rules.KnowsPerk("") {
		t.Error("a perk outside the table is known")
	}
	perRank := rules.PerkPerRank[RenownCritPermille]
	for rank, want := range map[int]int{-1: 0, 0: 0, 1: perRank, 5: 5 * perRank, 6: 5 * perRank} {
		if got := rules.PerkValue(RenownCritPermille, rank); got != want {
			t.Errorf("rank %d is worth %d, want %d", rank, got, want)
		}
	}
}

func TestABrokenRenownTableIsRejected(t *testing.T) {
	perks := func(table map[string]any) map[string]any { return table["perks"].(map[string]any) }
	broken := []struct {
		name, refusedFor string
		edit             func(table map[string]any)
	}{
		{"another version", "version 2", func(table map[string]any) { table["version"] = 2 }},
		{"no levels per rank", "levels_per_rank", func(table map[string]any) { table["levels_per_rank"] = 0 }},
		{"negative rank gems", "rank_gems", func(table map[string]any) { table["rank_gems"] = -10 }},
		{"no perk limit", "perk_max_rank", func(table map[string]any) { delete(table, "perk_max_rank") }},
		{"a perk left out", "missing perk " + RenownKeyPct, func(table map[string]any) {
			delete(perks(table), RenownKeyPct)
		}},
		{"a perk worth nothing", RenownGoldPct + " gives 0", func(table map[string]any) {
			perks(table)[RenownGoldPct] = 0
		}},
		{"a perk that takes away", RenownStaminaMax + " gives -1", func(table map[string]any) {
			perks(table)[RenownStaminaMax] = -1
		}},
		{"a perk the server cannot apply", "unknown perk luck_pct", func(table map[string]any) {
			perks(table)["luck_pct"] = 3
		}},
		{"a fractional value", "decode", func(table map[string]any) { perks(table)[RenownPowerPct] = 2.5 }},
	}
	for _, c := range broken {
		err := (&Catalog{}).loadRenown(renownTable(t, c.edit))
		if err == nil || !strings.Contains(err.Error(), c.refusedFor) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if err := (&Catalog{}).loadRenown(renownTable(t, func(map[string]any) {})); err != nil {
		t.Errorf("the shipped table: %v", err)
	}
}
