package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// healthServer spins up an httptest server that nexus's deploy pipeline will
// probe at /health, and returns its (host=127.0.0.1) port.
func healthServer(t *testing.T, handler http.HandlerFunc) int {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func testApp(t *testing.T, port int) AppConfig {
	t.Helper()
	return AppConfig{
		Name:           "test",
		AppsDir:        t.TempDir(),
		HealthPort:     port,
		HealthPath:     "/health",
		HealthTimeout:  Duration(500 * time.Millisecond),
		HealthInterval: Duration(50 * time.Millisecond),
	}
}

func newTestDeployer(t *testing.T, composeUp func(context.Context, string, string, bool) error) *Deployer {
	t.Helper()
	d := NewDeployer(NewNotifier(""))
	d.composeUp = composeUp
	return d
}

func TestDeploy_FirstDeploySuccess(t *testing.T) {
	port := healthServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	app := testApp(t, port)

	var ups atomic.Int32
	d := newTestDeployer(t, func(ctx context.Context, dir, project string, removeOrphans bool) error {
		ups.Add(1)
		if !removeOrphans {
			t.Error("first deploy should pass --remove-orphans")
		}
		return nil
	})

	if err := d.Deploy(context.Background(), app, []byte("services: {}\n")); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if ups.Load() != 1 {
		t.Errorf("composeUp calls = %d, want 1", ups.Load())
	}
	if _, err := os.Stat(filepath.Join(app.AppsDir, composeFile)); err != nil {
		t.Errorf("compose file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(app.AppsDir, rollbackFile)); !os.IsNotExist(err) {
		t.Errorf("rollback file should not exist after first deploy: %v", err)
	}
}

func TestDeploy_HappyPath_FinalizesAndEvictsRollback(t *testing.T) {
	port := healthServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	app := testApp(t, port)

	// Plant an existing compose to be backed up.
	composePath := filepath.Join(app.AppsDir, composeFile)
	if err := os.WriteFile(composePath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	d := newTestDeployer(t, func(context.Context, string, string, bool) error { return nil })

	if err := d.Deploy(context.Background(), app, []byte("new")); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	got, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("compose body = %q, want new", got)
	}
	if _, err := os.Stat(filepath.Join(app.AppsDir, rollbackFile)); !os.IsNotExist(err) {
		t.Errorf("rollback file should be evicted on success: %v", err)
	}
}

func TestDeploy_HealthFailure_RollsBack(t *testing.T) {
	port := healthServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	app := testApp(t, port)

	composePath := filepath.Join(app.AppsDir, composeFile)
	if err := os.WriteFile(composePath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	var ups atomic.Int32
	d := newTestDeployer(t, func(ctx context.Context, dir, project string, removeOrphans bool) error {
		ups.Add(1)
		return nil
	})

	err := d.Deploy(context.Background(), app, []byte("new"))
	if err == nil {
		t.Fatal("expected error on health failure")
	}
	// 2 composeUp calls: one for "new", one for restoring "old"
	if ups.Load() != 2 {
		t.Errorf("composeUp calls = %d, want 2", ups.Load())
	}
	got, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Errorf("after rollback compose body = %q, want old", got)
	}
}

func TestDeploy_ComposeUpFailure_Restores(t *testing.T) {
	app := testApp(t, 1) // health port doesn't matter; we'll fail before probing
	composePath := filepath.Join(app.AppsDir, composeFile)
	if err := os.WriteFile(composePath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	var ups atomic.Int32
	d := newTestDeployer(t, func(ctx context.Context, dir, project string, removeOrphans bool) error {
		n := ups.Add(1)
		if n == 1 {
			return errors.New("simulated docker failure")
		}
		return nil // restore call succeeds
	})

	if err := d.Deploy(context.Background(), app, []byte("new")); err == nil {
		t.Fatal("expected error on compose up failure")
	}
	got, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Errorf("after restore compose body = %q, want old", got)
	}
}

func TestDeploy_HealthFailure_NoPrevious_Errors(t *testing.T) {
	port := healthServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	app := testApp(t, port)

	d := newTestDeployer(t, func(context.Context, string, string, bool) error { return nil })
	err := d.Deploy(context.Background(), app, []byte("new"))
	if err == nil {
		t.Fatal("expected error when health fails and no rollback available")
	}
}

func TestBackupCurrent_NoExisting(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, composeFile)
	rollbackPath := filepath.Join(dir, rollbackFile)
	had, err := backupCurrent(composePath, rollbackPath)
	if err != nil {
		t.Fatal(err)
	}
	if had {
		t.Error("had=true with no existing compose")
	}
}

func TestBackupCurrent_ClearsStaleRollback(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, composeFile)
	rollbackPath := filepath.Join(dir, rollbackFile)
	if err := os.WriteFile(composePath, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rollbackPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	had, err := backupCurrent(composePath, rollbackPath)
	if err != nil {
		t.Fatal(err)
	}
	if !had {
		t.Error("had=false with existing compose")
	}
	got, _ := os.ReadFile(rollbackPath)
	if string(got) != "current" {
		t.Errorf("rollback content = %q, want current (stale should have been cleared first)", got)
	}
}
