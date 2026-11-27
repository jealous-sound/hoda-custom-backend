package catalog

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustDefault(t *testing.T) *Catalog {
	t.Helper()
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestEmbeddedCatalogLoads(t *testing.T) {
	c := mustDefault(t)
	for _, id := range []string{"recruit", "champion", "speed_kills", "peasant", "tutorial_consume"} {
		if _, ok := c.Pack(id); !ok {
			t.Errorf("pack %s missing", id)
		}
	}
	champion, _ := c.Pack("champion")
	if champion.Paid == nil || champion.Paid.Currency != CurrencyGems || champion.Paid.Price != 30 {
		t.Errorf("champion paid offer = %+v", champion.Paid)
	}
	if champion.Voucher < 0 || c.Vouchers[champion.Voucher] != "champion" {
		t.Errorf("champion voucher = %d", champion.Voucher)
	}
	if c.RotationDays == 0 {
		t.Error("no rotation period")
	}
}

func TestDefaultPoolSkipsWispsAndBanners(t *testing.T) {
	c := mustDefault(t)
	for rarity := RarityCommon; rarity <= RarityLegendary; rarity++ {
		for _, index := range c.RarityPool(rarity) {
			hero := c.Heroes[index]
			if !hero.Obtainable || hero.Rarity != rarity || bytes.Contains([]byte(hero.ID), []byte("WISP")) ||
				bytes.Contains([]byte(hero.ID), []byte("BANNER")) {
				t.Fatalf("%s should not be in the %d pool", hero.ID, rarity)
			}
		}
	}
	if len(c.TaggedPool("WISP", RarityCommon))+len(c.TaggedPool("WISP", RarityUncommon)) == 0 {
		t.Error("no wisps for the wisp packs")
	}
}

func TestSchedulesUseUTCAndWrapTheYear(t *testing.T) {
	c := mustDefault(t)
	wintersend, _ := c.Pack("wintersend")
	for _, day := range []string{"2026-12-10", "2026-12-31", "2027-01-15"} {
		at, _ := time.Parse("2006-01-02", day)
		if !c.PackOnSale(wintersend, at) {
			t.Errorf("wintersend should be on sale on %s", day)
		}
	}
	off, _ := time.Parse("2006-01-02", "2027-01-16")
	if c.PackOnSale(wintersend, off) {
		t.Error("wintersend should be off sale on 2027-01-16")
	}
}

func TestFlashPacksRotateOnePerPeriod(t *testing.T) {
	c := mustDefault(t)
	at := time.Unix(1_800_000_000, 0)
	onSale := map[string]int{}
	for i := range c.Packs {
		if rotation := c.Packs[i].Rotation; rotation != nil && c.PackOnSale(&c.Packs[i], at) {
			onSale[rotation.Group]++
		}
	}
	for _, group := range []string{"flash", "booster"} {
		if onSale[group] != 1 {
			t.Errorf("%d %s packs on sale, want 1", onSale[group], group)
		}
	}
}

var clientDataSources = map[string]string{
	"characters.json":     filepath.Join("client-additional-data", "characters.json"),
	"battle_recipes.json": filepath.Join("tests", "golden", "battle_recipes.json"),
}

func clientSource(name string) string {
	if source, ok := clientDataSources[name]; ok {
		return source
	}
	return filepath.Join("data", name)
}

func TestEmbeddedDataMatchesClientCheckout(t *testing.T) {
	client := filepath.Join("..", "..", "..", "hoda-custom-client")
	if dir := os.Getenv("HOD_CLIENT_DIR"); dir != "" {
		client = dir
	}
	if _, err := os.Stat(filepath.Join(client, "data")); err != nil {
		t.Skipf("no client checkout at %s", client)
	}
	entries, err := embedded.ReadDir("data")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		ours, _ := embedded.ReadFile("data/" + entry.Name())
		theirs, err := os.ReadFile(filepath.Join(client, clientSource(entry.Name())))
		if err != nil {
			t.Errorf("%s: %v", entry.Name(), err)
			continue
		}
		if !bytes.Equal(ours, theirs) {
			t.Errorf("%s differs from the client; run scripts/sync-client-data.sh", entry.Name())
		}
	}
}

