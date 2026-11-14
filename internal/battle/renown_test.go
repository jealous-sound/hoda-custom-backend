package battle

import (
	"encoding/json"
	"strings"
	"testing"
)

const (
	renownTestSeed      = 7
	renownPowerPct      = 10
	renownHealthPct     = 6
	renownCritPermille  = 25
	renownChangerPct    = 50
	limitedRuneSharePct = 8
)

func renownDefenders() Side {
	return Side{
		Units: []Unit{
			{Slot: 0, ID: "UNIT_REDIRONMAGE", Tier: "II", Level: 10},
			{Slot: 1, ID: "UNIT_HURLOCKGRUNT", Tier: "I", Level: 4},
		},
		Runes: []string{},
	}
}

func renowned(side Side) Side {
	side.Renown = NewSideRenown(renownPowerPct, renownHealthPct, renownCritPermille)
	return side
}

func seat(t *testing.T, replay Replay, budget *statBudget) *sim {
	t.Helper()
	d := loadTestData(t)
	player, err := d.buildSide(replay.Player, true)
	if err != nil {
		t.Fatal(err)
	}
	enemy, err := d.buildSide(replay.Enemy, replay.Enemy.HeroBuilds)
	if err != nil {
		t.Fatal(err)
	}
	s := &sim{}
	if budget != nil {
		s.useStatBudget(budget)
	}
	s.setup(player, enemy, replay.Changer)
	return s
}

func expectRenownOn(t *testing.T, label string, plain, boosted *sim, side int) {
	t.Helper()
	seated := 0
	for slot := range SlotCount {
		before, after := plain.unitAt(side, slot), boosted.unitAt(side, slot)
		if !before.present {
			continue
		}
		seated++
		wantPower := runeApplyPct(before.power, renownPowerPct)
		wantHealth := runeApplyPct(before.maxHealth, renownHealthPct)
		wantCrit := before.critPct + float32(renownCritPermille)/10
		if after.power != wantPower || after.startPower != wantPower || wantPower <= before.power {
			t.Errorf("%s slot %d: power %d from %d, want %d", label, slot, after.power, before.power, wantPower)
		}
		if after.maxHealth != wantHealth || after.health != wantHealth || wantHealth <= before.maxHealth {
			t.Errorf("%s slot %d: health %d of %d from %d, want %d", label, slot, after.health, after.maxHealth,
				before.maxHealth, wantHealth)
		}
		if after.critPct != wantCrit || wantCrit <= before.critPct {
			t.Errorf("%s slot %d: crit %v from %v, want %v", label, slot, after.critPct, before.critPct, wantCrit)
		}
	}
	if seated == 0 {
		t.Fatalf("%s: side %d seats nobody", label, side)
	}
}

func expectNoRenownOn(t *testing.T, label string, plain, boosted *sim, side int) {
	t.Helper()
	for slot := range SlotCount {
		before, after := plain.unitAt(side, slot), boosted.unitAt(side, slot)
		if after.power != before.power || after.startPower != before.startPower ||
			after.maxHealth != before.maxHealth || after.health != before.health || after.critPct != before.critPct {
			t.Errorf("%s slot %d: side %d fights as %d/%d/%v, want %d/%d/%v", label, slot, side, after.power,
				after.maxHealth, after.critPct, before.power, before.maxHealth, before.critPct)
		}
	}
}

func TestRenownRaisesPowerHealthAndCritOfItsOwnSideOnly(t *testing.T) {
	plain := seat(t, Replay{Player: challengers(), Enemy: renownDefenders()}, nil)

	attacker := seat(t, Replay{Player: renowned(challengers()), Enemy: renownDefenders()}, nil)
	expectRenownOn(t, "the attacker's renown", plain, attacker, 0)
	expectNoRenownOn(t, "the attacker's renown", plain, attacker, 1)

	defender := seat(t, Replay{Player: challengers(), Enemy: renowned(renownDefenders())}, nil)
	expectRenownOn(t, "the defender's renown", plain, defender, 1)
	expectNoRenownOn(t, "the defender's renown", plain, defender, 0)

	both := seat(t, Replay{Player: renowned(challengers()), Enemy: renowned(renownDefenders())}, nil)
	expectRenownOn(t, "renown on both sides", plain, both, 0)
	expectRenownOn(t, "renown on both sides", plain, both, 1)
}

