package economy

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

func gameProfile(t *testing.T, edit func(doc map[string]any)) json.RawMessage {
	t.Helper()
	completions := make([]any, catalog.CampaignMaxMaps)
	for i := range completions {
		completions[i] = make([]any, catalog.CampaignMaxNodes)
		for j := range catalog.CampaignMaxNodes {
			completions[i].([]any)[j] = 0
		}
	}
	nodes := make([]any, catalog.NexusMaxNodes)
	for i := range nodes {
		nodes[i] = map[string]any{"wins": 0, "losses": 0, "previous_wins": 0}
	}
	doc := map[string]any{
		"version": 8,
		"account": map[string]any{"level": 5, "experience": 160, "experience_min": 120, "experience_max": 200,
			"energy": 20, "energy_max": 16, "energy_regen_timer": 0, "stamina": 8, "stamina_max": 8,
			"stamina_regen_timer": 0, "gold": 1000, "gems": 200},
		"store": map[string]any{"rng_state": 5, "opened_total": 0, "opened_by_pack": []any{0, 0, 0, 0},
			"champion_since_legendary": 0, "vouchers": make([]any, 0)},
		"roster": map[string]any{"starter_count": 0, "next_instance_id": 3, "starters": []any{},
			"additions": []any{
				map[string]any{"id": "UNIT_ELVENMAGE", "instance_id": 1, "tier": "I", "level": 3, "xp": 0,
					"ascended": false, "consume_xp": 0},
				map[string]any{"id": "UNIT_ARIQUN", "instance_id": 2, "tier": "I", "level": 1, "xp": 0,
					"ascended": false, "consume_xp": 0},
			}},
		"current_squad": 0,
		"squads":        []any{},
		"campaign": map[string]any{"map_index": 0, "node_index": 0, "completions": completions,
			"node_losses": make([]any, 0)},
		"nexus": map[string]any{"potions": 2, "potion_max": 10, "keys": []any{0, 0, 0, 0},
			"run": map[string]any{"event_index": 0, "difficulty_index": 0, "committed": false, "reset": false,
				"current_node_step": 1, "escrow_gold": 0, "escrow_xp": 0, "escrow_potions": 0,
				"escrow_units": []any{0, 0, 0, 0, 0, 0, 0}, "nodes": nodes},
			"events": map[string]any{"completions": []any{}, "unlocked_at": []any{}, "remaining_runs": []any{}}},
		"gear": map[string]any{"tomes": 1, "inventory": map[string]any{"templar_longsword": 2},
			"heroes": []any{}},
		"runes": map[string]any{"next_uuid": 4, "seen": 0, "squads": []any{
			map[string]any{"prestige_unlocked": false, "slots": []any{0, 0, 0}},
		}, "items": []any{
			map[string]any{"uuid": 1, "def": "rune-greed-1", "expire_at": 0, "inventory_expire_at": 1900000000},
			map[string]any{"uuid": 2, "def": "rune-greed-1", "expire_at": 0, "inventory_expire_at": 1900000000},
			map[string]any{"uuid": 3, "def": "rune-power-1", "expire_at": 0, "inventory_expire_at": 1900000000},
		}},
		"missions": map[string]any{"counters": map[string]any{}, "progress": map[string]any{}, "claimed": []any{}},
		"tutorial": map[string]any{"complete": true, "consume_complete": true},
	}
	if edit != nil {
		edit(doc)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func section(doc map[string]any, path ...string) map[string]any {
	for _, name := range path {
		doc = doc[name].(map[string]any)
	}
	return doc
}

func asMap(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func number(t *testing.T, doc map[string]any, path ...string) float64 {
	t.Helper()
	value, ok := section(doc, path[:len(path)-1]...)[path[len(path)-1]].(float64)
	if !ok {
		t.Fatalf("%v is not a number", path)
	}
	return value
}

var ruleNow = time.Unix(1_900_000_000-86400, 0)

func act(t *testing.T, raw json.RawMessage, server *profile.ServerState, action, body string) (
	map[string]any, BattleResult, error,
) {
	t.Helper()
	next, result, err := Act(mustCatalog(t), raw, server, action, []byte(body), ruleNow)
	if err != nil {
		return nil, result, err
	}
	return asMap(t, next), result, nil
}

func mustAct(t *testing.T, raw json.RawMessage, server *profile.ServerState, action, body string) (
	map[string]any, BattleResult,
) {
	t.Helper()
	doc, result, err := act(t, raw, server, action, body)
	if err != nil {
		t.Fatalf("%s %s: %v", action, body, err)
	}
	return doc, result
}

func reencode(t *testing.T, doc map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestUploadCannotMintCurrencyHeroesRunesOrGear(t *testing.T) {
	c := mustCatalog(t)
	stored := profile.Record{Profile: gameProfile(t, nil), Revision: 1}
	forged := gameProfile(t, func(doc map[string]any) {
		section(doc, "account")["gold"] = 999999
		section(doc, "account")["level"] = 90
		section(doc, "nexus")["potions"] = 99
		roster := section(doc, "roster")
		roster["additions"] = append(roster["additions"].([]any), map[string]any{
			"id": "UNIT_ARIQUN", "instance_id": 50, "tier": "IV", "level": 60, "xp": 0, "ascended": true,
		})
		roster["additions"].([]any)[0].(map[string]any)["id"] = "UNIT_LEANDRAAMELL"
		runes := section(doc, "runes")
		runes["items"] = append(runes["items"].([]any), map[string]any{
			"uuid": 9, "def": "rune-greed-5", "expire_at": 0, "inventory_expire_at": 1900000000,
		})
		section(doc, "gear", "inventory")["templar_longsword"] = 9
		section(doc, "gear")["tomes"] = 5
		section(doc, "missions")["claimed"] = []any{"a1"}
	})
	server := profile.ServerState{}
	merged, err := MergeUpload(c, stored, &server, forged, ruleNow)
	if err != nil {
		t.Fatal(err)
	}
	doc := asMap(t, merged)
	if number(t, doc, "account", "gold") != 1000 || number(t, doc, "account", "level") != 5 ||
		number(t, doc, "nexus", "potions") != 2 || number(t, doc, "gear", "tomes") != 1 {
		t.Errorf("account = %v nexus potions = %v", doc["account"], section(doc, "nexus")["potions"])
	}
	additions := section(doc, "roster")["additions"].([]any)
	if len(additions) != 1 || additions[0].(map[string]any)["id"] != "UNIT_ARIQUN" {
		t.Errorf("roster additions = %v", additions)
	}
	if items := section(doc, "runes")["items"].([]any); len(items) != 3 {
		t.Errorf("rune items = %v", items)
	}
	if number(t, doc, "gear", "inventory", "templar_longsword") != 2 {
		t.Errorf("gear = %v", doc["gear"])
	}
	if claimed := section(doc, "missions")["claimed"].([]any); len(claimed) != 0 {
		t.Errorf("claimed = %v", claimed)
	}
}

func TestUploadKeepsHonestClientChanges(t *testing.T) {
	c := mustCatalog(t)
	stored := profile.Record{Profile: gameProfile(t, nil), Revision: 1}
	activation := ruleNow.Add(5 * time.Minute).Unix()
	honest := gameProfile(t, func(doc map[string]any) {
		roster := section(doc, "roster")
		hero := roster["additions"].([]any)[0].(map[string]any)
		hero["level"] = 9
		roster["additions"] = []any{hero}
		section(doc, "gear", "inventory")["templar_longsword"] = 1
		section(doc, "gear")["heroes"] = []any{map[string]any{"instance_id": 1, "ascended": false,
			"prestige_unlocked": false, "slots": []any{"templar_longsword", "", ""}}}
		runes := section(doc, "runes")
		runes["items"].([]any)[0].(map[string]any)["expire_at"] = activation
		runes["items"].([]any)[1].(map[string]any)["expire_at"] = activation + 86400
		doc["settings"] = map[string]any{"fast_battles": true}
	})
	server := profile.ServerState{}
	merged, err := MergeUpload(c, stored, &server, honest, ruleNow)
	if err != nil {
		t.Fatal(err)
	}
	doc := asMap(t, merged)
	additions := section(doc, "roster")["additions"].([]any)
	if len(additions) != 1 || additions[0].(map[string]any)["level"].(float64) != 9 {
		t.Errorf("roster = %v", additions)
	}
	if number(t, doc, "gear", "inventory", "templar_longsword") != 1 {
		t.Errorf("equipping gear was undone: %v", doc["gear"])
	}
	items := section(doc, "runes")["items"].([]any)
	if items[0].(map[string]any)["expire_at"].(float64) != float64(activation) {
		t.Errorf("rune activation was undone: %v", items[0])
	}
	limit := ruleNow.Add(5*time.Minute + runeActivationSlack).Unix()
	if items[1].(map[string]any)["expire_at"].(float64) != float64(limit) {
		t.Errorf("an overlong activation was kept: %v", items[1])
	}
	if !section(doc, "settings")["fast_battles"].(bool) {
		t.Errorf("settings were dropped")
	}
}

func TestEnergyRegeneratesOnTheServerClock(t *testing.T) {
	c := mustCatalog(t)
	stored := profile.Record{Profile: gameProfile(t, func(doc map[string]any) {
		section(doc, "account")["energy"] = 2
		section(doc, "account")["energy_regen_timer"] = 100
	}), Revision: 1}
	server := profile.ServerState{RegenAt: ruleNow.Add(-11 * time.Minute).Unix()}
	merged, err := MergeUpload(c, stored, &server, stored.Profile, ruleNow)
	if err != nil {
		t.Fatal(err)
	}
	doc := asMap(t, merged)
	if number(t, doc, "account", "energy") != 4 || number(t, doc, "account", "energy_regen_timer") != 160 {
		t.Errorf("account = %v", doc["account"])
	}
	if server.RegenAt != ruleNow.Unix() {
		t.Errorf("regen_at = %d", server.RegenAt)
	}
}

func TestFirstUploadStartsFromTheNewPlayerEconomy(t *testing.T) {
	c := mustCatalog(t)
	server := profile.ServerState{}
	merged, err := MergeUpload(c, profile.Record{}, &server, gameProfile(t, func(doc map[string]any) {
		section(doc, "roster")["additions"] = []any{
			map[string]any{"id": "UNIT_LEGIONNAIRESCOUT", "instance_id": 1, "tier": "I", "level": 1},
			map[string]any{"id": "UNIT_ARIQUN", "instance_id": 2, "tier": "I", "level": 1},
		}
	}), ruleNow)
	if err != nil {
		t.Fatal(err)
	}
	doc := asMap(t, merged)
	if number(t, doc, "account", "gold") != StartGold || number(t, doc, "account", "gems") != StartGems ||
		number(t, doc, "account", "level") != 1 || number(t, doc, "gear", "tomes") != 0 {
		t.Errorf("account = %v", doc["account"])
	}
	additions := section(doc, "roster")["additions"].([]any)
	if len(additions) != 1 || additions[0].(map[string]any)["id"] != "UNIT_LEGIONNAIRESCOUT" {
		t.Errorf("roster = %v", additions)
	}
	items := section(doc, "runes")["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["def"] != "rune-greed-1" ||
		items[1].(map[string]any)["def"] != "rune-power-1" {
		t.Errorf("starter runes should keep one greed and one power rune: %v", items)
	}
}

func TestCampaignBattlePaysFromTheNodeTable(t *testing.T) {
	c := mustCatalog(t)
	node := c.Campaign[0].Nodes[0]
	raw := gameProfile(t, nil)
	server := &profile.ServerState{StoreRNG: 3}
	doc, result := mustAct(t, raw, server, ActionBattle, `{"mode":"campaign","map":0,"node":0,"won":true,"crit_gold":99999}`)
	wantGold := 1000 + int64(node.Gold) + maxCreditedCrits*critGoldPerCrit
	if !result.FirstWin || result.Gems != int64(node.FirstWinGems) ||
		number(t, doc, "account", "gold") != float64(wantGold) ||
		number(t, doc, "account", "energy") != float64(20-node.EnergyCost) {
		t.Errorf("result = %+v account = %v", result, doc["account"])
	}
	if section(doc, "campaign")["completions"].([]any)[0].([]any)[0].(float64) != 1 {
		t.Errorf("completions = %v", section(doc, "campaign")["completions"])
	}
	_, _, err := act(t, raw, server, ActionBattle, `{"mode":"campaign","map":0,"node":3,"won":true}`)
	if refusalCode(err) != Unavailable {
		t.Errorf("locked node: %v", err)
	}
	_, _, err = act(t, raw, server, ActionBattle, `{"mode":"campaign","map":1,"node":0,"won":true}`)
	if refusalCode(err) != Unavailable {
		t.Errorf("locked map: %v", err)
	}
	tired := gameProfile(t, func(doc map[string]any) { section(doc, "account")["energy"] = 0 })
	if _, _, err := act(t, tired, server, ActionBattle, `{"mode":"campaign","map":0,"node":0,"won":true}`); refusalCode(err) != CantAfford {
		t.Errorf("no energy: %v", err)
	}
}

func TestCampaignMasteryPaysOnce(t *testing.T) {
	c := mustCatalog(t)
	node := c.Campaign[0].Nodes[0]
	raw := gameProfile(t, func(doc map[string]any) {
		section(doc, "campaign")["completions"].([]any)[0].([]any)[0] = node.WinsToMaster - 1
	})
	_, result := mustAct(t, raw, &profile.ServerState{StoreRNG: 3}, ActionBattle, `{"mode":"campaign","map":0,"node":0,"won":true}`)
	if !result.Mastered || result.Gems != int64(node.MasteryGems) {
		t.Errorf("result = %+v", result)
	}
	doc, result := mustAct(t, raw, &profile.ServerState{StoreRNG: 3}, ActionBattle, `{"mode":"campaign","map":0,"node":0,"won":false}`)
	if result.Gold != 0 || number(t, doc, "account", "gold") != 1000 {
		t.Errorf("a loss paid %+v", result)
	}
}

func TestPvpBattleDerivesTheOpponentFromTheSeed(t *testing.T) {
	c := mustCatalog(t)
	raw := gameProfile(t, nil)
	doc, result := mustAct(t, raw, &profile.ServerState{StoreRNG: 3}, ActionBattle, `{"mode":"pvp","seed":42,"won":true}`)
	level := pvpOpponentLevel(&c.Pvp, 5, 42)
	if result.Gold != pvpGold(&c.Pvp, level, true) || number(t, doc, "account", "stamina") != 7 {
		t.Errorf("result = %+v level %d", result, level)
	}
	tired := gameProfile(t, func(doc map[string]any) { section(doc, "account")["stamina"] = 0 })
	if _, _, err := act(t, tired, &profile.ServerState{}, ActionBattle, `{"mode":"pvp","seed":42,"won":true}`); refusalCode(err) != CantAfford {
		t.Errorf("no stamina: %v", err)
	}
}

func TestPvpEventBannersAreCappedAndClaimedOnce(t *testing.T) {
	c := mustCatalog(t)
	raw := gameProfile(t, nil)
	server := &profile.ServerState{StoreRNG: 3}
	_, result := mustAct(t, raw, server, ActionBattle, `{"mode":"pvp","seed":7,"won":true,"event":true,"banners":5000}`)
	if want := maxBannersPerWin(&c.Pvp.Events, 0); result.Banners != want || server.PvpEvent.Banners != want {
		t.Fatalf("banners = %d, want %d", result.Banners, want)
	}
	cycle := server.PvpEvent.Cycle
	_, _, err := act(t, raw, server, ActionPvpEventClaim, `{"cycle":`+itoa(cycle)+`}`)
	if refusalCode(err) != Unavailable {
		t.Errorf("claiming a running event: %v", err)
	}
	later := *server
	next, result, err := Act(c, raw, &later, ActionPvpEventClaim, []byte(`{"cycle":`+itoa(cycle)+`}`),
		ruleNow.Add(time.Duration(c.Pvp.Events.DurationSeconds)*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if result.EventRank < 1 || len(later.PvpResults) != 1 || !later.PvpResults[0].Claimed {
		t.Errorf("result = %+v server = %+v", result, later.PvpResults)
	}
	if _, _, err := Act(c, next, &later, ActionPvpEventClaim, []byte(`{"cycle":`+itoa(cycle)+`}`),
		ruleNow.Add(time.Duration(c.Pvp.Events.DurationSeconds)*time.Second)); refusalCode(err) != Unavailable {
		t.Errorf("second claim: %v", err)
	}
}

func itoa(value int64) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func TestNexusRunEscrowsAndAwards(t *testing.T) {
	c := mustCatalog(t)
	difficulty := c.Nexus[0].Difficulties[0]
	raw := gameProfile(t, func(doc map[string]any) { section(doc, "account")["energy"] = 99 })
	server := &profile.ServerState{StoreRNG: 3}
	doc, result := mustAct(t, raw, server, ActionBattle, `{"mode":"nexus","step":1,"won":true}`)
	run := section(doc, "nexus", "run")
	if !result.Escrowed || run["committed"] != true || run["current_node_step"].(float64) != 2 ||
		run["escrow_gold"].(float64) != float64(difficulty.Nodes[0].Gold) ||
		number(t, doc, "account", "gold") != 1000 {
		t.Fatalf("run = %v result = %+v", run, result)
	}
	if _, _, err := act(t, reencode(t, doc), server, ActionBattle, `{"mode":"nexus","step":1,"won":true}`); refusalCode(err) != Unavailable {
		t.Errorf("replayed step: %v", err)
	}
	awarded, result := mustAct(t, reencode(t, doc), server, ActionNexusAward, `{}`)
	if number(t, awarded, "account", "gold") != float64(1000+difficulty.Nodes[0].Gold) ||
		section(awarded, "nexus", "run")["committed"] != false || len(result.Granted.Heroes) != 1 {
		t.Errorf("award = %+v account = %v", result, awarded["account"])
	}
	if _, _, err := act(t, reencode(t, awarded), server, ActionNexusAward, `{}`); refusalCode(err) != Unavailable {
		t.Errorf("second award: %v", err)
	}
}

func TestNexusLossNeedsAPotionToContinue(t *testing.T) {
	raw := gameProfile(t, func(doc map[string]any) { section(doc, "account")["energy"] = 99 })
	server := &profile.ServerState{StoreRNG: 3}
	lost, _ := mustAct(t, raw, server, ActionBattle, `{"mode":"nexus","step":1,"won":false}`)
	if section(lost, "nexus", "run")["reset"] != true {
		t.Skip("first difficulty costs no potion")
	}
	if _, _, err := act(t, reencode(t, lost), server, ActionBattle, `{"mode":"nexus","step":1,"won":true}`); refusalCode(err) != Unavailable {
		t.Errorf("fought on without a revive: %v", err)
	}
	revived, _ := mustAct(t, reencode(t, lost), server, ActionBattle, `{"mode":"nexus","step":1,"won":true,"revive":true}`)
	if number(t, revived, "nexus", "potions") != 1 {
		t.Errorf("potions = %v", section(revived, "nexus")["potions"])
	}
}

func TestMissionsClaimOnceWhenReached(t *testing.T) {
	c := mustCatalog(t)
	index, _ := c.MissionIndex("a1")
	mission := c.Missions[index]
	raw := gameProfile(t, nil)
	if _, _, err := act(t, raw, &profile.ServerState{}, ActionMissionClaim, `{"mission":"a1"}`); refusalCode(err) != Unavailable {
		t.Errorf("unreached mission: %v", err)
	}
	beaten := gameProfile(t, func(doc map[string]any) {
		boss := c.Campaign[0].BossNode()
		section(doc, "campaign")["completions"].([]any)[0].([]any)[boss] = 1
	})
	doc, _ := mustAct(t, beaten, &profile.ServerState{}, ActionMissionClaim, `{"mission":"a1"}`)
	if number(t, doc, "account", "gems") != float64(200+mission.Gems) {
		t.Errorf("account = %v", doc["account"])
	}
	if _, _, err := act(t, reencode(t, doc), &profile.ServerState{}, ActionMissionClaim, `{"mission":"a1"}`); refusalCode(err) != Unavailable {
		t.Errorf("second claim: %v", err)
	}
}

func TestGearSellPrestigeAndAscension(t *testing.T) {
	c := mustCatalog(t)
	index, _ := c.GearIndex("templar_longsword")
	raw := gameProfile(t, nil)
	doc, _ := mustAct(t, raw, &profile.ServerState{}, ActionGearSell, `{"gear":"templar_longsword"}`)
	if number(t, doc, "account", "gold") != float64(1000+c.GearSellValue(c.Gear[index])) ||
		number(t, doc, "gear", "inventory", "templar_longsword") != 1 {
		t.Errorf("after sale %v %v", doc["account"], doc["gear"])
	}
	doc, _ = mustAct(t, raw, &profile.ServerState{}, ActionGearPrestige, `{"instance_id":1}`)
	if number(t, doc, "account", "gems") != float64(200-c.GearPrestigeGems) {
		t.Errorf("prestige charged %v", doc["account"])
	}
	if _, _, err := act(t, raw, &profile.ServerState{}, ActionAscend, `{"instance_id":1}`); refusalCode(err) != Unavailable {
		t.Errorf("non-legendary ascension: %v", err)
	}
	doc, _ = mustAct(t, raw, &profile.ServerState{}, ActionAscend, `{"instance_id":2}`)
	if number(t, doc, "gear", "tomes") != 0 {
		t.Errorf("ascension kept the tome: %v", doc["gear"])
	}
}

func TestRuneCraftAndPrestigeSlot(t *testing.T) {
	raw := gameProfile(t, nil)
	doc, result := mustAct(t, raw, &profile.ServerState{}, ActionRuneCraft, `{"pairs":[[1,2]]}`)
	items := section(doc, "runes")["items"].([]any)
	if len(items) != 2 || items[1].(map[string]any)["def"] != "rune-greed-2" || len(result.Granted.Runes) != 1 {
		t.Errorf("items = %v", items)
	}
	if _, _, err := act(t, raw, &profile.ServerState{}, ActionRuneCraft, `{"pairs":[[1,3]]}`); refusalCode(err) != Unavailable {
		t.Errorf("mismatched craft: %v", err)
	}
	doc, _ = mustAct(t, raw, &profile.ServerState{}, ActionRunePrestige, `{"squad":0}`)
	if number(t, doc, "account", "gems") != 200-RunePrestigeGems {
		t.Errorf("account = %v", doc["account"])
	}
}

func TestTutorialRewardsAreGivenOnce(t *testing.T) {
	raw := gameProfile(t, nil)
	server := &profile.ServerState{}
	doc, result := mustAct(t, raw, server, ActionTutorialReward, `{"reward":"parting_gift"}`)
	if len(result.Granted.Heroes) != 3 || len(result.Granted.Runes) != 2 ||
		len(section(doc, "roster")["additions"].([]any)) != 5 {
		t.Errorf("result = %+v", result)
	}
	if _, _, err := act(t, reencode(t, doc), server, ActionTutorialReward, `{"reward":"parting_gift"}`); refusalCode(err) != Unavailable {
		t.Errorf("second gift: %v", err)
	}
	if _, _, err := act(t, raw, server, "mint_gold", `{}`); err == nil || !strings.Contains(err.Error(), "no action") {
		t.Errorf("unknown action: %v", err)
	}
}

func TestLocalSaleOrCraftUploadedFirstStillReachesItsAction(t *testing.T) {
	c := mustCatalog(t)
	stored := profile.Record{Profile: gameProfile(t, func(doc map[string]any) {
		section(doc, "gear")["heroes"] = []any{map[string]any{"instance_id": 2, "ascended": false,
			"prestige_unlocked": false, "slots": []any{"templar_longsword"}}}
	}), Revision: 1}
	sold := gameProfile(t, func(doc map[string]any) {
		section(doc, "gear", "inventory")["templar_longsword"] = 1
		section(doc, "roster")["additions"] = section(doc, "roster")["additions"].([]any)[:1]
		runes := section(doc, "runes")
		runes["items"] = append(runes["items"].([]any)[2:], map[string]any{
			"uuid": 4, "def": "rune-greed-2", "expire_at": 0, "inventory_expire_at": 1900000000,
		})
	})
	server := profile.ServerState{}
	merged, err := MergeUpload(c, stored, &server, sold, ruleNow)
	if err != nil {
		t.Fatal(err)
	}
	doc := asMap(t, merged)
	if number(t, doc, "gear", "inventory", "templar_longsword") != 2 {
		t.Errorf("the sword to sell was not kept for the sale action, or the consumed hero's sword came back: %v",
			doc["gear"])
	}
	if items := section(doc, "runes")["items"].([]any); len(items) != 3 {
		t.Errorf("the idle runes to craft were not kept for the craft action: %v", items)
	}
	_, _ = mustAct(t, merged, &server, ActionGearSell, `{"gear":"templar_longsword"}`)
	_, _ = mustAct(t, merged, &server, ActionRuneCraft, `{"pairs":[[1,2]]}`)
}
