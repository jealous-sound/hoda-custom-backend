package economy

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const (
	benchFerelden = 3
	benchDwarf    = 4
	benchRare     = 5
	benchEpic     = 6
	benchGiant    = 7
)

func expeditionProfile(t *testing.T, level int, edit func(doc map[string]any)) json.RawMessage {
	t.Helper()
	return gameProfile(t, func(doc map[string]any) {
		section(doc, "account")["level"] = level
		roster := section(doc, "roster")
		roster["next_instance_id"] = 8
		roster["additions"] = append(roster["additions"].([]any),
			rosterEntry("UNIT_BLACKSTONEIRREGULAR", benchFerelden, "I"),
			rosterEntry("UNIT_CARTATHUG", benchDwarf, "I"),
			rosterEntry("UNIT_DALISHWARRIOR", benchRare, "I"),
			rosterEntry("UNIT_FENRIS", benchEpic, "I"),
			rosterEntry("UNIT_OGRE", benchGiant, "I"),
		)
		if edit != nil {
			edit(doc)
		}
	})
}

func actAt(t *testing.T, raw json.RawMessage, server *profile.ServerState, action, body string, now time.Time) (
	map[string]any, BattleResult, error,
) {
	t.Helper()
	next, result, err := Act(mustCatalog(t), raw, server, action, []byte(body), now)
	if err != nil {
		return nil, result, err
	}
	return asMap(t, next), result, nil
}

func activeExpeditions(doc map[string]any) []any {
	expeditions, _ := doc[expeditionsSection].(map[string]any)
	active, _ := expeditions[expeditionsActive].([]any)
	return active
}

func TestExpeditionCatalogFollowsTheClientTable(t *testing.T) {
	rules := mustCatalog(t).Expeditions
	if len(rules.List) != 10 || rules.Slots(1) != 0 || rules.Slots(3) != 1 || rules.Slots(40) != 5 {
		t.Fatalf("rules %+v", rules.SlotLevels)
	}
	patrol, ok := mustCatalog(t).Expedition("lothering")
	if !ok || patrol.Duration != 15*time.Minute || patrol.Party != 1 || patrol.Bonus.Group != "Ferelden" {
		t.Errorf("lothering = %+v", patrol)
	}
	if rules.GreatSuccess(120) != 180 {
		t.Errorf("a great success pays %d", rules.GreatSuccess(120))
	}
	dwarf, _ := mustCatalog(t).Hero("UNIT_CARTATHUG")
	ogre, _ := mustCatalog(t).Hero("UNIT_OGRE")
	if !(catalog.ExpeditionTrait{Group: "Dwarf", Count: 1}).Matches(dwarf, dwarf.Rarity) ||
		(catalog.ExpeditionTrait{Faction: catalog.FactionRed, Count: 1}).Matches(ogre, ogre.Rarity) ||
		!(catalog.ExpeditionTrait{Giant: true, Count: 1}).Matches(ogre, ogre.Rarity) ||
		!(catalog.ExpeditionTrait{Rarity: catalog.RarityEpic, Count: 1}).Matches(dwarf, catalog.RarityMythic) {
		t.Error("traits match the wrong heroes")
	}
}

func TestExpeditionStartLocksThePartyOutOfEverythingElse(t *testing.T) {
	server := &profile.ServerState{}
	raw := expeditionProfile(t, 5, nil)
	doc, _ := mustAct(t, raw, server, ActionExpeditionStart, `{"expedition":"storm_coast","heroes":[1,2]}`)
	active := activeExpeditions(doc)
	if len(active) != 1 {
		t.Fatalf("active = %v", active)
	}
	entry := active[0].(map[string]any)
	if entry["id"] != "storm_coast" || entry["started_at"].(float64) != float64(ruleNow.Unix()) ||
		entry["ends_at"].(float64) != float64(ruleNow.Add(time.Hour).Unix()) {
		t.Errorf("stored %v", entry)
	}
	away := reencode(t, doc)
	for action, body := range map[string]string{
		ActionExpeditionStart: `{"expedition":"lothering","heroes":[3]}`,
		ActionConsume:         `{"target":3,"seeds":[1]}`,
		ActionFortify:         `{"parent":1,"slots":[{"index":0,"unit":3}]}`,
		ActionRemoveFortify:   `{"parent":2,"index":-1}`,
	} {
		if _, _, err := act(t, away, &profile.ServerState{}, action, body); err == nil {
			t.Errorf("%s %s was allowed", action, body)
		}
	}
	if _, _, err := act(t, away, &profile.ServerState{}, ActionConsume, `{"target":1,"seeds":[3]}`); refusalCode(err) != Unavailable {
		t.Errorf("consuming into an away hero: %v", err)
	}
	if _, _, err := startBattle(t, away, &profile.ServerState{}, campaignStart(t, 0, 0), 1); refusalCode(err) != Unavailable {
		t.Errorf("an away hero fought: %v", err)
	}
	if _, _, err := act(t, away, &profile.ServerState{}, ActionExpeditionStart,
		`{"expedition":"storm_coast","heroes":[3,4]}`); refusalCode(err) != AlreadyFull {
		t.Errorf("a second party on one expedition: %v", err)
	}
}