func TestOnslaughtReplacesEveryFewPvpEvents(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	events := &c.Pvp.Events
	guildCycles := 0
	for cycle := int64(0); cycle < events.GuildEveryCycles*int64(len(events.GuildCatalog)); cycle++ {
		details := events.ForCycle(cycle)
		if events.IsGuildCycle(cycle) {
			guildCycles++
			if len(details.GuildRankRewards) == 0 || details.GuildMinimumBanners <= 0 {
				t.Fatalf("Onslaught cycle %d has no guild rewards", cycle)
			}
			if reward, found := details.GuildReward(1); !found || reward.Gems <= 0 {
				t.Fatalf("Onslaught cycle %d pays nothing for first place", cycle)
			}
		} else if len(details.GuildRankRewards) != 0 {
			t.Fatalf("ordinary cycle %d has guild rewards", cycle)
		}
	}
	if guildCycles != len(events.GuildCatalog) {
		t.Fatalf("%d Onslaughts, want %d", guildCycles, len(events.GuildCatalog))
	}
}

func TestEventRanksBelowTheRewardTablePayItsLastTier(t *testing.T) {
	details := PvpEventDetails{RankRewards: []PvpRankReward{
		{First: 1, Last: 1, PvpReward: PvpReward{Gold: 500}},
		{First: 2, Last: 10, PvpReward: PvpReward{Gold: 200}},
	}}
	for rank, want := range map[int]int{1: 500, 2: 200, 10: 200, 11: 200, 4000: 200} {
		if reward, rewarded := details.RankReward(rank); !rewarded || reward.Gold != want {
			t.Errorf("rank %d pays %d gold (rewarded %v), want %d", rank, reward.Gold, rewarded, want)
		}
	}
	if _, rewarded := details.RankReward(0); rewarded {
		t.Error("rank 0 is rewarded")
	}
	if _, rewarded := (&PvpEventDetails{}).RankReward(1); rewarded {
		t.Error("an event without rank rewards pays one")
	}
}

func TestARetieredHeroMeetsRequirementsAtTheRarityItIsShownAt(t *testing.T) {
	c := mustDefault(t)
	const drawnLegendaryPlayedAsRare = "UNIT_SISTERNIGHTINGALE"
	hero, ok := c.Hero(drawnLegendaryPlayedAsRare)
	if !ok {
		t.Fatalf("%s is not in the hero pool", drawnLegendaryPlayedAsRare)
	}
	if hero.Rarity != RarityRare || hero.OriginalRarity != RarityLegendary {
		t.Fatalf("%s plays at %d and is shown at %d", hero.ID, hero.Rarity, hero.OriginalRarity)
	}
	epicOrBetter := ExpeditionTrait{Rarity: RarityEpic}
	if !epicOrBetter.Matches(hero, hero.OriginalRarity) {
		t.Fatalf("%s shown as Legendary fails an Epic-or-better slot", hero.ID)
	}
	for _, hero := range c.Heroes {
		if hero.OriginalRarity < hero.Rarity {
			t.Fatalf("%s is shown below the rarity it plays at", hero.ID)
		}
	}
}

func legendaryRawPack(starWeights map[string]int) rawPack {
	return rawPack{
		ID: "stars", HeroCount: 1, MinimumRarity: "Legendary", LegendaryStarWeights: starWeights,
		Weights: map[string]int{"Legendary": HeroWeightTotal},
	}
}

func TestOpenLegendaryPacksNeedWholeStarWeights(t *testing.T) {
	c := mustDefault(t)
	valid := map[string]int{"1": 2000, "2": 7000, "3": 1000}
	if err := c.parseHeroContents(&Pack{}, legendaryRawPack(valid)); err != nil {
		t.Fatalf("whole star weights refused: %v", err)
	}
	for name, weights := range map[string]map[string]int{
		"missing":      nil,
		"short":        {"1": 2000, "2": 7000},
		"unknown star": {"1": 2000, "2": 7000, "4": 1000},
		"negative":     {"1": 11000, "2": -1000},
	} {
		if err := c.parseHeroContents(&Pack{}, legendaryRawPack(weights)); err == nil {
			t.Errorf("%s star weights were accepted", name)
		}
	}
	tagged := legendaryRawPack(valid)
	tagged.PoolTag = "WISP"
	if err := c.parseHeroContents(&Pack{}, tagged); err == nil {
		t.Error("a tagged pack took star weights it never rolls")
	}
}

