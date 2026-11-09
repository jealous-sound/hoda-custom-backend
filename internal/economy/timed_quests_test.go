package economy

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

func questProfile(t *testing.T, level int, quests map[string]any) json.RawMessage {
	t.Helper()
	return gameProfile(t, func(doc map[string]any) {
		section(doc, "account")["level"] = level
		if quests != nil {
			doc[questsSection] = quests
		}
	})
}

func todaysQuests(t *testing.T, now time.Time) map[string]catalog.TimedQuest {
	t.Helper()
	rules := mustCatalog(t).TimedQuests
	today := map[string]catalog.TimedQuest{}
	for slot, def := range rules.Slots {
		today[def.ID] = def.Quests[questDailyPool(questDay(now.Unix()), slot, 0, len(def.Quests))]
	}
	return today
}

func questCounts(doc map[string]any, period string) map[string]any {
	counts, _ := section(doc, questsSection)[period].(map[string]any)
	return counts
}

func TestTimedQuestCatalogFollowsTheClientTable(t *testing.T) {
	rules := mustCatalog(t).TimedQuests
	if rules.UnlockLevel != 3 || rules.DailySwaps != 1 || len(rules.Slots) != 5 || len(rules.Weekly) != 5 {
		t.Fatalf("rules %+v", rules)
	}
	if len(rules.DailyChests) != 3 || len(rules.WeeklyChests) != 2 || rules.WeeklyChests[1].HeroDropLimit() != 1 {
		t.Errorf("chests %+v %+v", rules.DailyChests, rules.WeeklyChests)
	}
	pinned := map[int64][]int{20400: {1, 1, 2, 1, 0}, 20401: {0, 1, 2, 2, 2}, 20402: {1, 0, 0, 1, 0}}
	for day, picks := range pinned {
		for slot, want := range picks {
			if got := questDailyPool(day, slot, 0, len(rules.Slots[slot].Quests)); got != want {
				t.Errorf("day %d slot %d picks %d, the client picks %d", day, slot, got, want)
			}
		}
	}
}

