package agent

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestState_LoadMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got := s.Get("anything"); got != "" {
		t.Errorf("expected empty version on missing file, got %q", got)
	}
}

func TestState_LoadEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got := s.Get("anything"); got != "" {
		t.Errorf("expected empty version on empty file, got %q", got)
	}
}

func TestState_LoadCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(path); err == nil {
		t.Fatal("expected error on corrupt state file, got nil")
	}
}

func TestState_SetGet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("app1", "sha123"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := s.Get("app1"); got != "sha123" {
		t.Errorf("Get app1 = %q, want sha123", got)
	}
	if got := s.Get("unknown"); got != "" {
		t.Errorf("Get unknown = %q, want empty", got)
	}
}

func TestState_PersistsAcrossLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("app1", "sha1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("app2", "sha2"); err != nil {
		t.Fatal(err)
	}

	s2, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Get("app1"); got != "sha1" {
		t.Errorf("reloaded app1 = %q, want sha1", got)
	}
	if got := s2.Get("app2"); got != "sha2" {
		t.Errorf("reloaded app2 = %q, want sha2", got)
	}
}

func TestState_SetUsesAtomicRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("app1", "sha1"); err != nil {
		t.Fatal(err)
	}
	// The temp file must not be left behind after a successful Set.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("expected %s.tmp to not exist after Set, stat err: %v", path, err)
	}
}

// TestState_ConcurrentSet verifies the mutex guards concurrent writes. Run
// with `go test -race` to catch any data race in the version map.
func TestState_ConcurrentSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Set("app1", "sha"); err != nil {
				t.Errorf("Set: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := s.Get("app1"); got != "sha" {
		t.Errorf("Get app1 = %q, want sha", got)
	}
}
