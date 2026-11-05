package guild

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const stateFileName = "guilds.json"

type Store struct {
	path  string
	mutex sync.Mutex
	state *State
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create guild directory: %w", err)
	}
	s := &Store{path: filepath.Join(dir, stateFileName)}
	state, err := s.load()
	if err != nil {
		return nil, err
	}
	s.state = state
	return s, nil
}

func (s *Store) Update(change func(*State) error) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	working := s.state.clone()
	if err := change(working); err != nil {
		return err
	}
	if err := s.save(working); err != nil {
		return err
	}
	s.state = working
	return nil
}

func (s *Store) View(read func(*State)) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	read(s.state)
}

func (s *Store) load() (*State, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return newState(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read guilds: %w", err)
	}
	state := newState()
	if err := json.Unmarshal(data, state); err != nil {
		return nil, fmt.Errorf("decode guilds: %w", err)
	}
	state.normalize()
	return state, nil
}

func (s *Store) save(state *State) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode guilds: %w", err)
	}
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o644); err != nil {
		return fmt.Errorf("write guilds: %w", err)
	}
	if err := os.Rename(temporary, s.path); err != nil {
		os.Remove(temporary)
		return fmt.Errorf("commit guilds: %w", err)
	}
	return nil
}
