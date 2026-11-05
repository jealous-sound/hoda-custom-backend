package economy

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const (
	ActionBattle         = "battle"
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
	case ActionBattle:
		return decodeInto(body, p.Battle)
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
	}
	return BattleResult{}, &BadRequest{Message: fmt.Sprintf("no action %q", action)}
}

type Duels interface {
	DuelLevel(memberID string) (int, error)
}

type noDuels struct{}

func (noDuels) DuelLevel(memberID string) (int, error) {
	return 0, refuse(Unavailable, "no guild to duel %q in", memberID)
}

func Act(c *catalog.Catalog, raw json.RawMessage, server *profile.ServerState, action string, body []byte,
	now time.Time,
) (json.RawMessage, BattleResult, error) {
	return ActWithDuels(c, raw, server, action, body, now, noDuels{})
}

func ActWithDuels(c *catalog.Catalog, raw json.RawMessage, server *profile.ServerState, action string, body []byte,
	now time.Time, duels Duels,
) (json.RawMessage, BattleResult, error) {
	p, err := openPlayer(c, raw, server, now)
	if err != nil {
		return nil, BattleResult{}, err
	}
	p.duels = duels
	result, err := p.dispatch(action, body)
	if err != nil {
		return nil, BattleResult{}, err
	}
	encoded, err := p.close()
	if err != nil {
		return nil, BattleResult{}, err
	}
	return encoded, result, nil
}
