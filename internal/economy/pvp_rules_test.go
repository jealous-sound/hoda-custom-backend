package economy

import (
	"errors"
	"testing"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const ruleEventSearchCycles = 200

func ruleEventTime(t *testing.T, wanted func(cycle int64) bool) (time.Time, int64) {
	t.Helper()
	c := mustCatalog(t)
	start := c.Battle.PvpEventCycle(ruleNow.Unix())
	for cycle := start; cycle < start+ruleEventSearchCycles; cycle++ {
		if wanted(cycle) {
			return time.Unix(c.Pvp.Events.CycleStart(cycle)+60, 0), cycle
		}
	}
	t.Fatal("no such event comes up")
	return time.Time{}, 0
}

func violationKind(err error) string {
	var v *battle.SquadViolation
	if errors.As(err, &v) {
		return v.Kind
	}
	return ""
}

func matchAt(t *testing.T, raw []byte, server *profile.ServerState, request PvpMatchRequest, at time.Time) (
	profile.PvpMatch, error,
) {
	t.Helper()
	_, result, err := ActWith(mustCatalog(t), raw, server, ActionPvpMatch, encodeBody(t, request), at,
		Env{Seeds: fixedSeed(7)})
	if err != nil {
		return profile.PvpMatch{}, err
	}
	return *result.Match, nil
}

func startAt(t *testing.T, raw []byte, server *profile.ServerState, request BattleStartRequest, at time.Time) (
	BattleResult, error,
) {
	t.Helper()
	_, result, err := ActWith(mustCatalog(t), raw, server, ActionBattleStart, encodeBody(t, request), at,
		Env{Seeds: fixedSeed(1)})
	return result, err
}

func TestARuleEventRefusesASquadThatBreaksItsRuleAndFightsALegalOne(t *testing.T) {
	c := mustCatalog(t)
	mageOnly := fixtureSquad()
	mageOnly.Units = mageOnly.Units[:1]
	at, _ := ruleEventTime(t, func(cycle int64) bool {
		return violationKind(c.Battle.CheckPvpEventSquad(cycle, 0, fixtureSquad())) == battle.ViolationRarity &&
			c.Battle.CheckPvpEventSquad(cycle, 0, mageOnly) == nil
	})
	raw := gameProfile(t, nil)
	server := &profile.ServerState{}
	match, err := matchAt(t, raw, server, PvpMatchRequest{Event: true}, at)
	if err != nil {
		t.Fatal(err)
	}
	request := pvpStartFor(t, match)
	if _, err := startAt(t, raw, server, request, at); refusalCode(err) != SquadNotAllowed {
		t.Fatalf("a legendary in a rarity-capped event: %v", err)
	}
	if server.Battle != nil || server.PvpMatch == nil || server.PvpMatch.ID != match.ID {
		t.Fatalf("the refusal used up the match: battle %+v, match %+v", server.Battle, server.PvpMatch)
	}
	request.Squad = mageOnly
	start, err := startAt(t, raw, server, request, at)
	if err != nil || start.BattleID == 0 {
		t.Fatalf("a legal squad: %+v, %v", start, err)
	}
}

func TestARuleEventLeavesLadderBattlesAlone(t *testing.T) {
	c := mustCatalog(t)
	at, _ := ruleEventTime(t, func(cycle int64) bool {
		return violationKind(c.Battle.CheckPvpEventSquad(cycle, 0, fixtureSquad())) != ""
	})
	raw := gameProfile(t, nil)
	server := &profile.ServerState{}
	match, err := matchAt(t, raw, server, PvpMatchRequest{}, at)
	if err != nil {
		t.Fatal(err)
	}
	if start, err := startAt(t, raw, server, pvpStartFor(t, match), at); err != nil || start.BattleID == 0 {
		t.Fatalf("a ladder battle during a rule event: %+v, %v", start, err)
	}
}

func TestFactionWarsMatchesYourSideAgainstAnother(t *testing.T) {
	c := mustCatalog(t)
	at, cycle := ruleEventTime(t, func(cycle int64) bool { return c.Battle.PvpEventSides(cycle) > 1 })
	sides := c.Battle.PvpEventSides(cycle)
	mine := -1
	for side := 0; side < sides && mine < 0; side++ {
		if c.Battle.CheckPvpEventSquad(cycle, side, fixtureSquad()) == nil {
			mine = side
		}
	}
	if mine < 0 {
		t.Fatal("the fixture squad fits no side")
	}
	raw := gameProfile(t, nil)
	for _, bad := range []PvpMatchRequest{{Event: true, Side: sides}, {Event: true, Side: -1}, {Side: 1}} {
		if _, err := matchAt(t, raw, &profile.ServerState{}, bad, at); refusalCode(err) != Unavailable {
			t.Errorf("match %+v: %v", bad, err)
		}
	}

	server := &profile.ServerState{}
	match, err := matchAt(t, raw, server, PvpMatchRequest{Event: true, Side: mine}, at)
	if err != nil || match.Side != mine {
		t.Fatalf("match %+v, %v", match, err)
	}
	enemy, err := c.Battle.PvpRoster(battle.PvpOpponent{Bot: match.Bot, Squad: match.Squad, Level: match.Level,
		Trophies: match.Trophies, Seed: match.Seed})
	if err != nil {
		t.Fatal(err)
	}
	if violationKind(c.Battle.CheckPvpEventSquad(cycle, mine, enemy)) != battle.ViolationSide {
		t.Fatalf("the opponent squad fits the player's own side %d", mine)
	}
	if start, err := startAt(t, raw, server, pvpStartFor(t, match), at); err != nil || start.BattleID == 0 {
		t.Fatalf("a squad on its matched side: %+v, %v", start, err)
	}

	other := (mine + 1) % sides
	server = &profile.ServerState{}
	match, err = matchAt(t, raw, server, PvpMatchRequest{Event: true, Side: other}, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := startAt(t, raw, server, pvpStartFor(t, match), at); refusalCode(err) != SquadNotAllowed {
		t.Fatalf("a squad fighting a match made for another side: %v", err)
	}
}
