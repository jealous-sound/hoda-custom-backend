package catalog

import (
	"fmt"
	"slices"
)

const (
	TimedQuestsFile      = "timed_quests.json"
	supportedTimedQuests = 1

	QuestStatBattles             = "battles"
	QuestStatBattleWins          = "battle_wins"
	QuestStatCampaignWins        = "campaign_wins"
	QuestStatPvpBattles          = "pvp_battles"
	QuestStatPvpWins             = "pvp_wins"
	QuestStatNexusWins           = "nexus_wins"
	QuestStatCrits               = "crits"
	QuestStatFactionHits         = "faction_hits"
	QuestStatConsumed            = "consumed"
	QuestStatSummoned            = "summoned"
	QuestStatExpeditionsSent     = "expeditions_sent"
	QuestStatExpeditionsReturned = "expeditions_returned"
	QuestStatDailyQuests         = "daily_quests"
	QuestStatActiveDays          = "active_days"
)

var QuestStats = []string{
	QuestStatBattles, QuestStatBattleWins, QuestStatCampaignWins, QuestStatPvpBattles, QuestStatPvpWins,
	QuestStatNexusWins, QuestStatCrits, QuestStatFactionHits, QuestStatConsumed, QuestStatSummoned,
	QuestStatExpeditionsSent, QuestStatExpeditionsReturned, QuestStatDailyQuests, QuestStatActiveDays,
}

type TimedQuest struct {
	ID     string `json:"id"`
	Stat   string `json:"stat"`
	Target int64  `json:"target"`
	Points int64  `json:"points"`
	Gold   int64  `json:"gold"`
	Gems   int64  `json:"gems"`
}

type TimedQuestSlot struct {
	ID     string
	Quests []TimedQuest
}

type TimedQuestChest struct {
	ID     string
	Points int64
	Gold   int64
	Gems   int64
	Drops  []ExpeditionDrop
}

func (c TimedQuestChest) HeroDropLimit() int {
	return Expedition{Drops: c.Drops}.HeroDropLimit()
}

type TimedQuestRules struct {
	UnlockLevel  int
	DailySwaps   int
	Slots        []TimedQuestSlot
	Weekly       []TimedQuest
	DailyChests  []TimedQuestChest
	WeeklyChests []TimedQuestChest
}

func (r TimedQuestRules) SlotIndex(id string) int {
	return slices.IndexFunc(r.Slots, func(slot TimedQuestSlot) bool { return slot.ID == id })
}

func (r TimedQuestRules) DailyQuest(id string) (slot, pool int, ok bool) {
	for slot, def := range r.Slots {
		if pool := slices.IndexFunc(def.Quests, func(q TimedQuest) bool { return q.ID == id }); pool >= 0 {
			return slot, pool, true
		}
	}
	return 0, 0, false
}

func (r TimedQuestRules) WeeklyQuest(id string) int {
	return slices.IndexFunc(r.Weekly, func(q TimedQuest) bool { return q.ID == id })
}

func chestIndex(chests []TimedQuestChest, id string) int {
	return slices.IndexFunc(chests, func(c TimedQuestChest) bool { return c.ID == id })
}

func (r TimedQuestRules) DailyChest(id string) int {
	return chestIndex(r.DailyChests, id)
}

func (r TimedQuestRules) WeeklyChest(id string) int {
	return chestIndex(r.WeeklyChests, id)
}

type rawTimedQuestChest struct {
	ID     string              `json:"id"`
	Points int64               `json:"points"`
	Gold   int64               `json:"gold"`
	Gems   int64               `json:"gems"`
	Drops  []rawExpeditionDrop `json:"drops"`
}

type rawTimedQuestPeriod struct {
	Swaps  int                  `json:"swaps"`
	Slots  []rawTimedQuestSlot  `json:"slots"`
	Quests []TimedQuest         `json:"quests"`
	Chests []rawTimedQuestChest `json:"chests"`
}

type rawTimedQuestSlot struct {
	ID     string       `json:"id"`
	Quests []TimedQuest `json:"quests"`
}

