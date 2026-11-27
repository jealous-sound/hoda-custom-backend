package battle

import (
	"errors"
	"testing"
)

const eventPoolMinSquads = 64

func squadRanksBelow(squad, weaker pvpSquad) bool {
	if squad.elite != weaker.elite {
		return weaker.elite
	}
	return squad.strength < weaker.strength
}

func ruleHero(id string, r rarity, f faction, giant bool, groups ...string) fieldedHero {
	return fieldedHero{hero: &character{id: id, rarity: r, originalRarity: r, faction: f, giant: giant, groups: groups}}
}

func squadOf(heroes ...fieldedHero) []fieldedHero {
	for slot := range heroes {
		heroes[slot].slot = slot
	}
	return heroes
}

func mustRules(t *testing.T, raw rawSquadRules) *squadRules {
	t.Helper()
	rules, err := parseSquadRules(&raw)
	if err != nil {
		t.Fatal(err)
	}
	return rules
}

func wantViolation(t *testing.T, name string, v *SquadViolation, kind string, slot int) {
	t.Helper()
	if kind == "" {
		if v != nil {
			t.Errorf("%s: refused with %+v", name, v)
		}
		return
	}
	if v == nil || v.Kind != kind || v.Slot != slot {
		t.Errorf("%s: got %+v, want %s in slot %d", name, v, kind, slot)
	}
}

func TestSquadRulesCapAndListRarities(t *testing.T) {
	common := ruleHero("UNIT_A", rarityCommon, factionRed, false)
	uncommon := ruleHero("UNIT_B", rarityUncommon, factionBlue, false)
	rare := ruleHero("UNIT_C", rarityRare, factionRed, false)
	otherRare := ruleHero("UNIT_E", rarityRare, factionWhite, false)
	legendaryGiant := ruleHero("UNIT_D", rarityLegendary, factionLarge, true)

	capped := mustRules(t, rawSquadRules{MaxRarity: "Uncommon"})
	_, v := capped.judge(squadOf(common, uncommon, common), anySide)
	wantViolation(t, "commons and uncommons", v, "", 0)
	_, v = capped.judge(squadOf(common, uncommon, rare), anySide)
	wantViolation(t, "a rare", v, ViolationRarity, 2)
	_, v = capped.judge(squadOf(legendaryGiant, common), anySide)
	wantViolation(t, "a legendary giant", v, ViolationRarity, 0)
	_, v = capped.judge(nil, anySide)
	wantViolation(t, "nobody", v, ViolationEmpty, -1)

	rares := mustRules(t, rawSquadRules{Rarities: []string{"Rare"}, UniqueHeroes: true})
	_, v = rares.judge(squadOf(rare, otherRare), anySide)
	wantViolation(t, "two rares", v, "", 0)
	_, v = rares.judge(squadOf(rare, common), anySide)
	wantViolation(t, "a common among rares", v, ViolationRarity, 1)
	_, v = rares.judge(squadOf(rare, otherRare, rare), anySide)
	wantViolation(t, "the same rare twice", v, ViolationDuplicate, 2)
}

func TestSquadRulesJudgeTheRarityOnTheCard(t *testing.T) {
	drawnLegendary := ruleHero("UNIT_A", rarityRare, factionRed, false)
	drawnLegendary.hero.originalRarity = rarityLegendary
	drawnRare := ruleHero("UNIT_B", rarityEpic, factionRed, false)
	drawnRare.hero.originalRarity = rarityRare

	rares := mustRules(t, rawSquadRules{Rarities: []string{"Rare"}})
	_, v := rares.judge(squadOf(drawnLegendary), anySide)
	wantViolation(t, "a card drawn Legendary", v, ViolationRarity, 0)
	_, v = rares.judge(squadOf(drawnRare), anySide)
	wantViolation(t, "a card drawn Rare", v, "", 0)
	drawnRare.ascended = true
	_, v = rares.judge(squadOf(drawnRare), anySide)
	wantViolation(t, "an ascended card", v, ViolationRarity, 0)
}

func TestSquadRulesFactionsLetLargeCreaturesJoinButGroupsDoNot(t *testing.T) {
	red := mustRules(t, rawSquadRules{Members: rawHeroFilter{Factions: []string{"Red"}}})
	redHero := ruleHero("UNIT_A", rarityRare, factionRed, false)
	dual := ruleHero("UNIT_B", rarityRare, factionWhiteRed, false)
	every := ruleHero("UNIT_D", rarityMythic, factionAll, false)
	giant := ruleHero("UNIT_E", rarityRare, factionLarge, true)
	warden := ruleHero("UNIT_C", rarityRare, factionBlue, false, "Warden")
	_, v := red.judge(squadOf(redHero, dual, every, giant), anySide)
	wantViolation(t, "red heroes and a giant", v, "", 0)
	_, v = red.judge(squadOf(redHero, warden), anySide)
	wantViolation(t, "a blue hero", v, ViolationMember, 1)

	wardens := mustRules(t, rawSquadRules{Members: rawHeroFilter{Groups: []string{"Warden"}}})
	wardenGiant := ruleHero("UNIT_F", rarityRare, factionLarge, true, "Warden")
	_, v = wardens.judge(squadOf(warden, wardenGiant), anySide)
	wantViolation(t, "wardens", v, "", 0)
	_, v = wardens.judge(squadOf(warden, giant), anySide)
	wantViolation(t, "a giant outside the group", v, ViolationMember, 1)
}

