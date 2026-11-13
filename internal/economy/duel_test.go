package economy

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const (
	defenderConsumeXP = 900
	defenderLevel     = 40
	duelSeed          = 9
)

func grownDefender(t *testing.T) json.RawMessage {
	t.Helper()
	return gameProfile(t, func(doc map[string]any) {
		doc["squads"] = []any{[]any{1, 2, nil, nil, nil}}
		mage := section(doc, "roster")["additions"].([]any)[0].(map[string]any)
		mage["tier"], mage["level"], mage["consume_xp"] = "IV", defenderLevel, defenderConsumeXP
		mage["fortify"] = []any{
			fortifyEntry(3, "UNIT_ARCANEMARIANHAWKE", "II", false), fortifyEntry(0, "UNIT_ELVENMAGE", "I", true),
		}
		section(doc, "gear")["heroes"] = []any{map[string]any{"instance_id": 1, "ascended": true,
			"slots": []any{"templar_longsword", "templar_longsword", "", "", ""}}}
	})
}

func duelOpponent(t *testing.T, raw json.RawMessage) DuelOpponent {
	t.Helper()
	opponent, err := DuelOpponentFrom("defender", raw, ruleNow)
	if err != nil {
		t.Fatal(err)
	}
	return opponent
}

func sideOnTheWire(t *testing.T, opponent DuelOpponent) battle.Side {
	t.Helper()
	var side battle.Side
	if err := json.Unmarshal(encodeBody(t, opponent), &side); err != nil {
		t.Fatal(err)
	}
	return side
}

func withoutBuilds(side battle.Side) battle.Side {
	bare := battle.Side{Units: []battle.Unit{}, Runes: side.Runes}
	for _, unit := range side.Units {
		bare.Units = append(bare.Units, battle.Unit{Slot: unit.Slot, ID: unit.ID, Tier: unit.Tier, Level: unit.Level})
	}
	return bare
}

func startDuel(t *testing.T, opponent DuelOpponent, enemy battle.Side) (*profile.ServerState, error) {
	t.Helper()
	request := BattleStartRequest{
		Mode: BattlePvp, Duel: opponent.ID, Squad: fixtureSquad(), Opponent: PvpOpponentRequest{Seed: 5}, Enemy: enemy,
	}
	server := &profile.ServerState{StoreRNG: 3}
	duels := fixedDuels{match: DuelMatch{Name: "Rival", Level: 7, Trophies: 900}, squad: opponent}
	_, _, err := ActWith(mustCatalog(t), gameProfile(t, nil), server, ActionBattleStart, encodeBody(t, request),
		ruleNow, Env{Duels: duels, Seeds: fixedSeed(duelSeed)})
	return server, err
}

func TestADuelOpponentCarriesEachHerosBuild(t *testing.T) {
	opponent := duelOpponent(t, grownDefender(t))
	want := []DuelUnit{
		{
			Slot: 0, ID: "UNIT_ELVENMAGE", Tier: "IV", Level: defenderLevel, ConsumeXP: defenderConsumeXP, Ascended: true,
			Gear: []string{"templar_longsword", "", "", "", ""},
			Fortify: []battle.FortifyChild{
				{Slot: 3, ID: "UNIT_ARCANEMARIANHAWKE", Tier: "II"}, {Slot: 0, ID: "UNIT_ELVENMAGE", Tier: "I", Ascended: true},
			},
		},
		{
			Slot: 1, ID: "UNIT_ARCANEMARIANHAWKE", Tier: "I", Level: 1, Gear: emptyGear(),
			Fortify: []battle.FortifyChild{},
		},
	}
	if !opponent.HeroBuilds || !reflect.DeepEqual(opponent.Units, want) {
		t.Errorf("opponent = %+v, want units %+v with hero builds", opponent, want)
	}
}

func TestADuelIsFoughtAgainstTheDefendersBuild(t *testing.T) {
	opponent := duelOpponent(t, grownDefender(t))
	claimed := sideOnTheWire(t, opponent)
	server, err := startDuel(t, opponent, claimed)
	if err != nil {
		t.Fatal(err)
	}
	fought := server.Battle.Replay.Enemy
	mage := fought.Units[0]
	if !fought.HeroBuilds || !mage.Ascended || mage.ConsumeXP != defenderConsumeXP ||
		!reflect.DeepEqual(mage.Gear, []string{"templar_longsword", "", "", "", ""}) ||
		len(mage.Fortify) != 2 || mage.Fortify[0].Slot != 0 || mage.Fortify[1].Slot != 3 {
		t.Errorf("the pending duel fields %+v", fought)
	}
	if _, err := startDuel(t, opponent, withoutBuilds(claimed)); refusalCode(err) != OpponentChanged {
		t.Errorf("a defender stripped of its build was accepted: %v", err)
	}
	data := mustCatalog(t).Battle
	built, err := data.Run(battle.Replay{Seed: duelSeed, Player: fixtureSquad(), Enemy: fought})
	if err != nil {
		t.Fatal(err)
	}
	stripped, err := data.Run(battle.Replay{Seed: duelSeed, Player: fixtureSquad(), Enemy: withoutBuilds(fought)})
	if err != nil {
		t.Fatal(err)
	}
	if built.Timeline == stripped.Timeline {
		t.Errorf("the defender's build changed nothing in the battle")
	}
}

func TestADuelWithoutHeroBuildsFieldsTheBareSquad(t *testing.T) {
	opponent := duelOpponent(t, grownDefender(t))
	opponent.HeroBuilds = false
	bare := withoutBuilds(sideOnTheWire(t, opponent))
	server, err := startDuel(t, opponent, bare)
	if err != nil {
		t.Fatal(err)
	}
	if fought := server.Battle.Replay.Enemy; !sameJSON(fought, canonicalSide(bare)) {
		t.Errorf("the pending duel fields %+v, want %+v", fought, bare)
	}
}
