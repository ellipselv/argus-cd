package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// DefaultStatePath is the canonical location of the persistent deploy state.
// It maps application name → currently-deployed commit SHA.
const DefaultStatePath = "/opt/argus/apps/argus-state.json"

type State struct {
	mu       sync.Mutex
	path     string
	versions map[string]string
}

func LoadState(path string) (*State, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	s := &State{path: path, versions: map[string]string{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(b, &s.versions); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *State) Get(app string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.versions[app]
}

// Set atomically updates the version for app. The temp+rename pattern keeps
// the file readable even if the host loses power mid-write.
func (s *State) Set(app, sha string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.versions[app] = sha
	raw, err := json.MarshalIndent(s.versions, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