func TestExpeditionStartRefusesBadParties(t *testing.T) {
	raw := expeditionProfile(t, 30, withSquad(2))
	for _, body := range []string{
		`{"expedition":"nowhere","heroes":[1,3]}`,
		`{"expedition":"storm_coast","heroes":[1]}`,
		`{"expedition":"storm_coast","heroes":[1,1]}`,
		`{"expedition":"storm_coast","heroes":[1,2]}`,
		`{"expedition":"storm_coast","heroes":[1,99]}`,
		`{"expedition":"fade_rift","heroes":[1,3,4]}`,
		`{"expedition":"tevinter","heroes":[1,3,4,5,6]}`,
	} {
		if _, _, err := act(t, raw, &profile.ServerState{}, ActionExpeditionStart, body); refusalCode(err) != Unavailable {
			t.Errorf("%s: %v", body, err)
		}
	}
	locked := expeditionProfile(t, 5, nil)
	if _, _, err := act(t, locked, &profile.ServerState{}, ActionExpeditionStart,
		`{"expedition":"deep_roads","heroes":[1,3,4]}`); refusalCode(err) != Unavailable {
		t.Errorf("a level 8 expedition at level 5: %v", err)
	}
	if _, _, err := act(t, expeditionProfile(t, 2, nil), &profile.ServerState{}, ActionExpeditionStart,
		`{"expedition":"lothering","heroes":[3]}`); refusalCode(err) != Unavailable {
		t.Errorf("expeditions before their unlock level: %v", err)
	}
	full := expeditionProfile(t, 5, nil)
	doc, _ := mustAct(t, full, &profile.ServerState{}, ActionExpeditionStart, `{"expedition":"lothering","heroes":[3]}`)
	if _, _, err := act(t, reencode(t, doc), &profile.ServerState{}, ActionExpeditionStart,
		`{"expedition":"storm_coast","heroes":[1,4]}`); refusalCode(err) != AlreadyFull {
		t.Errorf("a second party with one slot open: %v", err)
	}
}

