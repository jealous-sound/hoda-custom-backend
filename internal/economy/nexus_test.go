package economy

import (
	"testing"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

func TestANexusAwardHandsOutHeroesAtTheRarityTheyAreShownAt(t *testing.T) {
	c := mustCatalog(t)
	escrowed := []catalog.Rarity{
		catalog.RarityCommon, catalog.RarityEpic, catalog.RarityLegendary, catalog.RarityLegendary,
		catalog.RarityLegendary,
	}
	raw := gameProfile(t, func(doc map[string]any) {
		run := section(doc, "nexus", "run")
		run["committed"] = true
		units := make([]any, catalog.RarityMythic+1)
		for rarity := range units {
			units[rarity] = 0
		}
		for _, rarity := range escrowed {
			units[rarity] = units[rarity].(int) + 1
		}
		run["escrow_units"] = units
	})
	for seed := uint32(1); seed <= 20; seed++ {
		server := &profile.ServerState{StoreRNG: seed}
		_, result, err := ActWith(c, raw, server, ActionNexusAward, nil, ruleNow, Env{})
		if err != nil {
			t.Fatalf("seed %d: nexus_award: %v", seed, err)
		}
		if len(result.Granted.Heroes) != len(escrowed) {
			t.Fatalf("seed %d: granted %v, want %d heroes", seed, result.Granted.Heroes, len(escrowed))
		}
		for n, granted := range result.Granted.Heroes {
			hero, _ := c.Hero(granted.ID)
			if hero.OriginalRarity != escrowed[n] || hero.Rarity >= catalog.RarityLegendary {
				t.Errorf("seed %d: %s plays at %d and is shown at %d, want shown at %d below Legendary", seed,
					hero.ID, hero.Rarity, hero.OriginalRarity, escrowed[n])
			}
		}
	}
}
