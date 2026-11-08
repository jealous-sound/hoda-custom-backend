package economy

import (
	"maps"
	"slices"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
)

const (
	questsSection          = "timed_quests"
	questSecondsPerDay     = 86400
	questDaysPerWeek       = 7
	questEpochDaysToMonday = 3
	questDayMix            = uint32(2654435761)
	questSlotMix           = uint32(40503)

	questFieldDay           = "day"
	questFieldWeek          = "week"
	questFieldDaily         = "daily"
	questFieldWeekly        = "weekly"
	questFieldDailyClaimed  = "daily_claimed"
	questFieldWeeklyClaimed = "weekly_claimed"
	questFieldSwaps         = "daily_swaps"
	questFieldSwapsUsed     = "swaps_used"
	questFieldDailyChests   = "daily_chests"
	questFieldWeeklyChests  = "weekly_chests"
)

var questSectionFields = []string{
	questFieldDay, questFieldWeek, questFieldDaily, questFieldWeekly, questFieldDailyClaimed,
	questFieldWeeklyClaimed, questFieldSwaps, questFieldSwapsUsed, questFieldDailyChests, questFieldWeeklyChests,
}

type questPeriod int

const (
	questDaily questPeriod = iota
	questWeekly
	questPeriods
)

type questCount struct {
	stat   string
	amount int64
}

type questState struct {
	section   document
	day       int64
	week      int64
	counts    [questPeriods]map[string]int64
	claimed   [questPeriods][]string
	chests    [questPeriods][]string
	swaps     map[string]int64
	swapsUsed int64
}

func questDay(at int64) int64 {
	if at >= 0 {
		return at / questSecondsPerDay
	}
	return (at+1)/questSecondsPerDay - 1
}

func questWeek(at int64) int64 {
	return (questDay(at) + questEpochDaysToMonday) / questDaysPerWeek
}

func questDailyPool(day int64, slot, offset, size int) int {
	r := rng{state: uint32(day)*questDayMix + uint32(slot+1)*questSlotMix}
	return (r.below(size) + offset) % size
}

func readQuestCounts(section document, field string) (map[string]int64, error) {
	counts := map[string]int64{}
	stored, _ := section.optionalSection(field)
	for stat, value := range stored {
		count, err := numberValue(value, field+"."+stat)
		if err != nil {
			return nil, err
		}
		counts[stat] = max(count, 0)
	}
	return counts, nil
}