func TestStarWeightsNeedALegendaryOfThatStar(t *testing.T) {
	c := mustDefault(t)
	twoStarsOnly := *c
	twoStarsOnly.rarityPool[RarityLegendary] = nil
	for _, index := range c.rarityPool[RarityLegendary] {
		if c.Heroes[index].Stars == 2 {
			twoStarsOnly.rarityPool[RarityLegendary] = append(twoStarsOnly.rarityPool[RarityLegendary], index)
		}
	}
	if err := twoStarsOnly.parseHeroContents(&Pack{}, legendaryRawPack(map[string]int{"2": 10000})); err != nil {
		t.Fatalf("a two-star-only pack was refused: %v", err)
	}
	if err := twoStarsOnly.parseHeroContents(&Pack{}, legendaryRawPack(map[string]int{"1": 1, "2": 9999})); err == nil {
		t.Error("a weight on a star tier with no heroes was accepted")
	}
}

func pityRawPack(pity int, starWeights, guarantees map[string]int) rawPack {
	raw := legendaryRawPack(starWeights)
	raw.MinimumRarity = "Rare"
	raw.Weights = map[string]int{"Rare": HeroWeightTotal - 100, "Legendary": 100}
	raw.LegendaryPityPacks = pity
	raw.LegendaryStarGuarantees = guarantees
	return raw
}

func TestStarGuaranteesNeedAPitySlotAndATierToRoll(t *testing.T) {
	c := mustDefault(t)
	standard := map[string]int{"1": 2000, "2": 7000, "3": 1000}
	pack := &Pack{}
	if err := c.parseHeroContents(pack, pityRawPack(100, standard, map[string]int{"2": 125, "3": 1000})); err != nil {
		t.Fatalf("valid star guarantees refused: %v", err)
	}
	if pack.StarGuaranteePacks != [LegendaryStarMax + 1]int{0, 0, 125, 1000} || !pack.HasStarGuarantees() {
		t.Errorf("parsed star guarantees %v", pack.StarGuaranteePacks)
	}
	for name, raw := range map[string]rawPack{
		"one-star":            pityRawPack(100, standard, map[string]int{"1": 50}),
		"four-star":           pityRawPack(100, standard, map[string]int{"4": 50}),
		"unnamed":             pityRawPack(100, standard, map[string]int{"two": 50}),
		"negative":            pityRawPack(100, standard, map[string]int{"2": -1}),
		"without a pity":      pityRawPack(0, standard, map[string]int{"2": 125}),
		"on a missing weight": pityRawPack(100, map[string]int{"1": 2500, "2": 7500, "3": 0}, map[string]int{"3": 1000}),
	} {
		if err := c.parseHeroContents(&Pack{}, raw); err == nil {
			t.Errorf("a star guarantee %s was accepted", name)
		}
	}
}

func TestNexusRollsHeroesAtTheRarityTheyAreShownAtButNeverALegendary(t *testing.T) {
	c := mustDefault(t)
	const drawnLegendaryPlayedAsRare = "UNIT_SISTERNIGHTINGALE"
	drawnLegendaryRewarded := false
	for shown := RarityCommon; shown <= RarityMythic; shown++ {
		for _, index := range c.NexusRewardPool(shown) {
			hero := c.Heroes[index]
			if hero.OriginalRarity != shown || hero.Rarity >= RarityLegendary || !heroInDefaultPool(hero) {
				t.Fatalf("%s plays at %d and is shown at %d, yet the Nexus hands it out at %d", hero.ID, hero.Rarity,
					hero.OriginalRarity, shown)
			}
			drawnLegendaryRewarded = drawnLegendaryRewarded ||
				(hero.ID == drawnLegendaryPlayedAsRare && shown == RarityLegendary)
		}
	}
	if !drawnLegendaryRewarded {
		t.Errorf("a Nexus Legendary roll never hands out %s", drawnLegendaryPlayedAsRare)
	}

	decode := func(rarities string) []rawNexusEvent {
		var events []rawNexusEvent
		raw := `[{"id":"Test","difficulties":[{"id":"Easy_Test","nodes":[{"reward_rarity":` + rarities + `}]}]}]`
		if err := json.Unmarshal([]byte(raw), &events); err != nil {
			t.Fatal(err)
		}
		return events
	}
	fresh := *c
	fresh.Nexus = nil
	if err := fresh.parseNexus(decode("[1, 5]")); err != nil {
		t.Errorf("a Nexus node rolling Common and Legendary: %v", err)
	}
	if err := fresh.parseNexus(decode("[1, 6]")); err == nil || !strings.Contains(err.Error(), "rarity 6") {
		t.Errorf("a Nexus node rolling Mythic loaded with %v", err)
	}
}
