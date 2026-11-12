package economy

import (
	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const pvpMatchSalt = 0x6C2E95

type PvpMatchRequest struct {
	Event bool `json:"event"`
	Bot   *int `json:"bot"`
	Next  bool `json:"next"`
}

func (s *pvpStats) spendNext(limit int, day int64) bool {
	if s.NextsDay != day {
		s.NextsUsed, s.NextsDay = 0, day
	}
	if s.NextsUsed >= limit {
		return false
	}
	s.NextsUsed++
	return true
}

func pvpMatchSeed(roll uint32, event bool, bot int) uint32 {
	choice := uint32(bot+1) * 2
	if event {
		choice++
	}
	return nonZeroSeed(pvpMix(pvpMix(roll, pvpMatchSalt), choice))
}

func (p *player) pvpRollSeed() (uint32, error) {
	if p.server.PvpRoll == 0 {
		roll, err := p.seeds()
		if err != nil {
			return 0, err
		}
		p.server.PvpRoll = nonZeroSeed(roll)
	}
	return p.server.PvpRoll, nil
}

func (p *player) MatchPvp(request PvpMatchRequest) (BattleResult, error) {
	data := p.catalog.Battle
	bot := -1
	if request.Bot != nil {
		bot = *request.Bot
		if bot < 0 || bot >= data.PvpBotCount() {
			return BattleResult{}, refuse(Unavailable, "no pvp bot %d", bot)
		}
		if request.Next {
			return BattleResult{}, refuse(Unavailable, "a chosen bot has no next opponent")
		}
	}
	now := p.now.Unix()
	stats := readPvpStats(p.doc)
	if request.Next {
		if !stats.spendNext(p.catalog.Pvp.Matchmaking.DailyNexts, pvpDay(now)) {
			return BattleResult{}, refuse(CounterLimit, "no next opponents left today")
		}
		if err := stats.write(p.doc); err != nil {
			return BattleResult{}, err
		}
		p.server.PvpRoll = 0
	}
	roll, err := p.pvpRollSeed()
	if err != nil {
		return BattleResult{}, err
	}
	p.syncPvpEvent()
	seed := pvpMatchSeed(roll, request.Event, bot)
	level, day := p.account.Level, pvpDay(now)
	var opponent battle.PvpOpponent
	switch {
	case request.Bot != nil:
		opponent = data.PvpOpponentFromBot(bot, level, day, seed)
	case request.Event:
		opponent = data.PvpEventOpponent(level, p.server.PvpEvent.Banners, now, seed)
	default:
		opponent = data.PvpLadderOpponent(level, stats.Trophies, day, seed)
	}
	p.server.PvpMatches++
	match := profile.PvpMatch{
		ID: p.server.PvpMatches, Event: request.Event, Chosen: request.Bot != nil, Bot: opponent.Bot,
		Squad: opponent.Squad, Level: opponent.Level, Trophies: opponent.Trophies, Seed: opponent.Seed, At: now,
	}
	stored := match
	p.server.PvpMatch = &stored
	return BattleResult{Match: &match, Granted: p.granted}, nil
}

func (p *player) matchedRival(request BattleStartRequest) (pvpOpponent, battle.Side, *battle.Changer, error) {
	if request.Match == 0 {
		return pvpOpponent{}, battle.Side{}, nil, refuse(Unavailable, "match an opponent with pvp_match first")
	}
	match := p.server.PvpMatch
	if match == nil || match.ID != request.Match || match.Event != request.Event {
		return pvpOpponent{}, battle.Side{}, nil, refuse(OpponentChanged, "opponent %d is no longer offered",
			request.Match)
	}
	data := p.catalog.Battle
	cycle := data.PvpEventCycle(match.At)
	if match.Event && cycle != data.PvpEventCycle(p.now.Unix()) {
		return pvpOpponent{}, battle.Side{}, nil, refuse(OpponentChanged, "opponent %d was matched in an ended event",
			match.ID)
	}
	opponent := battle.PvpOpponent{
		Bot: match.Bot, Squad: match.Squad, Level: match.Level, Trophies: match.Trophies, Seed: match.Seed,
	}
	enemy, err := data.PvpRoster(opponent)
	if err != nil {
		return pvpOpponent{}, battle.Side{}, nil, refuse(Unavailable, "%v", err)
	}
	var changer *battle.Changer
	if match.Event {
		changer = data.PvpEventChanger(cycle)
	}
	return rivalOf(opponent), enemy, changer, nil
}

func (p *player) useUpPvpMatch() {
	p.server.PvpMatch = nil
	p.server.PvpRoll = 0
}
