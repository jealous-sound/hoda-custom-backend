package economy

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

func rareOnlyPack(heroes, guaranteedEpic, pity int) *catalog.Pack {
	pack := &catalog.Pack{
		HeroCount: heroes, GuaranteedEpic: guaranteedEpic, LegendaryPityPacks: pity, MinimumRarity: catalog.RarityRare,
	}
	pack.Weights[catalog.RarityRare] = 1
	return pack
}

func grantedLegendary(t *testing.T, c *catalog.Catalog, granted Granted) bool {
	t.Helper()
	for _, hero := range granted.Heroes {
		index, ok := c.HeroIndex(hero.ID)
		if !ok {
			t.Fatalf("granted unknown hero %s", hero.ID)
		}
		if c.Heroes[index].Rarity == catalog.RarityLegendary {
			return true
		}
	}
	return false
}

func onSaleInRotation(t *testing.T, c *catalog.Catalog, group string) *catalog.Pack {
	t.Helper()
	for i := range c.Packs {
		pack := &c.Packs[i]
		if pack.Rotation != nil && pack.Rotation.Group == group && c.PackOnSale(pack, springDay) {
			return pack
		}
	}
	t.Fatalf("no %s pack is on sale on %v", group, springDay)
	return nil
}

func priceOf(pack *catalog.Pack) PurchaseRequest {
	return PurchaseRequest{Pack: pack.ID, Currency: pack.Paid.Currency, Price: pack.Paid.Price}
}

func TestTheDuePackHoldsALegendaryInItsFirstRolledSlot(t *testing.T) {
	c := mustCatalog(t)
	single := rareOnlyPack(1, 0, 4)
	bulk := rareOnlyPack(3, 1, 4)
	for seed := uint32(1); seed <= 20; seed++ {
		r := rng{state: seed}
		due, ok := rollHeroes(c, single, packDue{sinceLegendary: single.LegendaryPityPacks - 1}, nil, &r)
		if !ok || due.rarities[0] != catalog.RarityLegendary {
			t.Fatalf("seed %d: the pack that was due rolled %v", seed, due.rarities)
		}
		r = rng{state: seed}
		early, _ := rollHeroes(c, single, packDue{sinceLegendary: single.LegendaryPityPacks - 2}, nil, &r)
		if early.rarities[0] != catalog.RarityRare {
			t.Fatalf("seed %d: a pack one short of the pity rolled %v", seed, early.rarities)
		}
		r = rng{state: seed}
		bulkDue, _ := rollHeroes(c, bulk, packDue{sinceLegendary: bulk.LegendaryPityPacks - 1}, nil, &r)
		want := []catalog.Rarity{catalog.RarityEpic, catalog.RarityLegendary, catalog.RarityRare}
		if !slices.Equal(bulkDue.rarities, want) {
			t.Fatalf("seed %d: the due bulk pack rolled %v, want its Epic kept and one Legendary", seed, bulkDue.rarities)
		}
	}
}

func TestPityCountsPacksAndResetsOnAnyLegendary(t *testing.T) {
	cases := []struct {
		since    int
		rarities []catalog.Rarity
		want     int
	}{
		{5, []catalog.Rarity{catalog.RarityRare, catalog.RarityRare, catalog.RarityEpic}, 6},
		{5, []catalog.Rarity{catalog.RarityRare, catalog.RarityLegendary}, 0},
		{0, []catalog.Rarity{catalog.RarityLegendary}, 0},
		{0, []catalog.Rarity{catalog.RarityEpic}, 1},
	}
	for _, tc := range cases {
		if got := packsSinceLegendaryAfter(tc.since, tc.rarities); got != tc.want {
			t.Errorf("after %d packs, a pack of %v leaves %d, want %d", tc.since, tc.rarities, got, tc.want)
		}
	}
}

func TestEveryPackThatCanMissALegendaryHasAPity(t *testing.T) {
	c := mustCatalog(t)
	for _, pack := range c.Packs {
		if pack.Kind != catalog.KindHero || pack.TutorialOnly {
			continue
		}
		weight := pack.Weights[catalog.RarityLegendary]
		canMiss := weight > 0 && weight < catalog.HeroWeightTotal && pack.GuaranteedLegendary == 0 &&
			pack.GuaranteedSlots() < pack.HeroCount
		if canMiss != (pack.LegendaryPityPacks > 0) {
			t.Errorf("pack %s can miss a Legendary: %v, pity %d", pack.ID, canMiss, pack.LegendaryPityPacks)
		}
	}
}

func TestPurchaseGuaranteesALegendaryOnTheLastPackOfThePity(t *testing.T) {
	c := mustCatalog(t)
	champion, _ := c.Pack("champion")
	request := priceOf(champion)
	for seed := uint32(1); seed <= 10; seed++ {
		raw := fixture{gems: 1000, packsSinceLegendary: map[string]int{"champion": champion.LegendaryPityPacks - 1}}.profile(t)
		next, granted, err := Purchase(c, raw, &profile.ServerState{StoreRNG: seed}, request, springDay)
		if err != nil {
			t.Fatal(err)
		}
		if !grantedLegendary(t, c, granted) {
			t.Fatalf("seed %d: pack %d of %d gave %v", seed, champion.LegendaryPityPacks, champion.LegendaryPityPacks, granted.Heroes)
		}
		if after := decode(t, next); after.Store.PacksSinceLegendary["champion"] != 0 {
			t.Errorf("seed %d: the counter stayed at %d after the Legendary", seed, after.Store.PacksSinceLegendary["champion"])
		}
	}
}

