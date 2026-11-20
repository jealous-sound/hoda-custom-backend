package economy

import "github.com/jealous-sound/hoda-custom-backend/internal/catalog"

func (p *player) awardChallengeDrop(mapID string) error {
	drops := p.catalog.ChallengeDrops
	if len(drops.Maps[mapID]) == 0 {
		return nil
	}
	drop, dropped := drops.Pick(mapID, p.rng.below(drops.RollTotal))
	switch {
	case !dropped:
		return nil
	case drop.Kind == catalog.ChallengeDropRune:
		return p.grantRunes([]string{drop.ID})
	case p.rosterRoom() <= 0:
		return nil
	default:
		return p.grantHeroes([]string{drop.ID})
	}
}
