package economy

import (
	"encoding/json"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

func ChargeGold(
	c *catalog.Catalog, raw json.RawMessage, server *profile.ServerState, cost int64, now time.Time,
) (json.RawMessage, error) {
	p, err := openPlayer(c, raw, server, now)
	if err != nil {
		return nil, err
	}
	if err := p.account.spendGold(cost); err != nil {
		return nil, err
	}
	return p.close()
}
