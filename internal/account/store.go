package account

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	guestIDPrefix = "guest-"
	guestIDBytes  = 8
	secretBytes   = 32
)

var ErrNotRegistered = errors.New("player has no account")

type Device struct {
	SecretHash string    `json:"secret_hash"`
	SignedInAt time.Time `json:"signed_in_at"`
}

type Account struct {
	PlayerID      string    `json:"player_id"`
	SecretHash    string    `json:"secret_hash"`
	Login         string    `json:"login,omitempty"`
	Password      string    `json:"password,omitempty"`
	Devices       []Device  `json:"devices,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	LastSessionAt time.Time `json:"last_session_at,omitzero"`
}

func (a Account) HasLogin() bool {
	return a.Login != ""
}

func (a Account) IsGuest() bool {
	return strings.HasPrefix(a.PlayerID, guestIDPrefix) && !a.HasLogin()
}

func (a Account) LastSeen() time.Time {
	if a.LastSessionAt.After(a.CreatedAt) {
		return a.LastSessionAt
	}
	return a.CreatedAt
}

type Credentials struct {
	PlayerID string
	Secret   string
}

type Store struct {
	dir                string
	now                func() time.Time
	mutex              sync.Mutex
	passwordIterations int
	decoy              func() (string, error)
	owners             map[string]string
}

type Option func(*Store)

func WithPasswordIterations(iterations int) Option {
	return func(s *Store) { s.passwordIterations = iterations }
}

func NewStore(dir string, now func() time.Time, options ...Option) (*Store, error) {
	accountsDir := filepath.Join(dir, "accounts")
	if err := os.MkdirAll(accountsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create account directory: %w", err)
	}
	s := &Store{dir: accountsDir, now: now, passwordIterations: DefaultPasswordIterations}
	for _, option := range options {
		option(s)
	}
	s.decoy = sync.OnceValues(func() (string, error) { return hashPassword(decoyPassword, s.passwordIterations) })
	if err := s.indexLogins(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) indexLogins() error {
	accounts, err := s.All()
	if err != nil {
		return err
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.owners = map[string]string{}
	for _, record := range accounts {
		if record.HasLogin() {
			s.owners[LoginKey(record.Login)] = record.PlayerID
		}
	}
	return nil
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
	record, err := s.read(playerID)
	if err != nil {
		return false, err
	}
	return record.holds(secret), nil
}

func (s *Store) RecordSession(playerID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	record, err := s.read(playerID)
	if err != nil {
		return err
	}
	record.LastSessionAt = s.now().UTC()
	return s.write(record)
}

func (s *Store) All() ([]Account, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	var accounts []Account
	for _, entry := range entries {
		playerID, isRecord := strings.CutSuffix(entry.Name(), ".json")
		if !isRecord || entry.IsDir() {
			continue
		}
		record, err := s.read(playerID)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, record)
	}
	return accounts, nil
}

func (s *Store) Guests() ([]Account, error) {
	accounts, err := s.All()
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(accounts, func(a Account) bool { return !a.IsGuest() }), nil
}

func (s *Store) DeleteIfUnseenSince(playerID string, cutoff time.Time) (bool, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	record, err := s.read(playerID)
	if errors.Is(err, ErrNotRegistered) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if record.LastSeen().After(cutoff) {
		return false, nil
	}
	if err := s.remove(record); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) remove(record Account) error {
	if err := os.Remove(s.path(record.PlayerID)); err != nil {
		return fmt.Errorf("delete account %s: %w", record.PlayerID, err)
	}
	if record.HasLogin() {
		delete(s.owners, LoginKey(record.Login))
	}
	return nil
}

func (s *Store) read(playerID string) (Account, error) {
	data, err := os.ReadFile(s.path(playerID))
	if errors.Is(err, os.ErrNotExist) {
		return Account{}, ErrNotRegistered
	}
	if err != nil {
		return Account{}, fmt.Errorf("read account %s: %w", playerID, err)
	}
	var record Account
	if err := json.Unmarshal(data, &record); err != nil {
		return Account{}, fmt.Errorf("decode account %s: %w", playerID, err)
	}
	return record, nil
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
