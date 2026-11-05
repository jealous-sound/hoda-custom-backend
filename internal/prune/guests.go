package prune

import (
	"errors"
	"fmt"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/account"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
	"github.com/jealous-sound/hoda-custom-backend/internal/session"
)

type Pruner struct {
	accounts *account.Store
	profiles *profile.Store
	sessions *session.Registry
	now      func() time.Time
	maxIdle  time.Duration
}

func NewPruner(
	accounts *account.Store, profiles *profile.Store, sessions *session.Registry, now func() time.Time,
	maxIdle time.Duration,
) *Pruner {
	return &Pruner{accounts: accounts, profiles: profiles, sessions: sessions, now: now, maxIdle: maxIdle}
}

func (p *Pruner) AbandonedGuests() ([]string, error) {
	guests, err := p.accounts.Guests()
	if err != nil {
		return nil, err
	}
	cutoff := p.now().Add(-p.maxIdle)
	var pruned []string
	for _, guest := range guests {
		if guest.LastSeen().After(cutoff) {
			continue
		}
		abandoned, err := p.leftNoProgress(guest.PlayerID)
		if err != nil {
			return pruned, err
		}
		if !abandoned {
			continue
		}
		deleted, err := p.accounts.DeleteIfUnseenSince(guest.PlayerID, cutoff)
		if err != nil {
			return pruned, err
		}
		if !deleted {
			continue
		}
		p.sessions.CloseAll(guest.PlayerID)
		if err := p.profiles.Delete(guest.PlayerID); err != nil {
			return pruned, fmt.Errorf("account %s deleted but profile kept: %w", guest.PlayerID, err)
		}
		pruned = append(pruned, guest.PlayerID)
	}
	return pruned, nil
}

func (p *Pruner) leftNoProgress(playerID string) (bool, error) {
	record, err := p.profiles.Get(playerID)
	if errors.Is(err, profile.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return !profile.ShowsProgress(record.Profile), nil
}
