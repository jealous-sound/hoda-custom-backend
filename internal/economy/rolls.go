package economy

import (
	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
)

const (
	xorshiftZeroReplacement = 0x6D2B79F5
	gearMultiplier          = 1664525
	gearIncrement           = 1013904223
	gearOutputShift         = 8
	percent                 = 100
	uniqueHeroRerolls       = 32
	noDuplicateRarity       = catalog.RarityLegendary
	noRoll                  = -1
)

type rng struct {
	state uint32
}

func (r *rng) next() uint32 {
	x := r.state
	if x == 0 {
		x = xorshiftZeroReplacement
	}
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	r.state = x
	return x
}

func (r *rng) below(count int) int {
	if count <= 0 {
		return 0
	}
	return int(r.next() % uint32(count))
}

func (r *rng) nextGear() uint32 {
	r.state = r.state*gearMultiplier + gearIncrement
	return r.state >> gearOutputShift
}

func (r *rng) chance(pct int) bool {
	return r.below(percent) < pct
}

func rollRarity(pack *catalog.Pack, r *rng) catalog.Rarity {
	total := 0
	for rarity := catalog.RarityCommon; rarity <= catalog.RarityLegendary; rarity++ {
		total += pack.Weights[rarity]
	}
	if total <= 0 {
		return pack.MinimumRarity
	}
	roll := r.below(total)
	for rarity := catalog.RarityCommon; rarity <= catalog.RarityLegendary; rarity++ {
		if roll < pack.Weights[rarity] {
			return rarity
		}
		roll -= pack.Weights[rarity]
	}
	return catalog.RarityLegendary
}

func slotRarity(pack *catalog.Pack, rolledSlot int, pity bool, r *rng) catalog.Rarity {
	legendaryEnd := pack.GuaranteedLegendary
	epicEnd := legendaryEnd + pack.GuaranteedEpic
	rareEnd := epicEnd + pack.GuaranteedRare
	switch {
	case rolledSlot < legendaryEnd || pity:
		return catalog.RarityLegendary
	case rolledSlot < epicEnd:
		return catalog.RarityEpic
	case rolledSlot < rareEnd:
		return catalog.RarityRare
	}
	return rollRarity(pack, r)
}

func alreadyRolled(hero int, rolled []int) bool {
	for _, previous := range rolled {
		if previous == hero {
			return true
		}
	}
	return false
}

func sparesDuplicates(pack *catalog.Pack, rarity catalog.Rarity) bool {
	return rarity == noDuplicateRarity && pack.PoolTag == ""
}

func notYetOwned(pool []int, owned map[int]bool, rolled []int) []int {
	fresh := make([]int, 0, len(pool))
	for _, hero := range pool {
		if !owned[hero] && !alreadyRolled(hero, rolled) {
			fresh = append(fresh, hero)
		}
	}
	return fresh
}

func pickHero(
	c *catalog.Catalog, pack *catalog.Pack, rarity catalog.Rarity, owned map[int]bool, rolled []int, r *rng,
) int {
	pool := c.RarityPool(rarity)
	if pack.PoolTag != "" {
		pool = c.TaggedPool(pack.PoolTag, rarity)
	} else if sparesDuplicates(pack, rarity) {
		if fresh := notYetOwned(pool, owned, rolled); len(fresh) > 0 {
			pool = fresh
		}
	}
	if len(pool) == 0 {
		return noRoll
	}
	return pool[r.below(len(pool))]
}

func rollPackHero(
	c *catalog.Catalog, pack *catalog.Pack, rarity catalog.Rarity, owned map[int]bool, rolled []int, r *rng,
) int {
	hero := pickHero(c, pack, rarity, owned, rolled, r)
	for attempt := 0; pack.UniqueHeroes && attempt < uniqueHeroRerolls && alreadyRolled(hero, rolled); attempt++ {
		hero = pickHero(c, pack, rarity, owned, rolled, r)
	}
	return hero
}

type heroRoll struct {
	heroes     []int
	rarities   []catalog.Rarity
	nextPity   int
	tracksPity bool
}

func rollHeroes(
	c *catalog.Catalog, pack *catalog.Pack, sinceLegendary int, owned map[int]bool, r *rng,
) (heroRoll, bool) {
	roll := heroRoll{tracksPity: pack.LegendaryPity > 0, nextPity: sinceLegendary}
	for i := 0; i < pack.HeroCount; i++ {
		var hero int
		var rarity catalog.Rarity
		if i < len(pack.FixedHeroes) {
			index, ok := c.HeroIndex(pack.FixedHeroes[i])
			if !ok {
				return roll, false
			}
			hero, rarity = index, c.Heroes[index].Rarity
		} else {
			pity := roll.tracksPity && roll.nextPity >= pack.LegendaryPity-1
			rarity = slotRarity(pack, i-len(pack.FixedHeroes), pity, r)
			hero = rollPackHero(c, pack, rarity, owned, roll.heroes, r)
		}
		if hero == noRoll {
			return roll, false
		}
		roll.heroes = append(roll.heroes, hero)
		roll.rarities = append(roll.rarities, rarity)
		if roll.tracksPity {
			if rarity == catalog.RarityLegendary {
				roll.nextPity = 0
			} else {
				roll.nextPity++
			}
		}
	}
	return roll, true
}

func sortByRarity(heroes []int, rarities []catalog.Rarity) {
	for i := 1; i < len(heroes); i++ {
		hero, rarity := heroes[i], rarities[i]
		j := i - 1
		for j >= 0 && rarities[j] > rarity {
			heroes[j+1], rarities[j+1] = heroes[j], rarities[j]
			j--
		}
		heroes[j+1], rarities[j+1] = hero, rarity
	}
}

func familyMatches(rune catalog.Rune, families []string) bool {
	if len(families) == 0 {
		return true
	}
	for _, family := range families {
		if rune.Family == family {
			return true
		}
	}
	return false
}

func rollRune(c *catalog.Catalog, slot catalog.RuneSlot, r *rng) int {
	var candidates []int
	for index, rune := range c.Runes {
		if rune.Rollable() && rune.Rarity >= slot.MinRarity && rune.Rarity <= slot.MaxRarity &&
			familyMatches(rune, slot.Families) {
			candidates = append(candidates, index)
		}
	}
	if len(candidates) == 0 {
		return noRoll
	}
	return candidates[r.below(len(candidates))]
}

func rollGearRarity(minRarity, maxRarity int, r *rng) int {
	total := 0
	for rarity := minRarity; rarity <= maxRarity; rarity++ {
		total += 1 << (maxRarity - rarity)
	}
	roll := int(r.nextGear() % uint32(total))
	for rarity := minRarity; rarity <= maxRarity; rarity++ {
		roll -= 1 << (maxRarity - rarity)
		if roll < 0 {
			return rarity
		}
	}
	return maxRarity
}

func rollGear(c *catalog.Catalog, minRarity, maxRarity int, r *rng) int {
	rarity := rollGearRarity(minRarity, maxRarity, r)
	var candidates []int
	for index, item := range c.Gear {
		if item.Rarity == rarity {
			candidates = append(candidates, index)
		}
	}
	if len(candidates) == 0 {
		return noRoll
	}
	return candidates[int(r.nextGear()%uint32(len(candidates)))]
}

func rollTome(pct int, r *rng) bool {
	return pct > 0 && int(r.nextGear()%percent) < pct
}