func TestRenownComesAfterRunesAndBeforeTheGameChanger(t *testing.T) {
	runed := challengers()
	runed.Runes = []string{"rune-power-1", "rune-health-1"}
	changer := &Changer{Effects: []ChangerEffect{{PowerPct: renownChangerPct, HealthPct: renownChangerPct}}}

	afterRunes := seat(t, Replay{Player: runed, Enemy: renownDefenders()}, nil)
	unruned := seat(t, Replay{Player: challengers(), Enemy: renownDefenders()}, nil)
	if afterRunes.unitAt(0, 0).power <= unruned.unitAt(0, 0).power {
		t.Fatal("the fixture runes add no power")
	}
	expectRenownOn(t, "renown over runes", afterRunes,
		seat(t, Replay{Player: renowned(runed), Enemy: renownDefenders()}, nil), 0)

	changed := seat(t, Replay{Player: renowned(runed), Enemy: renownDefenders(), Changer: changer}, nil)
	for slot := range SlotCount {
		before, after := afterRunes.unitAt(0, slot), changed.unitAt(0, slot)
		if !before.present {
			continue
		}
		wantPower := lround(mul(float32(runeApplyPct(before.power, renownPowerPct)), 1+percent(renownChangerPct)))
		wantHealth := lround(mul(float32(runeApplyPct(before.maxHealth, renownHealthPct)), 1+percent(renownChangerPct)))
		if after.power != wantPower || after.maxHealth != wantHealth || after.health != wantHealth {
			t.Errorf("slot %d under a game changer: %d/%d, want %d/%d", slot, after.power, after.maxHealth, wantPower,
				wantHealth)
		}
	}
}

func TestRenownRoundsOnTopOfTheRuneRoundedStats(t *testing.T) {
	const (
		stat            = 15
		runePct         = 10
		renownPct       = 50
		runesThenRenown = 26
		renownThenRunes = 25
	)
	if runeApplyPct(runeApplyPct(stat, runePct), renownPct) != runesThenRenown ||
		runeApplyPct(runeApplyPct(stat, renownPct), runePct) != renownThenRunes {
		t.Fatal("the fixture stat rounds the same in either order")
	}
	player, enemy := &side{}, &side{}
	player.heroes[backRowTestSlot] = grownHero("UNIT_A", rarityCommon, stat, stat)
	enemy.heroes[backRowTestSlot] = grownHero("UNIT_B", rarityCommon, stat, stat)
	player.runes = []*runeDef{{buff: buffPower, value: runePct}, {buff: buffHealth, value: runePct}}
	player.renown = SideRenown{PowerPct: renownPct, HealthPct: renownPct}
	s := &sim{}
	s.setup(player, enemy, nil)
	boosted, plain := s.unitAt(0, backRowTestSlot), s.unitAt(1, backRowTestSlot)
	if boosted.power != runesThenRenown || boosted.startPower != runesThenRenown ||
		boosted.maxHealth != runesThenRenown || boosted.health != runesThenRenown {
		t.Errorf("runes then renown give %d/%d, want %d", boosted.power, boosted.maxHealth, runesThenRenown)
	}
	if plain.power != stat || plain.maxHealth != stat {
		t.Errorf("the other side fights as %d/%d", plain.power, plain.maxHealth)
	}
}

func TestRenownIgnoresTheStatBudgetsRuneShare(t *testing.T) {
	budget := testBudget()
	budget.runeStatSharePct = limitedRuneSharePct
	runed := challengers()
	runed.Runes = []string{"rune-power-1", "rune-health-1"}

	unlimited := seat(t, Replay{Player: runed, Enemy: renownDefenders()}, nil)
	limited := seat(t, Replay{Player: runed, Enemy: renownDefenders()}, budget)
	if limited.budget != budget {
		t.Fatal("the fixture budget does not scale stats")
	}
	boosted := seat(t, Replay{Player: renowned(runed), Enemy: renowned(renownDefenders())}, budget)
	expectRenownOn(t, "renown under the stat budget", limited, boosted, 0)
	expectRenownOn(t, "renown under the stat budget", limited, boosted, 1)

	original := originalStatsBudget()
	trimmedOnly := seat(t, Replay{Player: renowned(runed), Enemy: renownDefenders()}, original)
	if trimmedOnly.budget != nil {
		t.Fatal("original stats should not scale")
	}
	expectRenownOn(t, "renown on original stats", unlimited, trimmedOnly, 0)
}