func TestExpeditionClaimPaysOnceWhenThePartyIsBack(t *testing.T) {
	server := &profile.ServerState{StoreRNG: 99}
	raw := expeditionProfile(t, 5, nil)
	doc, _ := mustAct(t, raw, server, ActionExpeditionStart, `{"expedition":"lothering","heroes":[3]}`)
	started := reencode(t, doc)
	claim := `{"expeditions":["lothering"]}`
	if _, _, err := actAt(t, started, server, ActionExpeditionClaim, claim, ruleNow.Add(14*time.Minute)); refusalCode(err) != Unavailable {
		t.Fatalf("an early claim: %v", err)
	}
	doc, result, err := actAt(t, started, server, ActionExpeditionClaim, claim, ruleNow.Add(15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if result.Gold < 180 || result.Gold > 270 || result.XP != 60 || len(result.Granted.Gear) != 1 {
		t.Errorf("a great success paid %+v", result)
	}
	if gold := number(t, doc, "account", "gold"); gold != 1000+float64(result.Gold) {
		t.Errorf("gold %v after +%d", gold, result.Gold)
	}
	if xp := rosterHero(t, doc, benchFerelden)["xp"].(float64); xp != 60 {
		t.Errorf("the party gained %v XP", xp)
	}
	if len(activeExpeditions(doc)) != 0 {
		t.Errorf("claimed expedition still active: %v", activeExpeditions(doc))
	}
	if _, _, err := actAt(t, reencode(t, doc), server, ActionExpeditionClaim, claim,
		ruleNow.Add(time.Hour)); refusalCode(err) != Unavailable {
		t.Errorf("a second claim: %v", err)
	}
	if _, _, err := act(t, reencode(t, doc), &profile.ServerState{}, ActionConsume, `{"target":1,"seeds":[3]}`); err != nil {
		t.Errorf("a returned hero cannot be consumed: %v", err)
	}
}

func TestExpeditionHeroDropsNeedRosterRoom(t *testing.T) {
	server := &profile.ServerState{StoreRNG: 7}
	raw := expeditionProfile(t, 15, nil)
	doc, _ := mustAct(t, raw, server, ActionExpeditionStart,
		`{"expedition":"western_approach","heroes":[3,4,5,6]}`)
	started := doc
	section(started, "roster")["starter_count"] = RosterCapacity
	claim := `{"expeditions":["western_approach"]}`
	back := ruleNow.Add(8 * time.Hour)
	if _, _, err := actAt(t, reencode(t, started), server, ActionExpeditionClaim, claim, back); refusalCode(err) != InventoryFull {
		t.Fatalf("a full roster: %v", err)
	}
	section(started, "roster")["starter_count"] = 0
	claimed, result, err := actAt(t, reencode(t, started), server, ActionExpeditionClaim, claim, back)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Granted.Heroes) != 1 {
		t.Fatalf("granted %+v", result.Granted)
	}
	hero, _ := mustCatalog(t).Hero(result.Granted.Heroes[0].ID)
	if hero.Rarity != catalog.RarityRare && hero.Rarity != catalog.RarityEpic {
		t.Errorf("rolled a %d hero", hero.Rarity)
	}
	if !hasHero(claimed, float64(result.Granted.Heroes[0].InstanceID)) {
		t.Error("the recruited hero is not in the roster")
	}
}

func TestExpeditionRecallFreesThePartyWithoutPay(t *testing.T) {
	raw := expeditionProfile(t, 5, nil)
	doc, _ := mustAct(t, raw, &profile.ServerState{}, ActionExpeditionStart, `{"expedition":"storm_coast","heroes":[3,4]}`)
	doc, result := mustAct(t, reencode(t, doc), &profile.ServerState{}, ActionExpeditionRecall,
		`{"expedition":"storm_coast"}`)
	if len(activeExpeditions(doc)) != 0 || result.Gold != 0 || number(t, doc, "account", "gold") != 1000 {
		t.Errorf("recall left %v and paid %+v", activeExpeditions(doc), result)
	}
	if _, _, err := act(t, reencode(t, doc), &profile.ServerState{}, ActionExpeditionRecall,
		`{"expedition":"storm_coast"}`); refusalCode(err) != Unavailable {
		t.Errorf("recalling twice: %v", err)
	}
}

func TestUploadCannotStartFinishOrDropExpeditions(t *testing.T) {
	raw := expeditionProfile(t, 5, nil)
	doc, _ := mustAct(t, raw, &profile.ServerState{}, ActionExpeditionStart, `{"expedition":"storm_coast","heroes":[3,4]}`)
	stored := reencode(t, doc)
	for name, upload := range map[string]json.RawMessage{
		"dropped": expeditionProfile(t, 5, nil),
		"rushed": expeditionProfile(t, 5, func(doc map[string]any) {
			doc[expeditionsSection] = map[string]any{expeditionsActive: []any{map[string]any{
				"id": "storm_coast", "heroes": []any{3, 4}, "started_at": 0, "ends_at": 0,
			}}}
		}),
	} {
		merged := merge(t, stored, upload, &profile.ServerState{})
		active := activeExpeditions(merged)
		if len(active) != 1 || active[0].(map[string]any)["ends_at"].(float64) != float64(ruleNow.Add(time.Hour).Unix()) {
			t.Errorf("%s upload changed expeditions to %v", name, active)
		}
	}
}
