package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

func vaultTable(t *testing.T, edit func(table map[string]any)) func(name string) ([]byte, error) {
	t.Helper()
	shipped, err := embedded.ReadFile("data/" + VaultFile)
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

func TestVaultRulesBoundTheDailyInterest(t *testing.T) {
	rules := mustDefault(t).Vault
	ceiling := rules.MilligemsPerDay(rules.Capacity())
	if rules.MilligemsPerDay(rules.Capacity()*10) != ceiling {
		t.Error("gems past the capacity still earn interest")
	}
	previous := int64(0)
	for locked := int64(0); locked <= rules.Capacity(); locked += 100 {
		earned := rules.MilligemsPerDay(locked)
		if earned < previous {
			t.Errorf("%d locked earns less than %d locked", locked, locked-100)
		}
		previous = earned
	}
	if rules.Tier(rules.Capacity()) != VaultTierGold {
		t.Error("a full vault is not gold")
	}
}

func TestEarlyWithdrawFeeRoundsUp(t *testing.T) {
	rules := VaultRules{EarlyWithdrawFeePct: 10}
	for gems, want := range map[int64]int64{0: 0, 1: 1, 10: 1, 11: 2, 1000: 100} {
		if got := rules.EarlyWithdrawFee(gems); got != want {
			t.Errorf("fee on %d is %d, want %d", gems, got, want)
		}
	}
}

func TestABrokenVaultTableIsRejected(t *testing.T) {
	tiers := func(table map[string]any) map[string]any { return table["tier_gems"].(map[string]any) }
	brackets := func(table map[string]any) []any { return table["brackets"].([]any) }
	broken := []struct {
		name, refusedFor string
		edit             func(table map[string]any)
	}{
		{"another version", "version 2", func(table map[string]any) { table["version"] = 2 }},
		{"no lock", "lock_days", func(table map[string]any) { table["lock_days"] = 0 }},
		{"no window", "unclaimed_days", func(table map[string]any) { delete(table, "unclaimed_days") }},
		{"fee past a whole", "early_withdraw_fee_pct", func(table map[string]any) { table["early_withdraw_fee_pct"] = 101 }},
		{"no brackets", "no brackets", func(table map[string]any) { table["brackets"] = []any{} }},
		{"falling bracket", "bracket 1", func(table map[string]any) {
			brackets(table)[1].(map[string]any)["up_to"] = 500
		}},
		{"free bracket", "bracket 0", func(table map[string]any) {
			brackets(table)[0].(map[string]any)["basis_points_per_day"] = 0
		}},
		{"missing tier", "tier silver", func(table map[string]any) { delete(tiers(table), "silver") }},
		{"tier past the capacity", "tier gold", func(table map[string]any) { tiers(table)["gold"] = 15001 }},
		{"unknown tier", "outside", func(table map[string]any) { tiers(table)["platinum"] = 15000 }},
	}
	for _, c := range broken {
		err := (&Catalog{}).loadVault(vaultTable(t, c.edit))
		if err == nil || !strings.Contains(err.Error(), c.refusedFor) {
			t.Errorf("%s: %v, want an error naming %q", c.name, err, c.refusedFor)
		}
	}
	if err := (&Catalog{}).loadVault(vaultTable(t, func(map[string]any) {})); err != nil {
		t.Errorf("the shipped table: %v", err)
	}
}