func checkTimedQuest(quest TimedQuest, seen map[string]bool) error {
	if quest.ID == "" || seen[quest.ID] {
		return fmt.Errorf("quest id %q is empty or repeated", quest.ID)
	}
	seen[quest.ID] = true
	if !slices.Contains(QuestStats, quest.Stat) {
		return fmt.Errorf("quest %s counts unknown stat %q", quest.ID, quest.Stat)
	}
	if quest.Target <= 0 || quest.Points < 0 || quest.Gold < 0 || quest.Gems < 0 {
		return fmt.Errorf("quest %s has a bad target or reward", quest.ID)
	}
	return nil
}

func parseTimedQuestChests(raws []rawTimedQuestChest, seen map[string]bool) ([]TimedQuestChest, error) {
	chests := []TimedQuestChest{}
	lastPoints := int64(0)
	for _, raw := range raws {
		chest := TimedQuestChest{ID: raw.ID, Points: raw.Points, Gold: raw.Gold, Gems: raw.Gems}
		if raw.ID == "" || seen[raw.ID] || raw.Points <= lastPoints || raw.Gold < 0 || raw.Gems < 0 {
			return nil, fmt.Errorf("chest %q is out of order, repeated or pays less than nothing", raw.ID)
		}
		seen[raw.ID] = true
		lastPoints = raw.Points
		for _, rawDrop := range raw.Drops {
			drop, err := parseExpeditionDrop(rawDrop)
			if err != nil {
				return nil, fmt.Errorf("chest %s: %w", raw.ID, err)
			}
			chest.Drops = append(chest.Drops, drop)
		}
		chests = append(chests, chest)
	}
	return chests, nil
}

func (c *Catalog) loadTimedQuests(read func(name string) ([]byte, error)) error {
	var raw struct {
		Version     int                 `json:"version"`
		UnlockLevel int                 `json:"unlock_level"`
		Daily       rawTimedQuestPeriod `json:"daily"`
		Weekly      rawTimedQuestPeriod `json:"weekly"`
	}
	if err := decodeFile(read, TimedQuestsFile, &raw); err != nil {
		return err
	}
	if raw.Version != supportedTimedQuests {
		return fmt.Errorf("%s version %d, want %d", TimedQuestsFile, raw.Version, supportedTimedQuests)
	}
	if raw.UnlockLevel < 1 || raw.Daily.Swaps < 0 || len(raw.Daily.Slots) == 0 {
		return fmt.Errorf("%s needs an unlock level, swaps and daily slots", TimedQuestsFile)
	}
	rules := TimedQuestRules{UnlockLevel: raw.UnlockLevel, DailySwaps: raw.Daily.Swaps, Weekly: raw.Weekly.Quests}
	seen := map[string]bool{}
	for _, slot := range raw.Daily.Slots {
		if slot.ID == "" || len(slot.Quests) == 0 || rules.SlotIndex(slot.ID) >= 0 {
			return fmt.Errorf("%s slot %q is empty or repeated", TimedQuestsFile, slot.ID)
		}
		for _, quest := range slot.Quests {
			if err := checkTimedQuest(quest, seen); err != nil {
				return fmt.Errorf("%s: %w", TimedQuestsFile, err)
			}
		}
		rules.Slots = append(rules.Slots, TimedQuestSlot{ID: slot.ID, Quests: slot.Quests})
	}
	for _, quest := range rules.Weekly {
		if err := checkTimedQuest(quest, seen); err != nil {
			return fmt.Errorf("%s: %w", TimedQuestsFile, err)
		}
	}
	var err error
	if rules.DailyChests, err = parseTimedQuestChests(raw.Daily.Chests, seen); err != nil {
		return fmt.Errorf("%s: %w", TimedQuestsFile, err)
	}
	if rules.WeeklyChests, err = parseTimedQuestChests(raw.Weekly.Chests, seen); err != nil {
		return fmt.Errorf("%s: %w", TimedQuestsFile, err)
	}
	c.TimedQuests = rules
	return nil
}