func TestEachPackKeepsItsOwnPityCounter(t *testing.T) {
	c := mustCatalog(t)
	champion, _ := c.Pack("champion")
	server := &profile.ServerState{StoreRNG: 2024}
	raw := fixture{gems: 100_000, packsSinceLegendary: map[string]int{"knight": 7}}.profile(t)
	since := 0
	for i := 0; i < 40; i++ {
		next, granted, err := Purchase(c, raw, server, priceOf(champion), springDay)
		if err != nil {
			t.Fatal(err)
		}
		after := decode(t, next)
		want := since + 1
		if grantedLegendary(t, c, granted) {
			want = 0
		}
		if after.Store.PacksSinceLegendary["champion"] != want {
			t.Fatalf("pack %d: champion counter %d, want %d", i+1, after.Store.PacksSinceLegendary["champion"], want)
		}
		if after.Store.PacksSinceLegendary["knight"] != 7 {
			t.Fatalf("pack %d: a Champion Pack moved the Knight counter to %d", i+1, after.Store.PacksSinceLegendary["knight"])
		}
		if after.Store.ReleasedClientPity == nil || *after.Store.ReleasedClientPity != 0 {
			t.Fatalf("pack %d: released clients lost the counter they read: %v", i+1, after.Store.ReleasedClientPity)
		}
		since, raw = want, next
	}
}

func TestPacksThatTakeTurnsShareOneCounter(t *testing.T) {
	c := mustCatalog(t)
	booster := onSaleInRotation(t, c, "booster")
	if booster.PityCounter() != "booster" {
		t.Fatalf("booster %s counts pity under %q", booster.ID, booster.PityCounter())
	}
	raw := fixture{gems: 1000, packsSinceLegendary: map[string]int{"booster": booster.LegendaryPityPacks - 1}}.profile(t)
	after, granted := buy(t, raw, &profile.ServerState{StoreRNG: 31}, priceOf(booster))
	if !grantedLegendary(t, c, granted) {
		t.Errorf("the shared booster counter was due but %s gave %v", booster.ID, granted.Heroes)
	}
	if _, own := after.Store.PacksSinceLegendary[booster.ID]; own || after.Store.PacksSinceLegendary["booster"] != 0 {
		t.Errorf("counters = %v", after.Store.PacksSinceLegendary)
	}
}

func TestPacksWithoutAPityAndScriptedPacksLeaveTheCountersAlone(t *testing.T) {
	c := mustCatalog(t)
	start := map[string]int{"champion": 12}
	after, _ := buy(t, fixture{gold: 1000, packsSinceLegendary: start}.profile(t), &profile.ServerState{StoreRNG: 3},
		PurchaseRequest{Pack: "recruit", Currency: catalog.CurrencyGold, Price: 350})
	if len(after.Store.PacksSinceLegendary) != 1 || after.Store.PacksSinceLegendary["champion"] != 12 {
		t.Errorf("a Recruit Pack changed the counters to %v", after.Store.PacksSinceLegendary)
	}
	champion, _ := c.Pack("champion")
	tutorial := map[string]any{"complete": false, "consume_complete": false, "restore_step": 3}
	raw := fixture{gems: 1000, tutorial: tutorial, packsSinceLegendary: start}.profile(t)
	scripted := priceOf(champion)
	scripted.Scripted = true
	after, granted := buy(t, raw, &profile.ServerState{StoreRNG: 3}, scripted)
	if granted.Heroes[0].ID != c.Scripted["champion"].Heroes[0] || after.Store.PacksSinceLegendary["champion"] != 12 {
		t.Errorf("the scripted Champion Pack gave %v and left the counter at %d", granted.Heroes,
			after.Store.PacksSinceLegendary["champion"])
	}
}

func TestUploadsCannotMoveThePityCounters(t *testing.T) {
	c := mustCatalog(t)
	stored := profile.Record{Profile: fixture{packsSinceLegendary: map[string]int{"champion": 4}}.profile(t), Revision: 1}
	forged := fixture{packsSinceLegendary: map[string]int{"champion": 99, "knight": 99}}.profile(t)
	merged, err := MergeUpload(c, stored, &profile.ServerState{}, forged, springDay)
	if err != nil {
		t.Fatal(err)
	}
	if counters := decode(t, merged).Store.PacksSinceLegendary; len(counters) != 1 || counters["champion"] != 4 {
		t.Errorf("an upload set the pity counters to %v", counters)
	}
}

func TestMalformedPityCountersAreRefused(t *testing.T) {
	c := mustCatalog(t)
	for _, counters := range []any{[]any{1, 2}, map[string]any{"champion": -1}, map[string]any{"champion": "many"}} {
		var doc map[string]any
		if err := json.Unmarshal(fixture{gems: 1000}.profile(t), &doc); err != nil {
			t.Fatal(err)
		}
		doc["store"].(map[string]any)[packsSinceLegendary] = counters
		raw, _ := json.Marshal(doc)
		champion, _ := c.Pack("champion")
		if _, _, err := Purchase(c, raw, &profile.ServerState{}, priceOf(champion), springDay); refusalCode(err) != BadProfile {
			t.Errorf("counters %v: got %v", counters, err)
		}
	}
}