func TestQuestCountsRollAtUtcMidnightAndOnMonday(t *testing.T) {
	doc := asMap(t, questProfile(t, 5, nil))
	monday := time.Unix(20402*questSecondsPerDay, 0)
	if err := countQuests(doc, monday, questCount{catalog.QuestStatBattles, 1}, questCount{catalog.QuestStatPvpWins, 2}); err != nil {
		t.Fatal(err)
	}
	if err := countQuests(doc, monday.Add(time.Hour), questCount{catalog.QuestStatBattles, 1}); err != nil {
		t.Fatal(err)
	}
	if err := countQuests(doc, monday.Add(25*time.Hour), questCount{catalog.QuestStatBattles, 1}); err != nil {
		t.Fatal(err)
	}
	daily, weekly := questCounts(doc, questFieldDaily), questCounts(doc, questFieldWeekly)
	if daily[catalog.QuestStatPvpWins] != nil || daily[catalog.QuestStatBattles] != json.Number("1") {
		t.Errorf("tuesday's counts %v", daily)
	}
	if weekly[catalog.QuestStatBattles] != json.Number("3") || weekly[catalog.QuestStatActiveDays] != json.Number("2") {
		t.Errorf("the week's counts %v", weekly)
	}
	if err := countQuests(doc, monday.Add(7*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(questCounts(doc, questFieldWeekly)) != 0 {
		t.Errorf("next monday kept %v", questCounts(doc, questFieldWeekly))
	}
}

func TestQuestClaimPaysTodaysFinishedQuestOnce(t *testing.T) {
	today := todaysQuests(t, ruleNow)
	campaign := today["campaign"]
	quests := map[string]any{
		"day": questDay(ruleNow.Unix()), "week": questWeek(ruleNow.Unix()),
		"daily": map[string]any{campaign.Stat: campaign.Target},
	}
	server := &profile.ServerState{}
	if _, _, err := act(t, questProfile(t, 2, quests), server, ActionQuestClaim, `{"quest":"`+campaign.ID+`"}`); refusalCode(err) != Unavailable {
		t.Fatalf("claimed below the unlock level: %v", err)
	}
	doc, result := mustAct(t, questProfile(t, 5, quests), server, ActionQuestClaim, `{"quest":"`+campaign.ID+`"}`)
	if result.Gold != campaign.Gold || number(t, doc, "account", "gold") != 1000+float64(campaign.Gold) {
		t.Errorf("paid %+v, gold %v", result, number(t, doc, "account", "gold"))
	}
	if questCounts(doc, questFieldWeekly)[catalog.QuestStatDailyQuests] != float64(1) {
		t.Errorf("weekly counts %v", questCounts(doc, questFieldWeekly))
	}
	if _, _, err := act(t, reencode(t, doc), server, ActionQuestClaim, `{"quest":"`+campaign.ID+`"}`); refusalCode(err) != Unavailable {
		t.Errorf("a second claim: %v", err)
	}
	pvp := today["pvp"]
	if _, _, err := act(t, reencode(t, doc), server, ActionQuestClaim, `{"quest":"`+pvp.ID+`"}`); refusalCode(err) != Unavailable {
		t.Errorf("an unfinished quest: %v", err)
	}
	for _, other := range mustCatalog(t).TimedQuests.Slots[0].Quests {
		if other.ID == campaign.ID {
			continue
		}
		withCounts := questProfile(t, 5, map[string]any{
			"day": questDay(ruleNow.Unix()), "daily": map[string]any{other.Stat: 99},
		})
		if _, _, err := act(t, withCounts, server, ActionQuestClaim, `{"quest":"`+other.ID+`"}`); refusalCode(err) != Unavailable {
			t.Errorf("claimed %s, which is not today's: %v", other.ID, err)
		}
	}
	yesterday := questProfile(t, 5, map[string]any{
		"day": questDay(ruleNow.Unix()) - 1, "daily": map[string]any{campaign.Stat: campaign.Target},
	})
	if _, _, err := act(t, yesterday, server, ActionQuestClaim, `{"quest":"`+campaign.ID+`"}`); refusalCode(err) != Unavailable {
		t.Errorf("yesterday's progress paid out: %v", err)
	}
}

func TestQuestSwapReplacesOneUnfinishedQuestADay(t *testing.T) {
	rules := mustCatalog(t).TimedQuests
	pvp := rules.SlotIndex("pvp")
	server := &profile.ServerState{}
	doc, _ := mustAct(t, questProfile(t, 5, nil), server, ActionQuestSwap, `{"slot":"pvp"}`)
	swapped := rules.Slots[pvp].Quests[questDailyPool(questDay(ruleNow.Unix()), pvp, 1, len(rules.Slots[pvp].Quests))]
	if section(doc, questsSection)[questFieldSwaps].(map[string]any)["pvp"] != float64(1) {
		t.Fatalf("swaps %v", section(doc, questsSection))
	}
	if _, _, err := act(t, reencode(t, doc), server, ActionQuestSwap, `{"slot":"campaign"}`); refusalCode(err) != CounterLimit {
		t.Errorf("a second swap: %v", err)
	}
	section(doc, questsSection)[questFieldDaily] = map[string]any{swapped.Stat: swapped.Target}
	doc, result := mustAct(t, reencode(t, doc), server, ActionQuestClaim, `{"quest":"`+swapped.ID+`"}`)
	if result.Gems != swapped.Gems {
		t.Errorf("the swapped quest paid %+v", result)
	}
	campaign := todaysQuests(t, ruleNow)["campaign"]
	done := questProfile(t, 5, map[string]any{
		"day": questDay(ruleNow.Unix()), "daily": map[string]any{campaign.Stat: campaign.Target},
	})
	if _, _, err := act(t, done, server, ActionQuestSwap, `{"slot":"campaign"}`); refusalCode(err) != Unavailable {
		t.Errorf("swapped a finished quest: %v", err)
	}
}

func TestQuestChestsNeedPointsAndRosterRoom(t *testing.T) {
	var claimed []any
	for _, quest := range todaysQuests(t, ruleNow) {
		claimed = append(claimed, quest.ID)
	}
	quests := map[string]any{"day": questDay(ruleNow.Unix()), "daily_claimed": claimed[:1]}
	server := &profile.ServerState{StoreRNG: 11}
	if _, _, err := act(t, questProfile(t, 5, quests), server, ActionQuestChest, `{"chest":"daily_chest_1"}`); refusalCode(err) != Unavailable {
		t.Fatalf("opened a chest with 20 points: %v", err)
	}
	quests["daily_claimed"] = claimed
	full := gameProfile(t, func(doc map[string]any) {
		doc[questsSection] = quests
		section(doc, "roster")["starter_count"] = RosterCapacity
	})
	if _, _, err := act(t, full, server, ActionQuestChest, `{"chest":"daily_chest_3"}`); refusalCode(err) != InventoryFull {
		t.Fatalf("a full roster: %v", err)
	}
	doc, result := mustAct(t, questProfile(t, 5, quests), server, ActionQuestChest, `{"chest":"daily_chest_3"}`)
	if len(result.Granted.Heroes) != 1 || result.Gems != 5 || number(t, doc, "account", "gems") != 205 {
		t.Fatalf("the chest paid %+v", result)
	}
	hero, _ := mustCatalog(t).Hero(result.Granted.Heroes[0].ID)
	if hero.Rarity > catalog.RarityEpic || !hasHero(doc, float64(result.Granted.Heroes[0].InstanceID)) {
		t.Errorf("rolled %+v", hero)
	}
	opened, _ := section(doc, questsSection)[questFieldDailyChests].([]any)
	if !slices.Contains(opened, any("daily_chest_3")) {
		t.Errorf("opened chests %v", opened)
	}
	if _, _, err := act(t, reencode(t, doc), server, ActionQuestChest, `{"chest":"daily_chest_3"}`); refusalCode(err) != Unavailable {
		t.Errorf("a second opening: %v", err)
	}
	doc, result = mustAct(t, reencode(t, doc), server, ActionQuestChest, `{"chest":"daily_chest_1"}`)
	if result.Gold != 1000 || len(result.Granted.Heroes) != 0 {
		t.Errorf("the first chest paid %+v", result)
	}
}

func TestQuestProgressCountsFromServerActions(t *testing.T) {
	server := &profile.ServerState{}
	doc, _ := mustAct(t, expeditionProfile(t, 5, nil), server, ActionExpeditionStart, `{"expedition":"lothering","heroes":[3]}`)
	doc, _, err := actAt(t, reencode(t, doc), server, ActionExpeditionClaim, `{"expeditions":["lothering"]}`, ruleNow.Add(15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	daily := questCounts(doc, questFieldDaily)
	if daily[catalog.QuestStatExpeditionsSent] != float64(1) || daily[catalog.QuestStatExpeditionsReturned] != float64(1) {
		t.Errorf("expedition counts %v", daily)
	}
	doc, _ = mustAct(t, reencode(t, doc), server, ActionConsume, `{"target":1,"seeds":[3,4]}`)
	if questCounts(doc, questFieldDaily)[catalog.QuestStatConsumed] != float64(2) {
		t.Errorf("consume counts %v", questCounts(doc, questFieldDaily))
	}
}

func TestUploadCannotWriteQuestProgress(t *testing.T) {
	campaign := todaysQuests(t, ruleNow)["campaign"]
	stored := questProfile(t, 5, map[string]any{"day": questDay(ruleNow.Unix()), "daily": map[string]any{}})
	forged := questProfile(t, 5, map[string]any{
		"day": questDay(ruleNow.Unix()), "daily": map[string]any{campaign.Stat: 99},
		"daily_claimed": []any{}, "swaps_used": -5,
	})
	merged := merge(t, stored, forged, &profile.ServerState{})
	if len(questCounts(merged, questFieldDaily)) != 0 || section(merged, questsSection)[questFieldSwapsUsed] != float64(0) {
		t.Errorf("the upload wrote %v", section(merged, questsSection))
	}
}