func readQuestIDs(section document, field string) ([]string, error) {
	if _, ok := section[field]; !ok {
		return []string{}, nil
	}
	items, err := section.array(field)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		id, ok := item.(string)
		if !ok {
			return nil, refuse(BadProfile, "%s holds a non-string", field)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func loadQuests(doc document, now time.Time) (*questState, error) {
	section, err := doc.section(questsSection)
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	q := &questState{section: section}
	if q.day, err = section.optionalInt64(questFieldDay); err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	if q.week, err = section.optionalInt64(questFieldWeek); err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	if q.swapsUsed, err = section.optionalInt64(questFieldSwapsUsed); err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	fields := [questPeriods]struct{ counts, claimed, chests string }{
		{questFieldDaily, questFieldDailyClaimed, questFieldDailyChests},
		{questFieldWeekly, questFieldWeeklyClaimed, questFieldWeeklyChests},
	}
	for period, names := range fields {
		if q.counts[period], err = readQuestCounts(section, names.counts); err != nil {
			return nil, refuse(BadProfile, "%v", err)
		}
		if q.claimed[period], err = readQuestIDs(section, names.claimed); err != nil {
			return nil, refuse(BadProfile, "%v", err)
		}
		if q.chests[period], err = readQuestIDs(section, names.chests); err != nil {
			return nil, refuse(BadProfile, "%v", err)
		}
	}
	if q.swaps, err = readQuestCounts(section, questFieldSwaps); err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	q.roll(now)
	return q, nil
}

func (q *questState) roll(now time.Time) {
	if day := questDay(now.Unix()); q.day != day {
		q.day = day
		q.counts[questDaily] = map[string]int64{}
		q.claimed[questDaily] = []string{}
		q.chests[questDaily] = []string{}
		q.swaps = map[string]int64{}
		q.swapsUsed = 0
	}
	if week := questWeek(now.Unix()); q.week != week {
		q.week = week
		q.counts[questWeekly] = map[string]int64{}
		q.claimed[questWeekly] = []string{}
		q.chests[questWeekly] = []string{}
	}
}

func (q *questState) add(stat string, amount int64) {
	if amount <= 0 {
		return
	}
	firstBattleToday := stat == catalog.QuestStatBattles && q.counts[questDaily][stat] == 0
	for period := range questPeriods {
		q.counts[period][stat] = saturatingAdd(q.counts[period][stat], amount)
	}
	if firstBattleToday {
		q.add(catalog.QuestStatActiveDays, 1)
	}
}

func questCountsObject(counts map[string]int64) map[string]any {
	object := map[string]any{}
	for _, stat := range slices.Sorted(maps.Keys(counts)) {
		if counts[stat] > 0 {
			object[stat] = numberArray([]int64{counts[stat]})[0]
		}
	}
	return object
}

func questIDsArray(ids []string) []any {
	items := make([]any, len(ids))
	for i, id := range ids {
		items[i] = id
	}
	return items
}

func (q *questState) store() {
	q.section.setInt(questFieldDay, q.day)
	q.section.setInt(questFieldWeek, q.week)
	q.section.setInt(questFieldSwapsUsed, q.swapsUsed)
	q.section[questFieldDaily] = questCountsObject(q.counts[questDaily])
	q.section[questFieldWeekly] = questCountsObject(q.counts[questWeekly])
	q.section[questFieldSwaps] = questCountsObject(q.swaps)
	q.section[questFieldDailyClaimed] = questIDsArray(q.claimed[questDaily])
	q.section[questFieldWeeklyClaimed] = questIDsArray(q.claimed[questWeekly])
	q.section[questFieldDailyChests] = questIDsArray(q.chests[questDaily])
	q.section[questFieldWeeklyChests] = questIDsArray(q.chests[questWeekly])
}

func countQuests(doc document, now time.Time, counts ...questCount) error {
	q, err := loadQuests(doc, now)
	if err != nil {
		return err
	}
	for _, count := range counts {
		q.add(count.stat, count.amount)
	}
	q.store()
	return nil
}

func (p *player) countQuests(counts ...questCount) error {
	return countQuests(p.doc, p.now, counts...)
}

func boolCount(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func (p *player) recordQuestBattle(mode string, won bool, crits int, factionHit bool) error {
	counts := []questCount{
		{catalog.QuestStatBattles, 1},
		{catalog.QuestStatCrits, int64(min(max(crits, 0), maxCreditedCrits))},
		{catalog.QuestStatFactionHits, boolCount(factionHit)},
		{catalog.QuestStatPvpBattles, boolCount(mode == BattlePvp)},
	}
	if won {
		wins := map[string]string{
			BattleCampaign: catalog.QuestStatCampaignWins,
			BattlePvp:      catalog.QuestStatPvpWins,
			BattleNexus:    catalog.QuestStatNexusWins,
		}
		counts = append(counts, questCount{catalog.QuestStatBattleWins, 1}, questCount{wins[mode], 1})
	}
	return p.countQuests(counts...)
}

func (q *questState) dailyPool(rules catalog.TimedQuestRules, slot int) int {
	quests := rules.Slots[slot].Quests
	return questDailyPool(q.day, slot, int(q.swaps[rules.Slots[slot].ID]), len(quests))
}

func (q *questState) points(rules catalog.TimedQuestRules, period questPeriod) int64 {
	points := int64(0)
	for _, id := range q.claimed[period] {
		if period == questWeekly {
			if index := rules.WeeklyQuest(id); index >= 0 {
				points += rules.Weekly[index].Points
			}
		} else if slot, pool, ok := rules.DailyQuest(id); ok {
			points += rules.Slots[slot].Quests[pool].Points
		}
	}
	return points
}

func (p *player) questsUnlocked() error {
	if p.account.Level < p.catalog.TimedQuests.UnlockLevel {
		return refuse(Unavailable, "quests open at level %d", p.catalog.TimedQuests.UnlockLevel)
	}
	return nil
}

type QuestClaimRequest struct {
	Quest string `json:"quest"`
}

func (p *player) questToClaim(q *questState, id string) (catalog.TimedQuest, questPeriod, error) {
	rules := p.catalog.TimedQuests
	if slot, pool, ok := rules.DailyQuest(id); ok {
		if q.dailyPool(rules, slot) != pool {
			return catalog.TimedQuest{}, 0, refuse(Unavailable, "%s is not one of today's quests", id)
		}
		return rules.Slots[slot].Quests[pool], questDaily, nil
	}
	if index := rules.WeeklyQuest(id); index >= 0 {
		return rules.Weekly[index], questWeekly, nil
	}
	return catalog.TimedQuest{}, 0, refuse(Unavailable, "no quest %q", id)
}

func (p *player) ClaimQuest(request QuestClaimRequest) (BattleResult, error) {
	if err := p.questsUnlocked(); err != nil {
		return BattleResult{}, err
	}
	q, err := loadQuests(p.doc, p.now)
	if err != nil {
		return BattleResult{}, err
	}
	quest, period, err := p.questToClaim(q, request.Quest)
	if err != nil {
		return BattleResult{}, err
	}
	if slices.Contains(q.claimed[period], quest.ID) {
		return BattleResult{}, refuse(Unavailable, "%s is already claimed", quest.ID)
	}
	if count := q.counts[period][quest.Stat]; count < quest.Target {
		return BattleResult{}, refuse(Unavailable, "%s is at %d of %d", quest.ID, count, quest.Target)
	}
	q.claimed[period] = append(q.claimed[period], quest.ID)
	if period == questDaily {
		q.add(catalog.QuestStatDailyQuests, 1)
	}
	q.store()
	result := BattleResult{Gold: quest.Gold, Gems: quest.Gems}
	p.account.Gold = saturatingAdd(p.account.Gold, quest.Gold)
	p.addGems(quest.Gems)
	return result, nil
}

type QuestSwapRequest struct {
	Slot string `json:"slot"`
}

func (p *player) SwapQuest(request QuestSwapRequest) (BattleResult, error) {
	if err := p.questsUnlocked(); err != nil {
		return BattleResult{}, err
	}
	rules := p.catalog.TimedQuests
	slot := rules.SlotIndex(request.Slot)
	if slot < 0 || len(rules.Slots[slot].Quests) < 2 {
		return BattleResult{}, refuse(Unavailable, "slot %q cannot be swapped", request.Slot)
	}
	q, err := loadQuests(p.doc, p.now)
	if err != nil {
		return BattleResult{}, err
	}
	if q.swapsUsed >= int64(rules.DailySwaps) {
		return BattleResult{}, refuse(CounterLimit, "no swaps left today")
	}
	current := rules.Slots[slot].Quests[q.dailyPool(rules, slot)]
	if slices.Contains(q.claimed[questDaily], current.ID) || q.counts[questDaily][current.Stat] >= current.Target {
		return BattleResult{}, refuse(Unavailable, "%s is already done", current.ID)
	}
	q.swaps[request.Slot]++
	q.swapsUsed++
	q.store()
	return BattleResult{}, nil
}

type QuestChestRequest struct {
	Chest string `json:"chest"`
}

func (p *player) OpenQuestChest(request QuestChestRequest) (BattleResult, error) {
	if err := p.questsUnlocked(); err != nil {
		return BattleResult{}, err
	}
	rules := p.catalog.TimedQuests
	period, chests := questDaily, rules.DailyChests
	index := rules.DailyChest(request.Chest)
	if index < 0 {
		period, chests, index = questWeekly, rules.WeeklyChests, rules.WeeklyChest(request.Chest)
	}
	if index < 0 {
		return BattleResult{}, refuse(Unavailable, "no chest %q", request.Chest)
	}
	chest := chests[index]
	q, err := loadQuests(p.doc, p.now)
	if err != nil {
		return BattleResult{}, err
	}
	if slices.Contains(q.chests[period], chest.ID) {
		return BattleResult{}, refuse(Unavailable, "%s is already open", chest.ID)
	}
	if points := q.points(rules, period); points < chest.Points {
		return BattleResult{}, refuse(Unavailable, "%s needs %d points, not %d", chest.ID, chest.Points, points)
	}
	if heroes := chest.HeroDropLimit(); heroes > p.rosterRoom() {
		return BattleResult{}, refuse(InventoryFull, "roster has no room for %d heroes", heroes)
	}
	q.chests[period] = append(q.chests[period], chest.ID)
	q.store()
	p.account.Gold = saturatingAdd(p.account.Gold, chest.Gold)
	p.addGems(chest.Gems)
	for _, drop := range chest.Drops {
		if p.rng.below(percent) < drop.Chance {
			if err := p.rollExpeditionDrop(drop); err != nil {
				return BattleResult{}, err
			}
		}
	}
	return BattleResult{Gold: chest.Gold, Gems: chest.Gems, Granted: p.granted}, nil
}
