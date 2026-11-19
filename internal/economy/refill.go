package economy

import (
	"encoding/json"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

type RefillKind string

const (
	RefillEnergy  RefillKind = "energy"
	RefillStamina RefillKind = "stamina"
	RefillRevive  RefillKind = "revive"
)

func Refill(
	c *catalog.Catalog, raw json.RawMessage, server *profile.ServerState, kind RefillKind, now time.Time,
) (json.RawMessage, error) {
	if kind != RefillEnergy && kind != RefillStamina && kind != RefillRevive {
		return nil, refuse(Unavailable, "nothing to refill called %q", kind)
	}
	p, err := openPlayer(c, raw, server, now)
	if err != nil {
		return nil, err
	}
	if kind == RefillRevive {
		if err := p.buyRevivePotion(); err != nil {
			return nil, err
		}
		return p.close()
	}
	pool, maximum := &p.account.Energy, p.account.EnergyMax
	timer := energyRegenTimer
	if kind == RefillStamina {
		pool, maximum, timer = &p.account.Stamina, p.account.StaminaMax, staminaRegenTimer
	}
	if *pool >= maximum {
		return nil, refuse(AlreadyFull, "%s is already full", kind)
	}
	if err := p.account.spendGems(RefillGemCost); err != nil {
		return nil, err
	}
	*pool = maximum
	p.account.doc.setFloat(timer, 0)
	return p.close()
}

func (p *player) buyRevivePotion() error {
	nexus, err := p.doc.section("nexus")
	if err != nil {
		return refuse(BadProfile, "%v", err)
	}
	potions, err := nexus.int64Field("potions")
	if err != nil {
		return refuse(BadProfile, "%v", err)
	}
	if maximum, _ := nexus.optionalInt("potion_max"); maximum > 0 && potions >= int64(maximum) {
		return refuse(AlreadyFull, "revive potions are already full")
	}
	if err := p.account.spendGems(RevivePotionGemCost); err != nil {
		return err
	}
	return p.addPotions(1)
}
