package battle

import (
	"math"
	"slices"
	"testing"
)

func seatAuraHero(t *testing.T, s *sim, side, slot int, id string) *unit {
	t.Helper()
	h, err := loadTestData(t).buildHero(Unit{ID: id, Slot: slot, Tier: "I", Level: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	s.initUnit(side, slot, h)
	return s.unitAt(side, slot)
}

func seatGroupedTestUnit(s *sim, side, slot int, groups ...string) *unit {
	u := seatGearTestUnit(s, side, slot, 100000)
	u.hero.def.groups = groups
	return u
}

func TestImmunitiesOutlastTrimsAndEnemyResistCuts(t *testing.T) {
	if resistOrImmune(true, -35) != immuneResistPct || resistOrImmune(true, 150) != 150 ||
		resistOrImmune(false, -35) != -35 {
		t.Fatal("an immunity does not floor the resist at 100%")
	}
	const pvpTrimCutPct = 28
	brittle := gearTestItem(gearTestEffect(gearStatStunResist, -50, onBattle, scopeEnemies),
		gearTestEffect(gearStatSlowResist, -50, onBattle, scopeEnemies))
	landed := func(immune bool) int {
		s := &sim{rng: 9}
		seatGearTestUnit(s, 0, 2, 100000, brittle)
		fen := seatAuraHero(t, s, 1, 2, "UNIT_FEN'HAREL")
		fen.passiveCutPct = pvpTrimCutPct
		s.battleStartPassives()
		if !fen.gear.stunImmune || !fen.gear.slowImmune || fen.gear.stunResistPct >= 0 {
			t.Fatalf("Fen'Harel's aura gave %+v", fen.gear)
		}
		fen.gear.stunImmune, fen.gear.slowImmune = immune, immune
		if !immune {
			cut := 100*(100-pvpTrimCutPct)/100 - 50
			fen.gear.stunResistPct, fen.gear.slowResistPct = cut, cut
		}
		hit := &attackRecipe{target: recipeTargetOne, stunPct: 100, slowPct: 100, slowAmt: -50, statusOnly: true}
		count := 0
		for range 50 {
			s.attack(2, &abilityRecipe{}, hit, true, false)
			if fen.stunned {
				count++
			}
			if fen.curInitiative != 0 {
				count++
			}
			fen.stunned, fen.curInitiative = false, 0
		}
		return count
	}
	if got := landed(true); got != 0 {
		t.Errorf("a trimmed, debuffed Fen'Harel was stunned or slowed %d times", got)
	}
	if landed(false) == 0 {
		t.Error("a 72% resist cut by -50% never let a stun or slow land")
	}
}

func TestLessLikelyTargetsHalveTheirWeight(t *testing.T) {
	s := &sim{}
	danyla := seatAuraHero(t, s, 0, 2, "UNIT_DANYLA")
	elf := seatGroupedTestUnit(s, 1, 1, "Elves")
	human := seatGroupedTestUnit(s, 1, 3)
	s.battleStartPassives()
	if got, want := s.targetWeight(danyla, elf), mul(s.targetWeight(danyla, human), gearAvoidTargetWeight); got != want {
		t.Errorf("Danyla weighs an elf at %v, want %v", got, want)
	}
}

func TestSquadTargetPreferencesReachEveryMatchingAlly(t *testing.T) {
	s := &sim{}
	seatAuraHero(t, s, 0, 0, "UNIT_GATEKEEPER")
	werewolf := seatGroupedTestUnit(s, 0, 1, "Werewolves")
	outsider := seatGroupedTestUnit(s, 0, 3)
	human := seatGroupedTestUnit(s, 1, 1, "Human")
	plain := seatGroupedTestUnit(s, 1, 3)
	s.battleStartPassives()
	if got, want := s.targetWeight(werewolf, human), mul(s.targetWeight(werewolf, plain), gearTargetWeight); got != want {
		t.Errorf("a werewolf weighs a human at %v, want %v", got, want)
	}
	if s.targetWeight(outsider, human) != s.targetWeight(outsider, plain) {
		t.Error("a non-werewolf ally prefers humans too")
	}
}

func TestRowPreferencesFollowTheAura(t *testing.T) {
	s := &sim{}
	varric := seatAuraHero(t, s, 0, 1, "UNIT_ARTIFICERVARRIC")
	carver := seatAuraHero(t, s, 0, 3, "UNIT_CARVERHAWKE")
	flanker := *carver.hero.def
	flanker.specialties = append(slices.Clone(flanker.specialties), specFlank)
	carver.hero.def = &flanker
	front := seatGroupedTestUnit(s, 1, 1)
	back := seatGroupedTestUnit(s, 1, 2)
	s.battleStartPassives()
	if rowWeight(varric, front) != gearTargetWeight || rowWeight(varric, back) != 1 {
		t.Errorf("Varric weighs the front row at %v and the back at %v", rowWeight(varric, front), rowWeight(varric, back))
	}
	if rowWeight(carver, front) != 1 || rowWeight(carver, back) != 1 {
		t.Errorf("flanking Carver weighs the rows at %v and %v", rowWeight(carver, front), rowWeight(carver, back))
	}
}

func TestPerAllyAurasCountTheirSquadmates(t *testing.T) {
	gearTotals := func(id, squadmate string, mates int) (gearState, float32) {
		s := &sim{}
		holder := seatAuraHero(t, s, 0, 2, id)
		for i := range mates {
			mate := seatGroupedTestUnit(s, 0, []int{1, 3}[i], "Nug")
			mate.hero.def.id = squadmate
		}
		var healthPct, powerPct [unitCount]float32
		s.gearAuras(&healthPct, &powerPct)
		return holder.gear, powerPct[2]
	}
	for mates := range 3 {
		if mistral, _ := gearTotals("UNIT_GREATERMISTRALDRAGONLING", "UNIT_DRAGONLING", mates); mistral.damageResistPct != 20*(1+mates) {
			t.Errorf("Greater Mistral with %d dragonlings resists %d%%", mates, mistral.damageResistPct)
		}
		if guard, _ := gearTotals("UNIT_DIVINEGUARD", "UNIT_DIVINELELIANA", mates); guard.damageResistPct != 10*mates {
			t.Errorf("Divine Guard with %d Divine allies resists %d%%", mates, guard.damageResistPct)
		}
		if _, power := gearTotals("UNIT_NUGKING", "UNIT_NUG", mates); power != float32(25*(1+mates)) {
			t.Errorf("Nug King with %d Nugs gains %v%% power", mates, power)
		}
	}
}

func TestTriggerFiltersCheckWhoSetItOff(t *testing.T) {
	rally := gearTestEffect(gearStatPower, 10, onAllyAttack, scopeAllies)
	rally.by = filter{hasMatch: true, terms: []matchTerm{{kind: termGroup, value: "Inquisition"}}}
	s := &sim{}
	seatGearTestUnit(s, 0, 1, 100000, gearTestItem(rally))
	seatGroupedTestUnit(s, 0, 2, "Inquisition")
	seatGroupedTestUnit(s, 0, 3)
	seatGroupedTestUnit(s, 1, 2)
	hit := []Target{{Unit: SlotCount + 2, HealthDelta: -100}}
	powers := func() [3]int { return [3]int{s.units[1].power, s.units[2].power, s.units[3].power} }
	s.gearReactToAttack(3, hit)
	if got := powers(); got != [3]int{1000, 1000, 1000} {
		t.Errorf("an attack by a non-Inquisition ally gave power: %v", got)
	}
	s.gearReactToAttack(2, hit)
	if got := powers(); got != [3]int{1100, 1100, 1100} {
		t.Errorf("an attack by an Inquisition ally left power at %v", got)
	}
}

func TestOpposingFactionBonusFollowsFactionAdvantage(t *testing.T) {
	s := &sim{}
	cassandra := seatAuraHero(t, s, 0, 2, "UNIT_LADYSEEKERCASSANDRA")
	red := seatGroupedTestUnit(s, 1, 1)
	red.faction = maskRed
	blue := seatGroupedTestUnit(s, 1, 3)
	blue.faction = maskBlue
	if cassandra.faction != maskWhite {
		t.Fatalf("Cassandra's faction mask is %d", cassandra.faction)
	}
	if got := s.gearVersusStat(cassandra, red, gearStatBonusDamage); got != 35 {
		t.Errorf("Cassandra gets %d%% bonus damage vs the red faction she beats", got)
	}
	if got := s.gearVersusStat(cassandra, blue, gearStatBonusDamage); got != 0 {
		t.Errorf("Cassandra gets %d%% bonus damage vs blue", got)
	}
}

func TestAssassinsPreferTheirStatedPrey(t *testing.T) {
	s := &sim{rng: 5}
	dowager := seatAuraHero(t, s, 0, 2, "UNIT_THEDOWAGER")
	plainAssassin := seatGroupedTestUnit(s, 0, 3)
	seatGroupedTestUnit(s, 1, 1, "Nobility")
	seatGroupedTestUnit(s, 1, 3)
	s.battleStartPassives()
	choices := []int{SlotCount + 1, SlotCount + 3}
	const trials = 4000
	dowagerNobles, plainNobles := 0, 0
	for range trials {
		if s.pickAssassinationVictim(dowager, choices) == choices[0] {
			dowagerNobles++
		}
		if s.pickAssassinationVictim(plainAssassin, choices) == choices[0] {
			plainNobles++
		}
	}
	preferred := float64(gearAssassinateMatchWeight / (gearAssassinateMatchWeight + 1))
	if share := float64(dowagerNobles) / trials; math.Abs(share-preferred) > 0.05 {
		t.Errorf("the Dowager picked Nobility %.2f of the time, want %.2f", share, preferred)
	}
	if share := float64(plainNobles) / trials; math.Abs(share-0.5) > 0.05 {
		t.Errorf("a plain assassin picked Nobility %.2f of the time", share)
	}
}
