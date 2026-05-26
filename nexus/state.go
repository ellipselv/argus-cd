package nexus

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// State persists the version currently deployed per project so the runner can
// skip redeploys when Occulus keeps serving the same bundle.
type State struct {
	mu       sync.Mutex
	path     string
	Versions map[string]string `json:"versions"`
}

func LoadState(appsDir string) (*State, error) {
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(appsDir, "nexus-state.json")
	s := &State{path: path, Versions: map[string]string{}}

	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, err
	}
	if s.Versions == nil {
		s.Versions = map[string]string{}
	}
	return s, nil
}

func (s *State) Get(project string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Versions[project]
}

func (s *State) Set(project, version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Versions[project] = version
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
