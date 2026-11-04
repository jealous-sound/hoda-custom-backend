package account

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	guestIDPrefix = "guest-"
	guestIDBytes  = 8
	secretBytes   = 32
)

var ErrNotRegistered = errors.New("player has no account")

type Account struct {
	PlayerID   string    `json:"player_id"`
	SecretHash string    `json:"secret_hash"`
	CreatedAt  time.Time `json:"created_at"`
}

type Credentials struct {
	PlayerID string
	Secret   string
}

type Store struct {
	dir   string
	now   func() time.Time
	mutex sync.Mutex
}

func NewStore(dir string, now func() time.Time) (*Store, error) {
	accountsDir := filepath.Join(dir, "accounts")
	if err := os.MkdirAll(accountsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create account directory: %w", err)
	}
	return &Store{dir: accountsDir, now: now}, nil
}

func (s *Store) path(playerID string) string {
	return filepath.Join(s.dir, playerID+".json")
}

func (s *Store) CreateGuest() (Credentials, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for {
		id, err := randomHex(guestIDBytes)
		if err != nil {
			return Credentials{}, err
		}
		playerID := guestIDPrefix + id
		if _, err := os.Stat(s.path(playerID)); err == nil {
			continue
		}
		secret, err := randomHex(secretBytes)
		if err != nil {
			return Credentials{}, err
		}
		record := Account{PlayerID: playerID, SecretHash: hashSecret(secret), CreatedAt: s.now().UTC()}
		if err := s.write(record); err != nil {
			return Credentials{}, err
		}
		return Credentials{PlayerID: playerID, Secret: secret}, nil
	}
}

func (s *Store) Verify(playerID, secret string) (bool, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	data, err := os.ReadFile(s.path(playerID))
	if errors.Is(err, os.ErrNotExist) {
		return false, ErrNotRegistered
	}
	if err != nil {
		return false, fmt.Errorf("read account %s: %w", playerID, err)
	}
	var record Account
	if err := json.Unmarshal(data, &record); err != nil {
		return false, fmt.Errorf("decode account %s: %w", playerID, err)
	}
	return subtle.ConstantTimeCompare([]byte(record.SecretHash), []byte(hashSecret(secret))) == 1, nil
}

func (s *Store) write(record Account) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode account %s: %w", record.PlayerID, err)
	}
	target := s.path(record.PlayerID)
	temporary := target + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return fmt.Errorf("write account %s: %w", record.PlayerID, err)
	}
	if err := os.Rename(temporary, target); err != nil {
		os.Remove(temporary)
		return fmt.Errorf("commit account %s: %w", record.PlayerID, err)
	}
	return nil
}

func randomHex(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
