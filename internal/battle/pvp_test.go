package battle

import (
	"slices"
	"testing"
)

func TestPvpSquadsRankFromWeakestToStrongest(t *testing.T) {
	tables := loadTestData(t).pvp
	eventsOnly := 0
	for _, squad := range tables.squads {
		if squad.eventsOnly {
			eventsOnly++
		}
	}
	if len(tables.ladder)+eventsOnly != len(tables.squads) {
		t.Fatalf("%d ladder and %d event squads of %d", len(tables.ladder), eventsOnly, len(tables.squads))
	}
	ranked := make([]bool, len(tables.squads))
	for rank, squad := range tables.ladder {
		if ranked[squad] {
			t.Fatalf("squad %d is ranked twice", squad)
		}
		ranked[squad] = true
		if rank == 0 {
			continue
		}
		weaker := tables.ladder[rank-1]
		elite, afterElite := tables.squads[squad].elite, tables.squads[weaker].elite
		if afterElite && !elite {
			t.Fatalf("rank %d: campaign squad %d follows elite squad %d", rank, squad, weaker)
		}
		gap := tables.squads[squad].strength - tables.squads[weaker].strength
		if elite == afterElite && (gap < 0 || (gap == 0 && squad < weaker)) {
			t.Fatalf("rank %d: squad %d (strength %d) follows squad %d (strength %d)", rank, squad,
				tables.squads[squad].strength, weaker, tables.squads[weaker].strength)
		}
	}
	strongest := tables.ladder[len(tables.ladder)-1]
	if !tables.squads[strongest].elite || tables.squads[tables.ladder[0]].elite {
		t.Fatalf("the ladder does not end on an elite squad")
	}
	lowest, top := tables.botStrengthRank(0), tables.botStrengthRank(len(tables.bots)-1)
	if lowest != 0 || top != len(tables.ladder)-1 {
		t.Fatalf("the ladder spans strength ranks %d to %d of %d squads", lowest, top, len(tables.ladder))
	}
}

func TestPvpOpponentsGrowStrongerUpTheLadder(t *testing.T) {
	d := loadTestData(t)
	const matches, day = 200, 20000
	previous := -1.0
	for _, trophies := range []int{0, 500, 1500, 3000, 6000} {
		total := 0
		for seed := uint32(1); seed <= matches; seed++ {
			total += d.pvp.squads[d.PvpLadderOpponent(30, trophies, day, seed).Squad].strength
		}
		mean := float64(total) / matches
		if mean <= previous {
			t.Fatalf("opponents at %d trophies average strength %.0f, no stronger than %.0f below", trophies, mean,
				previous)
		}
		previous = mean
	}
}

func TestPvpOpponentSquadIgnoresThePlayerLevel(t *testing.T) {
	d := loadTestData(t)
	for seed := uint32(1); seed <= 40; seed++ {
		newcomer := d.PvpLadderOpponent(1, 1500, 20000, seed)
		veteran := d.PvpLadderOpponent(PvpMaxLevel, 1500, 20000, seed)
		if newcomer.Squad != veteran.Squad {
			t.Fatalf("seed %d: squad %d at level 1, squad %d at level %d", seed, newcomer.Squad, veteran.Squad,
				PvpMaxLevel)
		}
	}
}

func TestThePvpTopBotFieldsAnEliteSquadInFullGear(t *testing.T) {
	d := loadTestData(t)
	top := d.PvpBotCount() - 1
	for seed := uint32(1); seed <= 12; seed++ {
		opponent := d.PvpOpponentFromBot(top, 10, 20000, seed)
		roster, err := d.PvpRoster(opponent)
		if err != nil {
			t.Fatal(err)
		}
		if !d.pvp.squads[opponent.Squad].elite || len(roster.Runes) != RuneSlotCount {
			t.Fatalf("seed %d: the top bot fields squad %d with runes %v", seed, opponent.Squad, roster.Runes)
		}
		for _, unit := range roster.Units {
			if !unit.Ascended || len(unit.Gear) != gearSlotCapacity || slices.Contains(unit.Gear, "") {
				t.Fatalf("seed %d: the top bot's %s wears %v (ascended %v)", seed, unit.ID, unit.Gear, unit.Ascended)
			}
		}
		newcomer, err := d.PvpRoster(d.PvpOpponentFromBot(0, 10, 20000, seed))
		if err != nil {
			t.Fatal(err)
		}
		for _, unit := range newcomer.Units {
			if unit.Gear != nil || unit.Ascended {
				t.Fatalf("seed %d: the bottom bot's %s wears %v", seed, unit.ID, unit.Gear)
			}
		}
		if len(newcomer.Runes) != 0 {
			t.Fatalf("seed %d: the bottom bot holds runes %v", seed, newcomer.Runes)
		}
	}
}

func TestPvpLoadoutsMustNameKnownGearAndRunes(t *testing.T) {
	d := loadTestData(t)
	for _, squad := range []pvpSquad{
		{runes: []string{"rune-that-does-not-exist"}},
		{cells: []Unit{{ID: "UNIT_BEAR", Gear: []string{"gear-that-does-not-exist", "", "", "", ""}}}},
	} {
		broken, tables := *d, *d.pvp
		tables.squads = append([]pvpSquad{squad}, d.pvp.squads...)
		broken.pvp = &tables
		if err := broken.checkPvpLoadouts(); err == nil {
			t.Errorf("a bot squad listing %v loaded", squad)
		}
	}
	if err := d.checkPvpLoadouts(); err != nil {
		t.Fatal(err)
	}
}
