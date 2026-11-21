package economy

import (
	"encoding/json"
	"maps"
	"testing"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

func grantedStars(t *testing.T, c *catalog.Catalog, granted Granted) []int {
	t.Helper()
	var stars []int
	for _, hero := range granted.Heroes {
		index, _ := c.HeroIndex(hero.ID)
		if c.Heroes[index].Rarity == catalog.RarityLegendary {
			stars = append(stars, c.Heroes[index].Stars)
		}
	}
	return stars
}

func TestStarGuaranteesFireEveryNthPackAndDeferTheLesserOne(t *testing.T) {
	pack := &catalog.Pack{ID: "champion", LegendaryPityPacks: 2, StarGuaranteePacks: [catalog.LegendaryStarMax + 1]int{0, 0, 4, 8}}
	firedOn := map[int]int{4: 2, 8: 3, 9: 2, 13: 2, 16: 3, 17: 2, 21: 2, 24: 3, 25: 2}
	var counters starCounters
	for opened := 1; opened <= 25; opened++ {
		fired := dueStarGuarantee(pack, counters)
		if fired != firedOn[opened] {
			t.Fatalf("pack %d fired the %d-star guarantee, want %d", opened, fired, firedOn[opened])
		}
		counters = starCountersAfter(pack, counters, fired)
	}
	overdue := starCountersAfter(pack, starCounters{0, 0, 40, 7}, catalog.LegendaryStarMax)
	if overdue[2] != 3 || overdue[3] != 0 {
		t.Errorf("a deferred guarantee left the counters at %v, want it still due next pack", overdue)
	}
	if got := dueStarGuarantee(&catalog.Pack{}, starCounters{0, 0, 500, 500}); got != 0 {
		t.Errorf("a pack without star guarantees fired a %d-star one", got)
	}
}

func TestADueStarGuaranteeRollsItsTierInThePitySlot(t *testing.T) {
	c := mustCatalog(t)
	grand := rareOnlyPack(4, 1, 10)
	grand.LegendaryStarWeights = starWeights{0, 2000, 6500, 1500}
	grand.StarGuaranteePacks = starWeights{0, 0, 13, 64}
	sawTwoStars := false
	for seed := uint32(1); seed <= legendSeeds; seed++ {
		for _, lowest := range []int{2, 3} {
			r := rng{state: seed}
			roll, ok := rollHeroes(c, grand, packDue{guaranteedStars: lowest}, map[int]bool{}, &r)
			if !ok {
				t.Fatalf("seed %d: the roll failed", seed)
			}
			for slot, hero := range roll.heroes {
				isLegend := roll.rarities[slot] == catalog.RarityLegendary
				if isLegend != (slot == grand.PitySlot()) {
					t.Fatalf("seed %d: slot %d rolled %v", seed, slot, roll.rarities)
				}
				if isLegend && c.Heroes[hero].Stars < lowest {
					t.Fatalf("seed %d: a %d-star guarantee rolled a %d-star Legendary", seed, lowest, c.Heroes[hero].Stars)
				}
				sawTwoStars = sawTwoStars || (isLegend && lowest == 2 && c.Heroes[hero].Stars == 2)
			}
		}
	}
	if !sawTwoStars {
		t.Error("the two-star guarantee never rolled a two-star Legendary")
	}
}

func TestAThreeStarGuaranteeRepeatsAThreeStarOnceTheTierIsOwned(t *testing.T) {
	c := mustCatalog(t)
	owned := map[int]bool{}
	ownAll(owned, legendsOfStars(c, 3))
	bloodstoneLike := starPack(starWeights{0, 2000, 6500, 1500}, true)
	for seed := uint32(1); seed <= legendSeeds; seed++ {
		r := rng{state: seed}
		hero := pickHero(c, bloodstoneLike, heroSlot{rarity: catalog.RarityLegendary, lowestStars: 3}, owned, nil, &r)
		if hero == noRoll || c.Heroes[hero].Stars != 3 {
			t.Fatalf("seed %d rolled %d, want a three-star Legendary", seed, hero)
		}
	}
}

func TestPurchaseGrantsTheDueStarGuaranteeAndKeepsCounting(t *testing.T) {
	c := mustCatalog(t)
	champion, _ := c.Pack("champion")
	twoStars, threeStars := champion.StarGuaranteePacks[2], champion.StarGuaranteePacks[3]
	start := map[string]map[string]int{"champion": {"2": twoStars - 1, "3": threeStars - 2}}
	raw := fixture{gems: 1000, starCounters: start, packsSinceLegendary: map[string]int{"champion": 7}}.profile(t)
	server := &profile.ServerState{StoreRNG: 77}
	steps := []struct {
		lowest int
		after  map[string]int
	}{
		{2, map[string]int{"3": threeStars - 1}},
		{3, map[string]int{"2": 1}},
		{0, map[string]int{"2": 2, "3": 1}},
	}
	for i, step := range steps {
		next, granted, err := Purchase(c, raw, server, priceOf(champion), springDay)
		if err != nil {
			t.Fatal(err)
		}
		after := decode(t, next)
		stars := grantedStars(t, c, granted)
		if step.lowest > 0 && (len(stars) != 1 || stars[0] < step.lowest) {
			t.Fatalf("pack %d gave Legendaries of %v stars, want one of at least %d", i+1, stars, step.lowest)
		}
		if step.lowest > 0 && after.Store.PacksSinceLegendary["champion"] != 0 {
			t.Errorf("pack %d: the guaranteed Legendary left the pity at %d", i+1, after.Store.PacksSinceLegendary["champion"])
		}
		if got := after.Store.StarCounters["champion"]; !maps.Equal(got, step.after) {
			t.Fatalf("pack %d left the star counters at %v, want %v", i+1, got, step.after)
		}
		raw = next
	}
}

func TestBothGuaranteesDueGrantTheThreeStarAndKeepTheOtherDue(t *testing.T) {
	c := mustCatalog(t)
	grand, _ := c.Pack("grand_champion")
	due := map[string]map[string]int{"grand_champion": {"2": grand.StarGuaranteePacks[2] - 1, "3": grand.StarGuaranteePacks[3] - 1}}
	raw := fixture{gems: 1000, starCounters: due}.profile(t)
	after, granted := buy(t, raw, &profile.ServerState{StoreRNG: 5}, priceOf(grand))
	threeStars := 0
	for _, stars := range grantedStars(t, c, granted) {
		if stars == 3 {
			threeStars++
		}
	}
	if threeStars == 0 {
		t.Fatalf("both guarantees were due but the pack gave %v-star Legendaries", grantedStars(t, c, granted))
	}
	want := map[string]int{"2": grand.StarGuaranteePacks[2] - 1}
	if got := after.Store.StarCounters["grand_champion"]; !maps.Equal(got, want) {
		t.Errorf("counters %v, want the two-star guarantee still due (%v)", got, want)
	}
}

func TestPacksWithoutStarGuaranteesAndScriptedPacksLeaveTheStarCountersAlone(t *testing.T) {
	c := mustCatalog(t)
	start := map[string]map[string]int{"champion": {"2": 12, "3": 300}}
	knight, _ := c.Pack("knight")
	after, _ := buy(t, fixture{gold: 100_000, starCounters: start}.profile(t), &profile.ServerState{StoreRNG: 3},
		priceOf(knight))
	if len(after.Store.StarCounters) != 1 || !maps.Equal(after.Store.StarCounters["champion"], start["champion"]) {
		t.Errorf("a Knight Pack changed the star counters to %v", after.Store.StarCounters)
	}
	champion, _ := c.Pack("champion")
	tutorial := map[string]any{"complete": false, "consume_complete": false, "restore_step": 3}
	scripted := priceOf(champion)
	scripted.Scripted = true
	after, _ = buy(t, fixture{gems: 1000, tutorial: tutorial, starCounters: start}.profile(t),
		&profile.ServerState{StoreRNG: 3}, scripted)
	if !maps.Equal(after.Store.StarCounters["champion"], start["champion"]) {
		t.Errorf("the scripted Champion Pack moved the star counters to %v", after.Store.StarCounters)
	}
}

func TestUploadsCannotMoveTheStarGuaranteeCounters(t *testing.T) {
	c := mustCatalog(t)
	stored := profile.Record{
		Profile:  fixture{starCounters: map[string]map[string]int{"champion": {"2": 4}}}.profile(t),
		Revision: 1,
	}
	forged := fixture{starCounters: map[string]map[string]int{"champion": {"2": 124, "3": 999}, "knight": {"2": 9}}}
	merged, err := MergeUpload(c, stored, &profile.ServerState{}, forged.profile(t), springDay)
	if err != nil {
		t.Fatal(err)
	}
	counters := decode(t, merged).Store.StarCounters
	if len(counters) != 1 || !maps.Equal(counters["champion"], map[string]int{"2": 4}) {
		t.Errorf("an upload set the star counters to %v", counters)
	}
	unset := profile.Record{Profile: fixture{}.profile(t), Revision: 1}
	merged, err = MergeUpload(c, unset, &profile.ServerState{}, forged.profile(t), springDay)
	if err != nil {
		t.Fatal(err)
	}
	if counters := decode(t, merged).Store.StarCounters; counters != nil {
		t.Errorf("an upload added star counters %v the server never wrote", counters)
	}
}

func TestMalformedStarGuaranteeCountersAreRefused(t *testing.T) {
	c := mustCatalog(t)
	champion, _ := c.Pack("champion")
	for _, counters := range []any{
		[]any{1, 2},
		map[string]any{"champion": 5},
		map[string]any{"champion": map[string]any{"2": -1}},
		map[string]any{"champion": map[string]any{"2": "many"}},
		map[string]any{"champion": map[string]any{"1": 3}},
		map[string]any{"champion": map[string]any{"three": 3}},
	} {
		var doc map[string]any
		if err := json.Unmarshal(fixture{gems: 1000}.profile(t), &doc); err != nil {
			t.Fatal(err)
		}
		doc["store"].(map[string]any)[packsSinceStarGuarantee] = counters
		raw, _ := json.Marshal(doc)
		if _, _, err := Purchase(c, raw, &profile.ServerState{}, priceOf(champion), springDay); refusalCode(err) != BadProfile {
			t.Errorf("counters %v: got %v", counters, err)
		}
	}
}

func TestShippedStarGuaranteesWaitLongerThanThePity(t *testing.T) {
	c := mustCatalog(t)
	var guaranteed []string
	for _, pack := range c.Packs {
		if !pack.HasStarGuarantees() {
			continue
		}
		guaranteed = append(guaranteed, pack.ID)
		every := pack.StarGuaranteePacks
		if !(pack.LegendaryPityPacks < every[2] && every[2] < every[3]) {
			t.Errorf("%s: pity %d, star guarantees %v", pack.ID, pack.LegendaryPityPacks, every)
		}
	}
	if len(guaranteed) != 2 || guaranteed[0] != "champion" || guaranteed[1] != "grand_champion" {
		t.Errorf("star guarantees on %v, want the Champion and Grand Champion Packs", guaranteed)
	}
}
