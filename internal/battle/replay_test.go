package battle

import (
	"reflect"
	"testing"
)

const (
	builtSquadSeed  = 11
	builtSquadLevel = 38
)

func builtSquad() Side {
	return Side{
		Units: []Unit{{
			Slot: 0, ID: "UNIT_ELVENMAGE", Tier: "IV", Level: builtSquadLevel, ConsumeXP: 900, Ascended: true,
			Gear: []string{"templar_longsword", "", "", "", ""},
			Fortify: []FortifyChild{
				{Slot: 0, ID: "UNIT_REDIRONMAGE", Tier: "II"}, {Slot: 3, ID: "UNIT_CASSANDRAPENTAGHAST", Tier: "I"},
			},
		}},
		Runes: []string{},
	}
}

func challengers() Side {
	return Side{
		Units: []Unit{
			{Slot: 0, ID: "UNIT_ELVENMAGE", Tier: "I", Level: 3},
			{Slot: 1, ID: "UNIT_ARCANEMARIANHAWKE", Tier: "I", Level: 1},
		},
		Runes: []string{},
	}
}

func buildEnemy(t *testing.T, d *Data, enemy Side) *hero {
	t.Helper()
	built, err := d.buildSide(enemy, enemy.HeroBuilds)
	if err != nil {
		t.Fatal(err)
	}
	return built.heroes[0]
}

func TestAnEnemySideWithHeroBuildsIsBuiltLikeAPlayerSide(t *testing.T) {
	d := loadTestData(t)
	squad := builtSquad()
	asPlayer := buildPlayerHero(t, d, squad.Units[0])
	bare := buildEnemy(t, d, squad)
	if bare.gear[0] != nil || bare.fortifyPower != 0 || bare.fortifyHealth != 0 || bare.critChance != 0 ||
		bare.statPower() >= asPlayer.statPower() || bare.statHealth() >= asPlayer.statHealth() {
		t.Errorf("an enemy without hero builds fights as %+v", bare)
	}
	squad.HeroBuilds = true
	if grown := buildEnemy(t, d, squad); !reflect.DeepEqual(grown, asPlayer) {
		t.Errorf("an enemy with hero builds = %+v, want the player's %+v", grown, asPlayer)
	}
	if asPlayer.gear[0] == nil || asPlayer.fortifyPower == 0 || asPlayer.fortifyHealth == 0 ||
		asPlayer.level != builtSquadLevel || asPlayer.critChance == 0 {
		t.Errorf("the fixture hero carries no build: %+v", asPlayer)
	}
}

func TestAReplayFightsTheEnemysHeroBuilds(t *testing.T) {
	d := loadTestData(t)
	replay := Replay{Seed: builtSquadSeed, Player: challengers(), Enemy: builtSquad()}
	bare, err := d.Run(replay)
	if err != nil {
		t.Fatal(err)
	}
	replay.Enemy.HeroBuilds = true
	grown, err := d.Run(replay)
	if err != nil {
		t.Fatal(err)
	}
	if grown.Timeline == bare.Timeline {
		t.Errorf("hero builds on the enemy side changed nothing: %+v", grown)
	}
	replay.Enemy.Units[0].Gear[0] = "gear-that-does-not-exist"
	if _, err := d.Run(replay); err == nil {
		t.Errorf("an enemy hero wore unknown gear")
	}
}
