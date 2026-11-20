package economy

import (
	"testing"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const (
	challengeMapIndex = 1
	challengeMapID    = "PVE_MAP_02"
	noDropSeed        = 1
	bearSeed          = 2
	healthRuneSeed    = 3
)

func rolledChallengeDrop(t *testing.T, edit func(doc map[string]any), mapID string, seed uint32) (*player, uint32) {
	t.Helper()
	p, err := openPlayer(mustCatalog(t), gameProfile(t, edit), &profile.ServerState{StoreRNG: seed}, ruleNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.awardChallengeDrop(mapID); err != nil {
		t.Fatal(err)
	}
	return p, p.rng.state
}

func TestChallengeDropsRollTheClientsPinnedStream(t *testing.T) {
	stepped := func(seed uint32) uint32 {
		r := rng{state: seed}
		r.next()
		return r.state
	}
	none, after := rolledChallengeDrop(t, nil, challengeMapID, noDropSeed)
	if len(none.granted.Heroes) != 0 || len(none.granted.Runes) != 0 || after != stepped(noDropSeed) {
		t.Errorf("seed %d granted %+v and left the stream at %d", noDropSeed, none.granted, after)
	}
	bear, after := rolledChallengeDrop(t, nil, challengeMapID, bearSeed)
	if len(bear.granted.Heroes) != 1 || bear.granted.Heroes[0].ID != "UNIT_BEAR" || after != stepped(bearSeed) {
		t.Errorf("seed %d granted %+v, want a Bear", bearSeed, bear.granted)
	}
	health, _ := rolledChallengeDrop(t, nil, challengeMapID, healthRuneSeed)
	if len(health.granted.Runes) != 1 || health.granted.Runes[0] != "rune-health-1" {
		t.Errorf("seed %d granted %+v, want a Rune of Health", healthRuneSeed, health.granted)
	}
	plain, after := rolledChallengeDrop(t, nil, "PVE_MAP_01", bearSeed)
	if len(plain.granted.Heroes) != 0 || after != bearSeed {
		t.Errorf("a map without challenge drops granted %+v and moved the stream to %d", plain.granted, after)
	}
}

func TestAFullRosterLeavesTheChallengeHeroBehind(t *testing.T) {
	full := func(doc map[string]any) {
		roster := section(doc, "roster")
		roster["starter_count"] = RosterCapacity - len(roster["additions"].([]any))
	}
	p, _ := rolledChallengeDrop(t, full, challengeMapID, bearSeed)
	if len(p.granted.Heroes) != 0 {
		t.Errorf("a full roster still took %+v", p.granted.Heroes)
	}
}

func challengeNodeOpen(doc map[string]any) {
	c := section(doc, "campaign")["completions"].([]any)
	c[0].([]any)[4] = 1
	for node := range 5 {
		c[challengeMapIndex].([]any)[node] = 1
	}
	section(doc, "campaign")["map_index"] = challengeMapIndex
}

func TestAChallengeNodeWinPaysTheDropAfterTheWinDrops(t *testing.T) {
	c := mustCatalog(t)
	m := c.Campaign[challengeMapIndex]
	grind := len(m.Nodes) - 1
	if m.ID != challengeMapID || !m.Nodes[grind].Grind {
		t.Fatalf("map %d is %s without a challenge node", challengeMapIndex, m.ID)
	}
	_, squad := strongerProfile(t)
	raw := edited(t, func(doc map[string]any) {
		section(doc, "account")["energy"] = 99
		for _, hero := range section(doc, "roster")["additions"].([]any) {
			hero.(map[string]any)["tier"], hero.(map[string]any)["level"] = "IV", 30
		}
	}, challengeNodeOpen)
	request := campaignStart(t, challengeMapIndex, grind)
	request.Squad = squad
	for seed := uint32(1); seed < 200; seed++ {
		r := rng{state: contractWinDrops(seed, len(c.Vouchers), 0, 0).rngAfter}
		want, dropped := c.ChallengeDrops.Pick(challengeMapID, r.below(c.ChallengeDrops.RollTotal))
		if !dropped || want.Kind != catalog.ChallengeDropUnit {
			continue
		}
		doc, result, _ := fight(t, raw, &profile.ServerState{StoreRNG: seed}, request, true)
		if len(result.Granted.Heroes) != 1 || result.Granted.Heroes[0].ID != want.ID {
			t.Fatalf("seed %d granted %+v, want %s", seed, result.Granted, want.ID)
		}
		added := rosterHero(t, doc, float64(result.Granted.Heroes[0].InstanceID))
		if added["id"] != want.ID || added["level"].(float64) != 1 {
			t.Errorf("the roster holds %v for the granted %s", added, want.ID)
		}
		return
	}
	t.Fatal("no seed rolled a hero on the challenge node")
}
