package battle

import "testing"

func buildPlayerHero(t *testing.T, d *Data, u Unit) *hero {
	t.Helper()
	h, err := d.buildHero(u, true)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func bonusFor(t *testing.T, parent Unit, child FortifyChild) fortifyBonus {
	t.Helper()
	d := loadTestData(t)
	h := buildPlayerHero(t, d, parent)
	bonus, err := d.fortifyBonus(h, d.consume[h.def.dataID()].archetype, child)
	if err != nil {
		t.Fatal(err)
	}
	return bonus
}

func TestFortifyBonusMatchesTheClientFormula(t *testing.T) {
	cases := []struct {
		name   string
		parent Unit
		child  FortifyChild
		want   fortifyBonus
	}{
		{
			"uncommon child into a health slot",
			Unit{Slot: 0, ID: "UNIT_ELVENMAGE", Tier: "IV", Level: 1},
			FortifyChild{Slot: 0, ID: "UNIT_REDIRONMAGE", Tier: "II"},
			fortifyBonus{power: false, rarity: 68, faction: 0, tier: 7},
		},
		{
			"same-faction child rounds the faction share half to even",
			Unit{Slot: 0, ID: "UNIT_ELVENMAGE", Tier: "III", Level: 1},
			FortifyChild{Slot: 1, ID: "UNIT_ELVENMAGE", Tier: "II"},
			fortifyBonus{power: true, rarity: 18, faction: 2, tier: 2},
		},
		{
			"tier I child adds no tier bonus",
			Unit{Slot: 0, ID: "UNIT_APOSTATEWANDERER", Tier: "III", Level: 1},
			FortifyChild{Slot: 3, ID: "UNIT_CASSANDRAPENTAGHAST", Tier: "I"},
			fortifyBonus{power: true, rarity: 49, faction: 0, tier: 0},
		},
		{
			"ascended giant parent counts five more levels and shares with a giant",
			Unit{Slot: GiantSlot, ID: "UNIT_INFERNOGOLEM", Tier: "IV", Level: 1, Ascended: true},
			FortifyChild{Slot: 2, ID: "UNIT_OGRE", Tier: "III"},
			fortifyBonus{power: false, rarity: 222, faction: 33, tier: 44},
		},
	}
	for _, c := range cases {
		if got := bonusFor(t, c.parent, c.child); got != c.want {
			t.Errorf("%s: bonus = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestAnAscendedFortifyChildCountsAtItsOwnRarity(t *testing.T) {
	parent := Unit{Slot: 0, ID: "UNIT_ELVENMAGE", Tier: "II", Level: 1}
	plain := FortifyChild{Slot: 1, ID: "UNIT_ELVENMAGE", Tier: "IV"}
	ascended := plain
	ascended.Ascended = true
	want := fortifyBonus{power: true, rarity: 16, faction: 3, tier: 5}
	if got := bonusFor(t, parent, ascended); got != want || got != bonusFor(t, parent, plain) {
		t.Errorf("ascended child bonus = %+v, want %+v like the plain child", got, want)
	}
}

func TestFortifyAddsToTheStartingStatsBeforeTheFrontRowBonus(t *testing.T) {
	d := loadTestData(t)
	healthChild := FortifyChild{Slot: 0, ID: "UNIT_REDIRONMAGE", Tier: "II"}
	powerChild := FortifyChild{Slot: 3, ID: "UNIT_CASSANDRAPENTAGHAST", Tier: "I"}
	parent := Unit{ID: "UNIT_ELVENMAGE", Tier: "IV", Level: 12}
	power := bonusFor(t, parent, powerChild).total()
	for _, slot := range []int{0, 1, 2, 3} {
		parent.Slot = slot
		plain := buildPlayerHero(t, d, parent)
		fortified := parent
		fortified.Fortify = []FortifyChild{healthChild, powerChild}
		s := &sim{}
		s.initUnit(0, slot, buildPlayerHero(t, d, fortified))
		boosted := s.unitAt(0, slot)
		if boosted.power != plain.statPower()+power || boosted.startPower != boosted.power {
			t.Errorf("slot %d: power %d, want %d", slot, boosted.power, plain.statPower()+power)
		}
		wantHealth := plain.statHealth() + 75
		if slotIsFront(slot) {
			wantHealth = (wantHealth*3 + 1) / 2
		}
		if boosted.maxHealth != wantHealth || boosted.health != wantHealth {
			t.Errorf("slot %d: health %d, want %d", slot, boosted.maxHealth, wantHealth)
		}
	}
}

func TestFortifyChangesThePlayerSideOnly(t *testing.T) {
	d := loadTestData(t)
	roster := Side{Units: []Unit{{Slot: 0, ID: "UNIT_REDIRONMAGE", Tier: "II", Level: 10}}, Runes: []string{}}
	squad := Side{Units: []Unit{{Slot: 0, ID: "UNIT_ELVENMAGE", Tier: "I", Level: 1}}, Runes: []string{}}
	fortified := Side{Units: []Unit{{Slot: 0, ID: "UNIT_ELVENMAGE", Tier: "I", Level: 1, Fortify: []FortifyChild{
		{Slot: 0, ID: "UNIT_CASSANDRAPENTAGHAST", Tier: "IV", Ascended: true},
		{Slot: 1, ID: "UNIT_CASSANDRAPENTAGHAST", Tier: "IV", Ascended: true},
	}}}, Runes: []string{}}
	plain, err := d.Run(Replay{Seed: 7, Player: squad, Enemy: roster})
	if err != nil {
		t.Fatal(err)
	}
	boosted, err := d.Run(Replay{Seed: 7, Player: fortified, Enemy: roster})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Timeline == boosted.Timeline {
		t.Error("a fortified hero fought exactly like an unfortified one")
	}
	mirrored, err := d.Run(Replay{Seed: 7, Player: roster, Enemy: fortified})
	if err != nil {
		t.Fatal(err)
	}
	unfortified, err := d.Run(Replay{Seed: 7, Player: roster, Enemy: squad})
	if err != nil {
		t.Fatal(err)
	}
	if mirrored.Timeline != unfortified.Timeline {
		t.Error("an enemy unit's fortify slots changed the battle")
	}
}

func TestFortifyRefusesBadSlotsAndChildren(t *testing.T) {
	d := loadTestData(t)
	for name, children := range map[string][]FortifyChild{
		"slot past the last": {{Slot: FortifySlotCount, ID: "UNIT_OGRE", Tier: "I"}},
		"negative slot":      {{Slot: -1, ID: "UNIT_OGRE", Tier: "I"}},
		"unknown child":      {{Slot: 0, ID: "UNIT_NOBODY", Tier: "I"}},
		"unknown tier":       {{Slot: 0, ID: "UNIT_OGRE", Tier: "V"}},
		"slot filled twice":  {{Slot: 2, ID: "UNIT_OGRE", Tier: "I"}, {Slot: 2, ID: "UNIT_OGRE", Tier: "II"}},
	} {
		unit := Unit{Slot: 0, ID: "UNIT_ELVENMAGE", Tier: "I", Level: 1, Fortify: children}
		if _, err := d.buildHero(unit, true); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
