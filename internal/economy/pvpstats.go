package economy

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
)

const (
	pvpBotSalt         = 0x7A3C11
	pvpSquadSalt       = 0x2F90D3
	pvpDriftSalt       = 0x1C8A77
	pvpDefenseSalt     = 0x3D6F02
	pvpHistoryCapacity = 30
	pvpHoldSeconds     = 7 * secondsPerDay
	secondsPerDay      = 86400
)

var pvpHoldThresholds = [...]int{500, 750, 1000, 1500, 2000}

type pvpBattle struct {
	At               int64  `json:"at"`
	Bot              int    `json:"bot"`
	Squad            int    `json:"squad"`
	Level            int    `json:"level"`
	OpponentTrophies int    `json:"opponent_trophies"`
	TrophyDelta      int    `json:"trophy_delta"`
	Gold             int64  `json:"gold"`
	XP               int64  `json:"xp"`
	Banners          int    `json:"banners"`
	Seed             uint32 `json:"seed"`
	Defense          bool   `json:"defense"`
	Won              bool   `json:"won"`
	Revenged         bool   `json:"revenged"`
	Opponent         string `json:"opponent,omitempty"`
}

type pvpStats struct {
	Trophies         int                           `json:"trophies"`
	BestTrophies     int                           `json:"best_trophies"`
	Wins             int                           `json:"wins"`
	Losses           int                           `json:"losses"`
	WinStreak        int                           `json:"win_streak"`
	BestWinStreak    int                           `json:"best_win_streak"`
	TrophiesWon      int                           `json:"trophies_won"`
	DefenseWins      int                           `json:"defense_wins"`
	DefenseLosses    int                           `json:"defense_losses"`
	DefenseCheckedAt int64                         `json:"defense_checked_at"`
	HeldSince        [len(pvpHoldThresholds)]int64 `json:"held_since"`
	History          []pvpBattle                   `json:"history"`
}

var pvpClientFieldDefaults = map[string]func() any{
	"trophies_seen": func() any { return json.Number("0") },
	"nexts_used":    func() any { return json.Number("0") },
	"nexts_day":     func() any { return json.Number("0") },
	"event": func() any {
		return map[string]any{"cycle": json.Number("0"), "banners": json.Number("0"),
			"milestones_paid": json.Number("0"), "buffed": false}
	},
	"event_results": func() any { return []any{} },
}

func readPvpStats(doc document) pvpStats {
	stats := pvpStats{History: []pvpBattle{}}
	section, ok := doc.optionalSection("pvp")
	if !ok {
		return stats
	}
	raw, err := json.Marshal(map[string]any(section))
	if err == nil {
		_ = json.Unmarshal(raw, &stats)
	}
	if stats.History == nil {
		stats.History = []pvpBattle{}
	}
	return stats
}