func TestSquadRulesSidesPickTheFirstSideTheWholeSquadFits(t *testing.T) {
	wars := mustRules(t, rawSquadRules{Sides: []rawHeroFilter{{Factions: []string{"Red"}}, {Factions: []string{"Blue"}}}})
	red := ruleHero("UNIT_A", rarityEpic, factionRed, false)
	blue := ruleHero("UNIT_B", rarityEpic, factionBlue, false)
	dual := ruleHero("UNIT_C", rarityEpic, factionRedBlue, false)
	giant := ruleHero("UNIT_D", rarityEpic, factionLarge, true)
	if side, v := wars.judge(squadOf(red, red, dual, giant), anySide); v != nil || side != 0 {
		t.Errorf("a red squad: side %d, %v", side, v)
	}
	if side, v := wars.judge(squadOf(dual, blue, giant), anySide); v != nil || side != 1 {
		t.Errorf("a blue squad: side %d, %v", side, v)
	}
	side, v := wars.judge(squadOf(red, blue, red), anySide)
	wantViolation(t, "a mixed squad", v, ViolationSide, 1)
	if side != 0 {
		t.Errorf("a mostly red squad is closest to side %d", side)
	}
	_, v = wars.judge(squadOf(red, red), 1)
	wantViolation(t, "a red squad matched as blue", v, ViolationSide, 0)
	_, v = wars.judge(squadOf(dual, dual), 1)
	wantViolation(t, "a dual squad matched as blue", v, "", 0)
	_, v = wars.judge(squadOf(red), len(wars.sides))
	wantViolation(t, "a side the event lacks", v, ViolationSide, 0)
}

func TestSquadRulesRefuseWhatTheyCannotEnforce(t *testing.T) {
	for name, raw := range map[string]rawSquadRules{
		"unknown rarity":        {MaxRarity: "Shiny"},
		"unknown listed rarity": {Rarities: []string{"Rare", "Shiny"}},
		"unknown faction":       {Members: rawHeroFilter{Factions: []string{"Purple"}}},
		"one side":              {Sides: []rawHeroFilter{{Factions: []string{"Red"}}}},
		"five sides": {Sides: []rawHeroFilter{{Factions: []string{"Red"}}, {Factions: []string{"Blue"}},
			{Factions: []string{"White"}}, {Factions: []string{"Black"}}, {Groups: []string{"Warden"}}}},
	} {
		if _, err := parseSquadRules(&raw); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	if rules, err := parseSquadRules(nil); rules != nil || err != nil {
		t.Errorf("no rules: %+v, %v", rules, err)
	}
}

func TestRuleEventBotsDefendWithSquadsTheRuleAllows(t *testing.T) {
	d := loadTestData(t)
	restricted, sided := 0, 0
	for e, event := range d.pvp.events {
		if event.rules == nil {
			continue
		}
		restricted++
		if len(event.rules.sides) > 0 {
			sided++
		}
		if len(event.changer.Effects) != 0 {
			t.Errorf("event %d buffs heroes on top of its squad rule", e)
		}
		for side, pool := range event.pools {
			if len(pool) < eventPoolMinSquads {
				t.Errorf("event %d side %d fields %d squads", e, side, len(pool))
			}
			for rank, squad := range pool {
				heroes, err := d.fieldedHeroes(d.pvp.squads[squad].cells)
				if err != nil {
					t.Fatal(err)
				}
				if !event.rules.fieldsAgainst(heroes, side) {
					t.Errorf("event %d side %d fields squad %d, which its rule refuses", e, side, squad)
				}
				if rank > 0 && squadRanksBelow(d.pvp.squads[squad], d.pvp.squads[pool[rank-1]]) {
					t.Errorf("event %d side %d pool is not weakest first at rank %d", e, side, rank)
				}
			}
		}
	}
	if restricted == 0 || sided == 0 {
		t.Fatalf("%d rule events, %d with sides", restricted, sided)
	}
}

func TestRuleEventOpponentsComeFromTheirPoolAndTheLadderSkipsThem(t *testing.T) {
	d := loadTestData(t)
	for _, squad := range d.pvp.ladder {
		if d.pvp.squads[squad].eventsOnly {
			t.Fatalf("the ladder fields events-only squad %d", squad)
		}
	}
	start := d.PvpEventCycle(1_900_000_000)
	checked := 0
	for cycle := start; cycle < start+int64(len(d.pvp.events))*4; cycle++ {
		event := d.pvp.eventFor(cycle)
		if event.rules == nil {
			continue
		}
		now := cycle*d.pvp.eventDuration + d.pvp.eventDuration/2
		for side, pool := range event.pools {
			pooled := map[int]bool{}
			for _, squad := range pool {
				pooled[squad] = true
			}
			for seed := uint32(1); seed <= 40; seed++ {
				o := d.PvpEventOpponent(30, int(seed)*40, now, side, seed)
				if !pooled[o.Squad] {
					t.Fatalf("cycle %d side %d fields squad %d from outside its pool", cycle, side, o.Squad)
				}
				if chosen := d.PvpEventOpponentFromBot(o.Bot, 30, now, side, seed); chosen.Squad != o.Squad {
					t.Fatalf("cycle %d: the chosen bot fields squad %d, matchmaking %d", cycle, chosen.Squad, o.Squad)
				}
				roster, err := d.PvpRoster(o)
				if err != nil {
					t.Fatal(err)
				}
				var v *SquadViolation
				if len(event.rules.sides) > 0 && !errors.As(d.CheckPvpEventSquad(cycle, side, roster), &v) {
					t.Fatalf("cycle %d side %d: the opponent fits the player's own side", cycle, side)
				}
			}
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no rule event came up")
	}
}
