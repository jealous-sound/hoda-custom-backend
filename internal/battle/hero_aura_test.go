package battle

import "testing"

func TestHeroAuraJoinsTheHeroAsOneMoreGearSource(t *testing.T) {
	d := loadTestData(t)
	h, err := d.buildHero(Unit{ID: "UNIT_FERELDANKNIGHT", Slot: 1, Tier: "I", Level: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	aura := h.gear[heroAuraSource]
	if aura == nil {
		t.Fatal("the knight has no hero aura")
	}
	for _, effect := range aura.effects {
		if effect.stat == gearStatDefender && effect.value > defenderAbsorbPct {
			return
		}
	}
	t.Errorf("the knight's aura does not absorb more than the %d%% defender default", defenderAbsorbPct)
}

func TestHeroAurasRejectAnUnknownVersion(t *testing.T) {
	if _, err := loadHeroAuras([]byte(`{"version": 2, "items": []}`)); err == nil {
		t.Error("a version 2 table loaded")
	}
}

func TestDefenderAbsorbsTheLargerOfItsAuraAndTheSpecialtyDefault(t *testing.T) {
	defender := &unit{hero: &hero{def: &character{specialties: []specialty{specDefender}}}}
	if got := defender.defenderAbsorbPct(); got != defenderAbsorbPct {
		t.Errorf("unstated defender absorbs %d%%", got)
	}
	defender.gear.defenderPct = 65
	if got := defender.defenderAbsorbPct(); got != 65 {
		t.Errorf("a 65%% defender aura absorbs %d%%", got)
	}
}

func TestAnyPerceptiveSquadmateRevealsStealthHits(t *testing.T) {
	rally := gearTestItem(gearTestEffect(gearStatPower, 10, onSelfHit, scopeSelf))
	const attackerIdx, hitIdx = 2, SlotCount + 2
	gain := func(perceptiveSquadmate bool) int {
		s := &sim{}
		seatGearTestUnit(s, 0, 2, 100000).gear.stealth = true
		seatGearTestUnit(s, 1, 2, 100000, rally)
		seatGearTestUnit(s, 1, 3, 100000).gear.perceptive = perceptiveSquadmate
		before := s.units[hitIdx].power
		s.gearReactToAttack(attackerIdx, []Target{{Unit: hitIdx, HealthDelta: -100}})
		return s.units[hitIdx].power - before
	}
	if got := gain(false); got != 0 {
		t.Errorf("a stealth hit gave %d power", got)
	}
	if got := gain(true); got != 100 {
		t.Errorf("with a perceptive squadmate the hit gave %d power", got)
	}
}

func TestPvpTrimCutsOnlyTheHeroAuraByItsPassiveShare(t *testing.T) {
	effect := &gearEffect{stat: gearStatDefender, value: 65}
	trimmed := &unit{passiveCutPct: 40}
	if got := trimmed.gearEffectValue(heroAuraSource, effect); got != 39 {
		t.Errorf("a 40%% trim leaves a 65%% aura at %d%%", got)
	}
	if got := trimmed.gearEffectValue(0, effect); got != 65 {
		t.Errorf("a 40%% trim cut worn gear to %d%%", got)
	}
	if got := (&unit{}).gearEffectValue(heroAuraSource, effect); got != 65 {
		t.Errorf("an untrimmed aura gives %d%%", got)
	}
}
