package battle

import "testing"

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
		gap := tables.squads[squad].strength - tables.squads[weaker].strength
		if gap < 0 || (gap == 0 && squad < weaker) {
			t.Fatalf("rank %d: squad %d (strength %d) follows squad %d (strength %d)", rank, squad,
				tables.squads[squad].strength, weaker, tables.squads[weaker].strength)
		}
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
