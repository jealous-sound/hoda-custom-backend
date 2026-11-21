package economy

import (
	"testing"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
)

const legendSeeds = 40

type starWeights = [catalog.LegendaryStarMax + 1]int

func legendsOfStars(c *catalog.Catalog, stars int) []int {
	var legends []int
	for _, hero := range c.RarityPool(catalog.RarityLegendary) {
		if c.Heroes[hero].Stars == stars {
			legends = append(legends, hero)
		}
	}
	return legends
}

func ownAll(owned map[int]bool, heroes []int) {
	for _, hero := range heroes {
		owned[hero] = true
	}
}

func starPack(weights starWeights, acrossStars bool) *catalog.Pack {
	pack := &catalog.Pack{HeroCount: 1, LegendaryStarWeights: weights, LegendaryNoDupeAcrossStars: acrossStars}
	pack.Weights[catalog.RarityLegendary] = catalog.HeroWeightTotal
	return pack
}

func rollLegend(
	t *testing.T, c *catalog.Catalog, pack *catalog.Pack, owned map[int]bool, rolled []int, seed uint32,
) int {
	t.Helper()
	r := rng{state: seed}
	hero := pickHero(c, pack, anyStars(catalog.RarityLegendary), owned, rolled, &r)
	if hero == noRoll || c.Heroes[hero].Rarity != catalog.RarityLegendary {
		t.Fatalf("seed %d rolled %d, want a Legendary", seed, hero)
	}
	return hero
}

func TestLegendaryStarWeightsChooseTheStarTier(t *testing.T) {
	c := mustCatalog(t)
	for stars := 1; stars <= catalog.LegendaryStarMax; stars++ {
		var weights starWeights
		weights[stars] = catalog.HeroWeightTotal
		pack := starPack(weights, false)
		for seed := uint32(1); seed <= legendSeeds; seed++ {
			if got := c.Heroes[rollLegend(t, c, pack, map[int]bool{}, nil, seed)].Stars; got != stars {
				t.Fatalf("seed %d rolled a %d-star Legendary from a %d-star-only pack", seed, got, stars)
			}
		}
	}
}

func TestLegendaryNoDupeHoldsWithinTheRolledStarTier(t *testing.T) {
	c := mustCatalog(t)
	threeStars := legendsOfStars(c, 3)
	if len(threeStars) < 2 {
		t.Fatalf("only %d three-star Legendaries in the pool", len(threeStars))
	}
	missing := threeStars[len(threeStars)-1]
	owned := map[int]bool{}
	ownAll(owned, legendsOfStars(c, 2))
	ownAll(owned, threeStars[:len(threeStars)-1])
	twoStarPack := starPack(starWeights{0, 0, catalog.HeroWeightTotal, 0}, false)
	threeStarPack := starPack(starWeights{0, 0, 0, catalog.HeroWeightTotal}, false)
	for seed := uint32(1); seed <= legendSeeds; seed++ {
		if stars := c.Heroes[rollLegend(t, c, twoStarPack, owned, nil, seed)].Stars; stars != 2 {
			t.Fatalf("seed %d: a missing three-star turned a two-star roll into a %d-star", seed, stars)
		}
		if hero := rollLegend(t, c, threeStarPack, owned, nil, seed); hero != missing {
			t.Fatalf("seed %d rolled %d, want the one three-star not owned (%d)", seed, hero, missing)
		}
	}
	repeated := false
	for seed := uint32(1); seed <= legendSeeds; seed++ {
		hero := rollLegend(t, c, threeStarPack, owned, []int{missing}, seed)
		if c.Heroes[hero].Stars != 3 {
			t.Fatalf("seed %d left the three-star tier once every three-star was owned", seed)
		}
		repeated = repeated || hero != missing
	}
	if !repeated {
		t.Error("with every three-star owned or rolled, the tier never repeated an owned hero")
	}
}

func TestAcrossStarNoDupeFindsTheLastMissingLegendary(t *testing.T) {
	c := mustCatalog(t)
	threeStars := legendsOfStars(c, 3)
	missing := threeStars[len(threeStars)-1]
	owned := map[int]bool{}
	ownAll(owned, legendsOfStars(c, 1))
	ownAll(owned, legendsOfStars(c, 2))
	ownAll(owned, threeStars[:len(threeStars)-1])
	weights := starWeights{0, catalog.HeroWeightTotal - 1, 0, 1}
	bloodstone := starPack(weights, true)
	perStar := starPack(weights, false)
	for seed := uint32(1); seed <= legendSeeds; seed++ {
		if hero := rollLegend(t, c, bloodstone, owned, nil, seed); hero != missing {
			t.Fatalf("seed %d rolled %d, want the last Legendary not owned (%d)", seed, hero, missing)
		}
		if stars := c.Heroes[rollLegend(t, c, perStar, owned, nil, seed)].Stars; stars != 1 {
			t.Fatalf("seed %d: a per-star pack rolled a %d-star off a one-star weight", seed, stars)
		}
		if stars := c.Heroes[rollLegend(t, c, bloodstone, owned, []int{missing}, seed)].Stars; stars != 1 {
			t.Fatalf("seed %d: with nothing left to find, Bloodstone ignored its star weights (%d-star)", seed, stars)
		}
	}
}

func TestIronPityLegendariesStopAtTwoStars(t *testing.T) {
	c := mustCatalog(t)
	iron, ok := c.Pack("iron")
	if !ok || iron.LegendaryPityPacks == 0 {
		t.Fatal("no iron pack with a Legendary pity")
	}
	for seed := uint32(1); seed <= legendSeeds; seed++ {
		r := rng{state: seed}
		roll, ok := rollHeroes(c, iron, packDue{sinceLegendary: iron.LegendaryPityPacks - 1}, map[int]bool{}, &r)
		if !ok || roll.rarities[0] != catalog.RarityLegendary {
			t.Fatalf("seed %d: the due iron pack held %v", seed, roll.rarities)
		}
		if stars := c.Heroes[roll.heroes[0]].Stars; stars > 2 {
			t.Fatalf("seed %d: iron dropped a %d-star Legendary", seed, stars)
		}
	}
}

func TestShippedPacksMakeThreeStarLegendariesTheRarest(t *testing.T) {
	c := mustCatalog(t)
	starred := 0
	for i := range c.Packs {
		pack := &c.Packs[i]
		if pack.Kind != catalog.KindHero || !pack.RollsOpenLegendary() {
			continue
		}
		starred++
		weights := pack.LegendaryStarWeights
		if weights[3] >= weights[1] || weights[3] >= weights[2] {
			t.Errorf("%s weighs three-stars at %d against %v", pack.ID, weights[3], weights)
		}
		if pack.LegendaryNoDupeAcrossStars != (pack.ID == "bloodstone") {
			t.Errorf("%s: no-dupe across stars = %v, want it on Bloodstone only", pack.ID, pack.LegendaryNoDupeAcrossStars)
		}
	}
	if iron, _ := c.Pack("iron"); iron.LegendaryStarWeights[3] != 0 {
		t.Error("iron can drop three-star Legendaries")
	}
	if starred == 0 {
		t.Error("no shipped pack rolls open Legendaries")
	}
}
