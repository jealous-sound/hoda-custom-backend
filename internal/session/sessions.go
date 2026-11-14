package session

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

const (
	tokenBytes           = 32
	MaxSessionsPerPlayer = 8
	sweepEvery           = time.Minute
)

type entry struct {
	player   string
	lastUsed time.Time
}

type Registry struct {
	mutex     sync.Mutex
	tokens    map[string]*entry
	now       func() time.Time
	idle      time.Duration
	lastSweep time.Time
}

func NewRegistry(now func() time.Time, idle time.Duration) *Registry {
	return &Registry{tokens: map[string]*entry{}, now: now, idle: idle, lastSweep: now()}
}

func (r *Registry) Open(playerID string) (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	now := r.now()
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.sweepLocked(now)
	r.makeRoomLocked(playerID)
	r.tokens[token] = &entry{player: playerID, lastUsed: now}
	return token, nil
}

func (r *Registry) Player(token string) (string, bool) {
	now := r.now()
	r.mutex.Lock()
	defer r.mutex.Unlock()
	session, ok := r.tokens[token]
	if !ok {
		return "", false
	}
	if r.expired(session, now) {
		delete(r.tokens, token)
		return "", false
	}
	session.lastUsed = now
	return session.player, true
}

func (r *Registry) CloseAll(playerID string) {
	r.CloseOthers(playerID, "")
}

func (r *Registry) CloseOthers(playerID, keptToken string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	for token, session := range r.tokens {
		if session.player == playerID && token != keptToken {
			delete(r.tokens, token)
		}
	}
}

func (r *Registry) Close(token string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	delete(r.tokens, token)
}

func (r *Registry) expired(session *entry, now time.Time) bool {
	return r.idle > 0 && now.Sub(session.lastUsed) >= r.idle
}

func (r *Registry) sweepLocked(now time.Time) {
	if now.Sub(r.lastSweep) < sweepEvery {
		return
	}
	r.lastSweep = now
	for token, session := range r.tokens {
		if r.expired(session, now) {
			delete(r.tokens, token)
		}
	}
}

func (r *Registry) makeRoomLocked(playerID string) {
	var owned []string
	for token, session := range r.tokens {
		if session.player == playerID {
			owned = append(owned, token)
		}
	}
	if len(owned) < MaxSessionsPerPlayer {
		return
	}
	sort.Slice(owned, func(i, j int) bool { return r.tokens[owned[i]].lastUsed.Before(r.tokens[owned[j]].lastUsed) })
	for _, token := range owned[:len(owned)-MaxSessionsPerPlayer+1] {
		delete(r.tokens, token)
	}
}

func (r *Registry) Count() int {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return len(r.tokens)
}
