package economy

import (
	"encoding/json"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const (
	pvpLevelSalt           = 0x51ED27
	pvpEventSalt           = 0x4E11A9
	pvpEventSkillJitterPct = 25
	pvpMaxLevel            = 100
	pvpResultsKept         = 8
	mixGolden              = 0x9E3779B1
	mixFirst               = 0x85EBCA77
	mixSecond              = 0xC2B2AE3D
)

func pvpMix(a, b uint32) uint32 {
	h := a ^ (b * mixGolden)
	h ^= h >> 15
	h *= mixFirst
	h ^= h >> 13
	h *= mixSecond
	return h ^ (h >> 16)
}

func pvpRoll(seed, salt uint32, count int) int {
	if count <= 0 {
		return 0
	}
	return int(pvpMix(seed, salt) % uint32(count))
}

func pvpOpponentLevel(rules *catalog.PvpRules, level int, seed uint32) int {
	if seed == 0 {
		seed = 1
	}
	spread := rules.Matchmaking.LevelSpread
	return min(max(level+pvpRoll(seed, pvpLevelSalt, spread*2+1)-spread, 1), pvpMaxLevel)
}

func pvpGold(rules *catalog.PvpRules, opponentLevel int, won bool) int64 {
	gold := int64(rules.Rewards.WinGoldBase + rules.Rewards.WinGoldPerLevel*opponentLevel)
	if won {
		return gold
	}
	return gold * int64(rules.Rewards.LossGoldPct) / 100
}

func pvpXP(rules *catalog.PvpRules, opponentLevel int, won bool) int64 {
	xp := int64(rules.Rewards.WinXPBase + rules.Rewards.WinXPPerLevel*opponentLevel)
	if won {
		return xp
	}
	return xp * int64(rules.Rewards.LossXPPct) / 100
}

func maxBannersPerWin(events *catalog.PvpEvents, banners int) int {
	ladder := events.LadderBanners
	step := max(ladder/events.WinBannersStepsToPeak, 1)
	midpoint := ladder / 2
	peak := events.WinBannersStart + events.WinBannersPeakBonus
	position := min(banners, ladder)
	if position <= midpoint {
		return events.WinBannersStart + position/step
	}
	return peak - (position-midpoint)/(step*events.WinBannersDeclineSlowdown)
}

func botBanners(rules *catalog.PvpRules, bot int, cycle, elapsed int64) int64 {
	bots := int64(len(rules.Bots))
	standing := int64(bot+1) * int64(bot+1)
	full := rules.Events.TopBotBanners * standing / (bots * bots)
	seed := pvpMix(uint32(bot), uint32(cycle))
	full = full * int64(100-pvpEventSkillJitterPct+pvpRoll(seed, pvpEventSalt, pvpEventSkillJitterPct*2+1)) / 100
	return full * elapsed / rules.Events.DurationSeconds
}

func pvpEventRank(rules *catalog.PvpRules, banners int, cycle, elapsed int64) int {
	rank := 1
	for bot := range rules.Bots {
		if botBanners(rules, bot, cycle, elapsed) > int64(banners) {
			rank++
		}
	}
	return rank
}

func (p *player) eventDetails(cycle int64) *catalog.PvpEventDetails {
	return p.catalog.Pvp.Events.ForCycle(cycle)
}

func (p *player) syncPvpEvent() {
	cycle := p.now.Unix() / p.catalog.Pvp.Events.DurationSeconds
	state := &p.server.PvpEvent
	if state.Cycle == cycle {
		return
	}
	if state.Banners > 0 {
		p.server.PvpResults = append(p.server.PvpResults, profile.PvpEventResult{
			Cycle: state.Cycle, Banners: state.Banners,
		})
		if extra := len(p.server.PvpResults) - pvpResultsKept; extra > 0 {
			p.server.PvpResults = append([]profile.PvpEventResult(nil), p.server.PvpResults[extra:]...)
		}
	}
	*state = profile.PvpEventState{Cycle: cycle}
}

func (p *player) payPvpReward(reward catalog.PvpReward, result *BattleResult) error {
	result.Gold = saturatingAdd(result.Gold, int64(reward.Gold))
	result.Gems = saturatingAdd(result.Gems, int64(reward.Gems))
	for _, prize := range reward.Prizes {
		var err error
		switch prize.Kind {
		case catalog.PrizeUnit:
			heroes := make([]string, prize.Count)
			for i := range heroes {
				heroes[i] = prize.ID
			}
			err = p.grantHeroes(heroes)
		case catalog.PrizeGear:
			err = p.grantGear(prize.ID, prize.Count)
		case catalog.PrizeTome:
			err = p.grantTomes(prize.Count)
		case catalog.PrizeVoucher:
			err = p.addVoucher(prize.Index, prize.Count)
			for range prize.Count {
				p.granted.Vouchers = append(p.granted.Vouchers, prize.Index)
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func rewardUnits(reward catalog.PvpReward) int {
	units := 0
	for _, prize := range reward.Prizes {
		if prize.Kind == catalog.PrizeUnit {
			units += prize.Count
		}
	}
	return units
}

func (p *player) payMilestones(result *BattleResult) error {
	state := &p.server.PvpEvent
	milestones := p.eventDetails(state.Cycle).Milestones
	room := p.rosterRoom()
	for state.MilestonesPaid < len(milestones) {
		next := milestones[state.MilestonesPaid]
		units := rewardUnits(next.PvpReward)
		if state.Banners < next.Banners || units > room {
			break
		}
		if err := p.payPvpReward(next.PvpReward, result); err != nil {
			return err
		}
		room -= units
		state.MilestonesPaid++
	}
	return nil
}

func pvpBannerPct(rules *catalog.PvpRules, trophies, opponentTrophies int) int {
	span := rules.Trophies.MaxGain - rules.Trophies.MinGain
	floor := rules.Events.WinBannersFloorPct
	if span <= 0 {
		return floor
	}
	gain := trophyGain(rules, trophies, opponentTrophies)
	return floor + (percent-floor)*(gain-rules.Trophies.MinGain)/span
}

func (p *player) startPvp(request BattleStartRequest) (battleStart, error) {
	stats := readPvpStats(p.doc)
	rival, enemy, changer, err := p.pvpRival(request, &stats)
	if err != nil {
		return battleStart{}, err
	}
	if err := stats.write(p.doc); err != nil {
		return battleStart{}, err
	}
	pending := profile.PendingBattle{PvpEvent: request.Event, Opponent: profile.PendingRival{
		Bot: rival.bot, Squad: rival.squad, Level: rival.level, Trophies: rival.trophies, Seed: rival.seed,
		Name: rival.name,
	}}
	if request.Event {
		pending.BannerPct = pvpBannerPct(&p.catalog.Pvp, stats.Trophies, rival.trophies)
	}
	return battleStart{pending: pending, enemy: enemy, changer: changer}, nil
}

func (p *player) settlePvp(pending profile.PendingBattle, outcome battle.Outcome) (BattleResult, error) {
	rules := &p.catalog.Pvp
	stats := readPvpStats(p.doc)
	rival := pvpOpponent{
		bot: pending.Opponent.Bot, squad: pending.Opponent.Squad, level: pending.Opponent.Level,
		trophies: pending.Opponent.Trophies, seed: pending.Opponent.Seed, name: pending.Opponent.Name,
	}
	result := BattleResult{}
	gold := pvpGold(rules, rival.level, outcome.Won)
	if pending.PvpEvent {
		p.syncPvpEvent()
		state := &p.server.PvpEvent
		earned := rules.Events.LossBanners
		if outcome.Won {
			earned = maxBannersPerWin(&rules.Events, state.Banners) * pending.BannerPct / percent
		}
		state.Banners += earned
		result.Banners = earned
		result.Cycle = state.Cycle
		p.count(statEventBanners, int64(earned))
		if outcome.SquadBuffed && !state.Buffed {
			state.Buffed = true
			p.count(statEventBuffedSquads, 1)
		}
		milestones := BattleResult{}
		if err := p.payMilestones(&milestones); err != nil {
			return BattleResult{}, err
		}
		gold = saturatingAdd(gold, milestones.Gold)
		result.Gems = milestones.Gems
		result.EventRank = pvpEventRank(rules, state.Banners, state.Cycle, p.now.Unix()%rules.Events.DurationSeconds)
	}
	result.TrophyDelta = p.recordAttack(&stats, rival, outcome.Won, pending.PvpEvent, result.Banners)
	p.simulateDefense(&stats)
	if err := stats.write(p.doc); err != nil {
		return BattleResult{}, err
	}
	xp := pvpXP(rules, rival.level, outcome.Won)
	return result, p.payBattle(&result, gold, xp, outcome.Crits, outcome.Won)
}

func ClaimGuildAward(c *catalog.Catalog, raw json.RawMessage, server *profile.ServerState, reward catalog.PvpReward,
	rank int, now time.Time,
) (json.RawMessage, BattleResult, error) {
	p, err := openPlayer(c, raw, server, now)
	if err != nil {
		return nil, BattleResult{}, err
	}
	if rewardUnits(reward) > p.rosterRoom() {
		return nil, BattleResult{}, refuse(InventoryFull, "roster has no room for the guild prizes")
	}
	result := BattleResult{EventRank: rank}
	if err := p.payPvpReward(reward, &result); err != nil {
		return nil, BattleResult{}, err
	}
	p.addGold(result.Gold)
	p.addGems(result.Gems)
	result.Granted = p.granted
	encoded, err := p.close()
	if err != nil {
		return nil, BattleResult{}, err
	}
	return encoded, result, nil
}

type EventClaimRequest struct {
	Cycle int64 `json:"cycle"`
}

func (p *player) ClaimPvpEvent(request EventClaimRequest) (BattleResult, error) {
	p.syncPvpEvent()
	rules := &p.catalog.Pvp
	for i := range p.server.PvpResults {
		entry := &p.server.PvpResults[i]
		if entry.Cycle != request.Cycle {
			continue
		}
		if entry.Claimed {
			return BattleResult{}, refuse(Unavailable, "event %d is already claimed", request.Cycle)
		}
		rank := pvpEventRank(rules, entry.Banners, entry.Cycle, rules.Events.DurationSeconds)
		result := BattleResult{EventRank: rank, Banners: entry.Banners}
		for _, reward := range p.eventDetails(entry.Cycle).RankRewards {
			if rank < reward.First || rank > reward.Last {
				continue
			}
			if rewardUnits(reward.PvpReward) > p.rosterRoom() {
				return BattleResult{}, refuse(InventoryFull, "roster has no room for the event prizes")
			}
			if err := p.payPvpReward(reward.PvpReward, &result); err != nil {
				return BattleResult{}, err
			}
			break
		}
		entry.Claimed = true
		p.addGold(result.Gold)
		p.addGems(result.Gems)
		result.Granted = p.granted
		return result, nil
	}
	return BattleResult{}, refuse(Unavailable, "no finished event %d to claim", request.Cycle)
}
