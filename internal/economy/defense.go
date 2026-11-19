package economy

import (
	"encoding/json"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
)

const (
	AttackCooldown         RefusalCode = "attack_cooldown"
	pvpRepeatAttackSeconds             = secondsPerDay
)

type Strike struct {
	Defender         string
	Attacker         string
	AttackerName     string
	AttackerLevel    int
	AttackerTrophies int
	AttackerWon      bool
	Event            bool
	Trophies         int
	At               int64
}

func botDefenseTrophies(rules *catalog.PvpRules, botTrophies, defenderTrophies int, botWon bool) int {
	share := rules.Defense.LossSharePct
	if botWon {
		return -trophyGain(rules, botTrophies, defenderTrophies) * share / percent
	}
	return trophyLoss(rules, botTrophies, defenderTrophies) * share / percent
}

func (s *pvpStats) attackCooldownLeft(rival string, now int64) int64 {
	return max(s.Attacked[rival]+pvpRepeatAttackSeconds-now, 0)
}

func (s *pvpStats) noteAttack(rival string, now int64) {
	for id, at := range s.Attacked {
		if at+pvpRepeatAttackSeconds <= now {
			delete(s.Attacked, id)
		}
	}
	s.Attacked[rival] = now
}

func (s *pvpStats) mayAttack(rival string, now int64) error {
	if left := s.attackCooldownLeft(rival, now); left > 0 {
		return refuse(AttackCooldown, "%s was attacked less than a day ago; %d seconds left", rival, left)
	}
	return nil
}

func duelSide(data *battle.Data, duel DuelOpponent) battle.Side {
	side := battle.Side{
		Units: []battle.Unit{}, Runes: []string{}, HeroBuilds: duel.HeroBuilds, Renown: duel.Renown.Canonical(),
	}
	for _, unit := range duel.Units {
		if unit.Slot < 0 || unit.Slot >= battle.SlotCount || unit.ID == "" || !data.Knows(unit.ID) {
			continue
		}
		fielded := battle.Unit{
			Slot: unit.Slot, ID: unit.ID, Tier: battle.ParseTier(unit.Tier).String(), Level: max(unit.Level, 1),
		}
		if duel.HeroBuilds {
			fielded.ConsumeXP, fielded.Ascended = unit.ConsumeXP, unit.Ascended
			fielded.Gear, fielded.Fortify = unit.Gear, unit.Fortify
		}
		side.Units = append(side.Units, fielded)
	}
	for _, id := range duel.Runes {
		if data.RuneKnown(id) && len(side.Runes) < battle.RuneSlotCount {
			side.Runes = append(side.Runes, id)
		}
	}
	return canonicalSide(side)
}

func (p *player) firstSquad(at int64) (battle.Side, error) {
	raw, err := json.Marshal(p.doc)
	if err != nil {
		return battle.Side{}, err
	}
	defender, err := DuelOpponentFrom(p.catalog, "", raw, time.Unix(at, 0))
	if err != nil {
		return battle.Side{}, err
	}
	return duelSide(p.catalog.Battle, defender), nil
}

func (p *player) firstSquadHolds(attacker pvpOpponent, at int64) (bool, error) {
	defender, err := p.firstSquad(at)
	if err != nil || len(defender.Units) == 0 {
		return false, err
	}
	data := p.catalog.Battle
	enemy, err := data.PvpRoster(battle.PvpOpponent{
		Bot: attacker.bot, Squad: attacker.squad, Level: attacker.level, Trophies: attacker.trophies,
		Seed: attacker.seed,
	})
	if err != nil {
		return false, refuse(Unavailable, "%v", err)
	}
	outcome, err := data.Run(battle.Replay{
		Seed: attacker.seed, Player: defender, Enemy: enemy, StatBudget: data.PvpStatBudget(),
	})
	if err != nil {
		return false, refuse(Unavailable, "%v", err)
	}
	return outcome.Won, nil
}

func (p *player) strikeOn(rival pvpOpponent, attackerTrophies, attackerDelta int, won, event bool) *Strike {
	if rival.player == "" {
		return nil
	}
	return &Strike{
		Defender: rival.player, AttackerName: p.account.name(), AttackerLevel: p.account.Level,
		AttackerTrophies: attackerTrophies, AttackerWon: won, Event: event, Trophies: -attackerDelta,
		At: p.now.Unix(),
	}
}

func (r BattleResult) Strikes() []Strike {
	strikes := []Strike{}
	if r.Strike != nil {
		strikes = append(strikes, *r.Strike)
	}
	if r.Settled != nil {
		strikes = append(strikes, r.Settled.Strikes()...)
	}
	return strikes
}

func LandStrike(raw json.RawMessage, strike Strike) (json.RawMessage, error) {
	doc, err := parseDocument(raw)
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	stats := readPvpStats(doc)
	line := pvpBattle{
		At: strike.At, Level: strike.AttackerLevel, OpponentTrophies: strike.AttackerTrophies, Defense: true,
		Won: !strike.AttackerWon, Event: strike.Event, Opponent: strike.AttackerName, OpponentID: strike.Attacker,
		TrophyDelta: stats.applyTrophies(strike.Trophies, strike.At),
	}
	stats.countDefense(line.Won)
	stats.DefenseCheckedAt = max(stats.DefenseCheckedAt, strike.At)
	stats.push(line)
	if err := stats.write(doc); err != nil {
		return nil, err
	}
	return doc.encode()
}