func TestAReplayFightsWithEachSidesRenown(t *testing.T) {
	d := loadTestData(t)
	run := func(player, enemy Side) Outcome {
		t.Helper()
		outcome, err := d.Run(Replay{Seed: renownTestSeed, Player: player, Enemy: enemy})
		if err != nil {
			t.Fatal(err)
		}
		return outcome
	}
	plain := run(challengers(), renownDefenders())
	if run(renowned(challengers()), renownDefenders()).Timeline == plain.Timeline {
		t.Error("the attacker's renown changed nothing in the battle")
	}
	if run(challengers(), renowned(renownDefenders())).Timeline == plain.Timeline {
		t.Error("the defender's renown changed nothing in the battle")
	}
	empty := challengers()
	empty.Renown = &SideRenown{}
	if run(empty, renownDefenders()) != plain {
		t.Error("a renown that gives nothing changed the battle")
	}
}

func TestRenownIsLeftOffTheWireWhenItGivesNothing(t *testing.T) {
	if NewSideRenown(0, 0, 0) != nil || (&SideRenown{}).Canonical() != nil || (*SideRenown)(nil).Canonical() != nil {
		t.Error("a renown that gives nothing is not nil")
	}
	for _, given := range []*SideRenown{{PowerPct: 2}, {HealthPct: 2}, {CritPermille: 5}} {
		kept := NewSideRenown(given.PowerPct, given.HealthPct, given.CritPermille)
		if kept == nil || *kept != *given || *given.Canonical() != *given {
			t.Errorf("renown %+v became %+v", given, kept)
		}
	}
	bare, err := json.Marshal(challengers())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bare), "renown") {
		t.Errorf("a side without renown is sent as %s", bare)
	}
	sent, err := json.Marshal(renowned(Side{Units: []Unit{}, Runes: []string{}}))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"units":[],"runes":[],"renown":{"power_pct":10,"health_pct":6,"crit_permille":25}}`
	if string(sent) != want {
		t.Errorf("a side with renown is sent as %s, want %s", sent, want)
	}
}

func TestTheGoldenLoaderReadsASidesRenown(t *testing.T) {
	recorded := `{"name":"renowned duel","seed":7,"changer":null,` +
		`"player":{"units":[{"slot":0,"id":"UNIT_ELVENMAGE","tier":"I","level":3}],"runes":[],` +
		`"renown":{"power_pct":10,"health_pct":6,"crit_permille":25}},` +
		`"enemy":{"units":[{"slot":0,"id":"UNIT_REDIRONMAGE","tier":"II","level":10}],"runes":[],"hero_builds":true,` +
		`"renown":{"power_pct":0,"health_pct":4,"crit_permille":0}},"won":true,"crits":2}`
	var golden goldenBattle
	if err := json.Unmarshal([]byte(recorded), &golden); err != nil {
		t.Fatal(err)
	}
	attacker, defender := golden.Player.Renown, golden.Enemy.Renown
	if attacker == nil || *attacker != *NewSideRenown(renownPowerPct, renownHealthPct, renownCritPermille) {
		t.Errorf("the attacker's renown loaded as %+v", attacker)
	}
	if defender == nil || *defender != (SideRenown{HealthPct: 4}) {
		t.Errorf("the defender's renown loaded as %+v", defender)
	}
	if !golden.Won || golden.Crits != 2 || golden.Seed != renownTestSeed {
		t.Errorf("the battle beside the renown loaded as %+v", golden)
	}
	if _, err := loadTestData(t).Run(golden.Replay); err != nil {
		t.Errorf("a recorded battle with renown does not replay: %v", err)
	}
	for _, battle := range loadCorpus(t).Battles {
		if _, err := json.Marshal(battle.Replay); err != nil {
			t.Fatalf("%s: %v", battle.Name, err)
		}
	}
}
