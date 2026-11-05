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
)

func Refill(
	c *catalog.Catalog, raw json.RawMessage, server *profile.ServerState, kind RefillKind, now time.Time,
) (json.RawMessage, error) {
	if kind != RefillEnergy && kind != RefillStamina {
		return nil, refuse(Unavailable, "nothing to refill called %q", kind)
	}
	p, err := openPlayer(c, raw, server, now)
	if err != nil {
		return nil, err
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