func (s *pvpStats) write(doc document) error {
	section, err := doc.section("pvp")
	if err != nil {
		return refuse(BadProfile, "%v", err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var fields map[string]any
	if err := decoder.Decode(&fields); err != nil {
		return err
	}
	for name, value := range fields {
		section[name] = value
	}
	for name, fallback := range pvpClientFieldDefaults {
		if _, present := section[name]; !present {
			section[name] = fallback()
		}
	}
	return nil
}

func (s *pvpStats) holdUpdate(now int64) {
	for i, threshold := range pvpHoldThresholds {
		switch {
		case s.Trophies <= threshold:
			s.HeldSince[i] = 0
		case s.HeldSince[i] == 0:
			s.HeldSince[i] = now
		}
	}
}

func (s *pvpStats) held(now int64) int64 {
	held := 0
	for i, threshold := range pvpHoldThresholds {
		if s.HeldSince[i] > 0 && now-s.HeldSince[i] >= pvpHoldSeconds {
			held = threshold
		}
	}
	return int64(held)
}

func (s *pvpStats) applyTrophies(delta int, now int64) int {
	before := s.Trophies
	s.Trophies = max(before+delta, 0)
	s.BestTrophies = max(s.BestTrophies, s.Trophies)
	s.holdUpdate(now)
	return s.Trophies - before
}

func (s *pvpStats) push(battle pvpBattle) {
	s.History = slices.Insert(s.History, 0, battle)
	if len(s.History) > pvpHistoryCapacity {
		s.History = s.History[:pvpHistoryCapacity]
	}
}

func (s *pvpStats) countAttack(won bool, now int64) {
	if s.Wins+s.Losses == 0 {
		s.DefenseCheckedAt = now
	}
	if won {
		s.Wins++
		s.WinStreak++
	} else {
		s.Losses++
		s.WinStreak = 0
	}
	s.BestWinStreak = max(s.BestWinStreak, s.WinStreak)
}

type pvpOpponent struct {
	bot      int
	squad    int
	level    int
	trophies int
	seed     uint32
	name     string
}

func nonZeroSeed(seed uint32) uint32 {
	return max(seed, 1)
}

func pvpDay(at int64) int64 {
	return at / secondsPerDay
}

func trophyGain(rules *catalog.PvpRules, trophies, opponent int) int {
	t := rules.Trophies
	return min(max(t.BaseGain+(opponent-trophies)/t.PointsPerStep, t.MinGain), t.MaxGain)
}

func trophyLoss(rules *catalog.PvpRules, trophies, opponent int) int {
	t := rules.Trophies
	return min(max(t.BaseLoss+(trophies-opponent)/t.PointsPerStep, t.MinLoss), t.MaxLoss)
}

func botTrophies(rules *catalog.PvpRules, bot int, day int64) int {
	drift := rules.Matchmaking.BotDailyDrift
	seed := pvpMix(uint32(bot), uint32(day))
	return max(rules.Bots[bot].Trophies+pvpRoll(seed, pvpDriftSalt, drift*2+1)-drift, 0)
}

func pickBot(rules *catalog.PvpRules, trophies int, day int64, seed uint32) int {
	inWindow := func(bot int) bool {
		delta := botTrophies(rules, bot, day) - trophies
		return delta >= -rules.Matchmaking.WindowBelow && delta <= rules.Matchmaking.WindowAbove
	}
	candidates, closest, closestDistance := 0, 0, -1
	for bot := range rules.Bots {
		if inWindow(bot) {
			candidates++
		}
		distance := botTrophies(rules, bot, day) - trophies
		distance = max(distance, -distance)
		if closestDistance < 0 || distance < closestDistance {
			closest, closestDistance = bot, distance
		}
	}
	if candidates == 0 {
		return closest
	}
	pick := pvpRoll(seed, pvpBotSalt, candidates)
	for bot := range rules.Bots {
		if inWindow(bot) {
			if pick == 0 {
				return bot
			}
			pick--
		}
	}
	return closest
}

func targetRating(rules *catalog.PvpRules, level, trophyLead int) int64 {
	squads := rules.Squads
	l := int64(level)
	base := squads.RatingBase + squads.RatingPerLevel*l + squads.RatingPerLevelSquared*l*l
	shift := min(max(trophyLead/squads.TrophiesPerShiftPct, -squads.RatingMaxShiftPct), squads.RatingMaxShiftPct)
	return base * int64(percent+shift) / percent
}

func pickSquad(rules *catalog.PvpRules, rating int64, seed uint32) int {
	pool := rules.SquadPool
	distance := func(index int) int64 {
		delta := int64(pool[index].Rating) - rating
		return max(delta, -delta)
	}
	nearest := 0
	for index := 1; index < len(pool); index++ {
		if distance(index) < distance(nearest) {
			nearest = index
		}
	}
	candidates := rules.Squads.Candidates
	first := min(max(nearest-candidates/2, 0), max(len(pool)-candidates, 0))
	return first + pvpRoll(seed, pvpSquadSalt, min(candidates, len(pool)))
}

func (p *player) botOpponent(bot, trophies int, day int64, seed uint32) pvpOpponent {
	rules := &p.catalog.Pvp
	seed = nonZeroSeed(seed)
	opponent := pvpOpponent{bot: bot, seed: seed, trophies: botTrophies(rules, bot, day)}
	opponent.level = pvpOpponentLevel(rules, p.account.Level, seed)
	opponent.squad = pickSquad(rules, targetRating(rules, p.account.Level, opponent.trophies-trophies), seed)
	return opponent
}

func (p *player) generatedOpponent(trophies int, day int64, seed uint32) pvpOpponent {
	return p.botOpponent(pickBot(&p.catalog.Pvp, trophies, day, nonZeroSeed(seed)), trophies, day, seed)
}

func (p *player) foughtBotOpponent(request BattleRequest, trophies int) pvpOpponent {
	today := pvpDay(p.now.Unix())
	if request.Bot != nil {
		for _, day := range []int64{today, today - 1} {
			if pickBot(&p.catalog.Pvp, trophies, day, nonZeroSeed(request.Seed)) == *request.Bot {
				return p.botOpponent(*request.Bot, trophies, day, request.Seed)
			}
		}
	}
	return p.generatedOpponent(trophies, today, request.Seed)
}

func (p *player) eventOpponent(request BattleRequest, trophies int) pvpOpponent {
	today := pvpDay(p.now.Unix())
	if request.Bot != nil && *request.Bot >= 0 && *request.Bot < len(p.catalog.Pvp.Bots) {
		return p.botOpponent(*request.Bot, trophies, today, request.Seed)
	}
	return p.generatedOpponent(trophies, today, request.Seed)
}

func (s *pvpStats) revengeTarget(request BattleRequest) *pvpBattle {
	for i := range s.History {
		battle := &s.History[i]
		if battle.Defense && !battle.Won && !battle.Revenged && battle.At == request.RevengeAt &&
			battle.Seed == nonZeroSeed(request.Seed) {
			return battle
		}
	}
	return nil
}

func (p *player) pvpOpponent(request BattleRequest, stats *pvpStats) (pvpOpponent, error) {
	exclusive := 0
	for _, chosen := range []bool{request.Event, request.Duel != "", request.RevengeAt != 0} {
		if chosen {
			exclusive++
		}
	}
	if exclusive > 1 {
		return pvpOpponent{}, refuse(Unavailable, "a battle is one of an event battle, a duel or a revenge")
	}
	switch {
	case request.Duel != "":
		match, err := p.duels.DuelOpponent(request.Duel)
		if err != nil {
			return pvpOpponent{}, err
		}
		return pvpOpponent{
			level: min(max(match.Level, 1), pvpMaxLevel), trophies: max(match.Trophies, 0),
			seed: nonZeroSeed(request.Seed), name: match.Name,
		}, nil
	case request.RevengeAt != 0:
		target := stats.revengeTarget(request)
		if target == nil {
			return pvpOpponent{}, refuse(Unavailable, "no lost defense at %d to revenge", request.RevengeAt)
		}
		target.Revenged = true
		return pvpOpponent{
			bot: target.Bot, squad: target.Squad, level: min(max(target.Level, 1), pvpMaxLevel),
			trophies: target.OpponentTrophies, seed: target.Seed,
		}, nil
	case request.Event:
		return p.eventOpponent(request, stats.Trophies), nil
	}
	return p.foughtBotOpponent(request, stats.Trophies), nil
}

func (p *player) recordAttack(stats *pvpStats, opponent pvpOpponent, won, event bool, banners int) int {
	rules := &p.catalog.Pvp
	now := p.now.Unix()
	delta := 0
	if !event {
		change := -trophyLoss(rules, stats.Trophies, opponent.trophies)
		if won {
			change = trophyGain(rules, stats.Trophies, opponent.trophies)
		}
		delta = stats.applyTrophies(change, now)
	}
	stats.countAttack(won, now)
	if delta > 0 {
		stats.TrophiesWon += delta
	}
	stats.push(pvpBattle{
		At: now, Bot: opponent.bot, Squad: opponent.squad, Level: opponent.level,
		OpponentTrophies: opponent.trophies, TrophyDelta: delta, Gold: pvpGold(rules, opponent.level, won),
		XP: pvpXP(rules, opponent.level, won), Banners: banners, Seed: opponent.seed, Won: won,
		Opponent: opponent.name,
	})
	return delta
}

func (p *player) simulateDefense(stats *pvpStats) {
	if stats.Wins+stats.Losses == 0 {
		return
	}
	rules := &p.catalog.Pvp
	defense := rules.Defense
	now := p.now.Unix()
	if stats.DefenseCheckedAt <= 0 || stats.DefenseCheckedAt > now {
		stats.DefenseCheckedAt = now
		return
	}
	due := (now - stats.DefenseCheckedAt) / defense.AttackIntervalSeconds
	if due <= 0 {
		return
	}
	attacks := min(due, defense.MaxAttacksPerLogin)
	for i := range attacks {
		at := now - (attacks-1-i)*defense.AttackIntervalSeconds
		seed := pvpMix(uint32(at), pvpDefenseSalt+uint32(i))
		opponent := p.generatedOpponent(stats.Trophies, pvpDay(at), seed)
		won := pvpRoll(seed, pvpDefenseSalt, percent) < defense.WinPct
		stake := trophyLoss(rules, stats.Trophies, opponent.trophies) * defense.LossSharePct / percent
		if !won {
			stake = -stake
		}
		delta := stats.applyTrophies(stake, at)
		if won {
			stats.DefenseWins++
		} else {
			stats.DefenseLosses++
		}
		stats.push(pvpBattle{
			At: at, Bot: opponent.bot, Squad: opponent.squad, Level: opponent.level,
			OpponentTrophies: opponent.trophies, TrophyDelta: delta, Seed: opponent.seed, Defense: true, Won: won,
		})
	}
	stats.DefenseCheckedAt += due * defense.AttackIntervalSeconds
}

func (p *player) DefendPvp() (BattleResult, error) {
	stats := readPvpStats(p.doc)
	p.simulateDefense(&stats)
	return BattleResult{}, stats.write(p.doc)
}

func keepPvp(next, stored document) error {
	stats := readPvpStats(stored)
	return stats.write(next)
}

func originsMember(name string) bool {
	for _, origin := range originsNames {
		if strings.Contains(name, origin) {
			return true
		}
	}
	return false
}
