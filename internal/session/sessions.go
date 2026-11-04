package session

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

const tokenBytes = 32

type Registry struct {
	mutex   sync.RWMutex
	players map[string]string
}

func NewRegistry() *Registry {
	return &Registry{players: map[string]string{}}
}

func (r *Registry) Open(playerID string) (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	r.mutex.Lock()
	r.players[token] = playerID
	r.mutex.Unlock()
	return token, nil
}

func (r *Registry) Player(token string) (string, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	playerID, ok := r.players[token]
	return playerID, ok
}

func (r *Registry) CloseAll(playerID string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	for token, owner := range r.players {
		if owner == playerID {
			delete(r.players, token)
		}
	}
}
