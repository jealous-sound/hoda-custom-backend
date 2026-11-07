package economy

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const (
	ActionBattle         = "battle"
	ActionBattleStart    = "battle_start"
	ActionNexusAward     = "nexus_award"
	ActionNexusReset     = "nexus_reset"
	ActionNexusUnlock    = "nexus_unlock"
	ActionMissionClaim   = "mission_claim"
	ActionPvpEventClaim  = "pvp_event_claim"
	ActionGearSell       = "gear_sell"
	ActionGearPrestige   = "gear_prestige"
	ActionAscend         = "ascend"
	ActionRunePrestige   = "rune_prestige"
	ActionRuneCraft      = "rune_craft"
	ActionTutorialReward = "tutorial_reward"
	ActionConsume        = "consume"
	ActionPvpDefend      = "pvp_defend"
	ActionPvpMatch       = "pvp_match"
	ActionFortify        = "fortify"
	ActionRemoveFortify  = "remove_fortify"
)

func decodeInto[T any](body []byte, run func(T) (BattleResult, error)) (BattleResult, error) {
	var request T
	if err := json.Unmarshal(body, &request); err != nil {
		return BattleResult{}, &BadRequest{Message: err.Error()}
	}
	return run(request)
}

type BadRequest struct {
	Message string
}

func (b *BadRequest) Error() string {
	return b.Message
}

func (p *player) dispatch(action string, body []byte) (BattleResult, error) {
	switch action {
	case ActionBattleStart:
		return decodeInto(body, p.StartBattle)
	case ActionBattle:
		return decodeInto(body, p.FinishBattle)
	case ActionNexusAward:
		return p.AwardNexusRun()
	case ActionNexusReset:
		return p.ResetNexusRun()
	case ActionNexusUnlock:
		return decodeInto(body, p.UnlockNexusEvent)
	case ActionMissionClaim:
		return decodeInto(body, p.ClaimMission)
	case ActionPvpEventClaim:
		return decodeInto(body, p.ClaimPvpEvent)
	case ActionGearSell:
		return decodeInto(body, p.SellGear)
	case ActionGearPrestige:
		return decodeInto(body, p.UnlockGearPrestige)
	case ActionAscend:
		return decodeInto(body, p.AscendHero)
	case ActionRunePrestige:
		return decodeInto(body, p.UnlockRunePrestige)
	case ActionRuneCraft:
		return decodeInto(body, p.CraftRunes)
	case ActionTutorialReward:
		return decodeInto(body, p.ClaimTutorialReward)
	case ActionConsume:
		return decodeInto(body, p.ConsumeHeroes)
	case ActionPvpDefend:
		return p.DefendPvp()
	case ActionPvpMatch:
		return decodeInto(body, p.MatchPvp)
	case ActionFortify:
		return decodeInto(body, p.FortifyHero)
	case ActionRemoveFortify:
		return decodeInto(body, p.RemoveFortify)
	}
	return BattleResult{}, &BadRequest{Message: fmt.Sprintf("no action %q", action)}
}

type DuelMatch struct {
	Name     string
	Level    int
	Trophies int
}

type Duels interface {
	DuelOpponent(memberID string) (DuelMatch, error)
	DuelSquad(memberID string) (DuelOpponent, error)
}

type noDuels struct{}

func (noDuels) DuelOpponent(memberID string) (DuelMatch, error) {
	return DuelMatch{}, refuse(Unavailable, "no guild to duel %q in", memberID)
}

func (noDuels) DuelSquad(memberID string) (DuelOpponent, error) {
	return DuelOpponent{}, refuse(Unavailable, "no guild to duel %q in", memberID)
}

type Env struct {
	Duels Duels
	Seeds SeedSource
}

func Act(c *catalog.Catalog, raw json.RawMessage, server *profile.ServerState, action string, body []byte,
	now time.Time,
) (json.RawMessage, BattleResult, error) {
	return ActWith(c, raw, server, action, body, now, Env{})
}

func CommittedRefusal(err error) bool {
	var refusal *Refusal
	return errors.As(err, &refusal) && refusal.Committed
}

func ActWith(c *catalog.Catalog, raw json.RawMessage, server *profile.ServerState, action string, body []byte,
	now time.Time, env Env,
) (json.RawMessage, BattleResult, error) {
	p, err := openPlayer(c, raw, server, now)
	if err != nil {
		return nil, BattleResult{}, err
	}
	if env.Duels != nil {
		p.duels = env.Duels
	}
	if env.Seeds != nil {
		p.seeds = env.Seeds
	}
	result, err := p.dispatch(action, body)
	if err != nil && !CommittedRefusal(err) {
		return nil, BattleResult{}, err
	}
	encoded, closeErr := p.close()
	if closeErr != nil {
		return nil, BattleResult{}, closeErr
	}
	return encoded, result, err
}
