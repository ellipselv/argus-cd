package nexus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ellipselv/argus-cd/pkg/bundle"
)

const (
	composeFile  = "docker-compose.yml"
	rollbackFile = "docker-compose.rollback.yml"
)

// Deployer applies a verified manifest to the local Docker engine using the
// FetchTask → Backup → Deploy → Health pipeline from the spec.
type Deployer struct {
	cfg      *Config
	notifier *Notifier
}

func NewDeployer(cfg *Config, n *Notifier) *Deployer {
	return &Deployer{cfg: cfg, notifier: n}
}

// Deploy is the full pipeline for a single manifest. It either finalises
// (returns nil) or rolls back to the previous compose file. The caller should
// only mark the version as deployed when Deploy returns nil.
func (d *Deployer) Deploy(ctx context.Context, m *bundle.Manifest) error {
	dir := filepath.Join(d.cfg.AppsDir, m.Project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create project dir: %w", err)
	}

	composePath := filepath.Join(dir, composeFile)
	rollbackPath := filepath.Join(dir, rollbackFile)

	hadPrevious, err := backupCurrent(composePath, rollbackPath)
	if err != nil {
		return fmt.Errorf("backup current state: %w", err)
	}

	if err := os.WriteFile(composePath, []byte(m.Compose), 0o600); err != nil {
		// Restore the rollback file so the host is left in a consistent state.
		if hadPrevious {
			_ = os.Rename(rollbackPath, composePath)
		}
		return fmt.Errorf("write new compose: %w", err)
	}

	if err := dockerComposeUp(ctx, dir, m.Project, true); err != nil {
		slog.Error("compose up failed; attempting restore", "project", m.Project, "err", err)
		if hadPrevious {
			if rbErr := d.restore(ctx, dir, m.Project); rbErr != nil {
				return fmt.Errorf("compose up failed and restore failed: %w (restore: %v)", err, rbErr)
			}
			d.notifier.Notify(ctx, AlertDeployFailure, fmtAlert(
				"Compose bring-up failed for %s@%s; restored previous version.", m.Project, m.Version))
			return fmt.Errorf("compose up failed (rolled back): %w", err)
		}
		return fmt.Errorf("compose up failed (no previous version): %w", err)
	}

	healthy := waitHealthy(ctx, healthURL(m.HealthPort), d.cfg.GracePeriod, d.cfg.HealthInterval)
	if !healthy {
		if hadPrevious {
			if rbErr := d.restore(ctx, dir, m.Project); rbErr != nil {
				return fmt.Errorf("health failed and rollback failed: %w", rbErr)
			}
			d.notifier.Notify(ctx, AlertRollback, fmtAlert(
				"Automated Rollback Triggered for %s@%s: new version failed health checks, restored previous working state.",
				m.Project, m.Version))
			return errors.New("rolled back: health check failed")
		}
		d.notifier.Notify(ctx, AlertDeployFailure, fmtAlert(
			"Health check failed for %s@%s and no previous version to roll back to.",
			m.Project, m.Version))
		return errors.New("health check failed (no rollback available)")
	}

	if hadPrevious {
		if err := os.Remove(rollbackPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("evict rollback file", "err", err)
		}
	}
	slog.Info("deploy finalized", "project", m.Project, "version", m.Version)
	return nil
}

func backupCurrent(composePath, rollbackPath string) (bool, error) {
	if _, err := os.Stat(composePath); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	// Clear any stale rollback file from a prior aborted deploy.
	if err := os.Remove(rollbackPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("remove stale rollback: %w", err)
	}
	if err := os.Rename(composePath, rollbackPath); err != nil {
		return false, err
	}
	return true, nil
}

func (d *Deployer) restore(ctx context.Context, dir, project string) error {
	composePath := filepath.Join(dir, composeFile)
	rollbackPath := filepath.Join(dir, rollbackFile)
	if err := os.Rename(rollbackPath, composePath); err != nil {
		return fmt.Errorf("restore compose: %w", err)
	}
	if err := dockerComposeUp(ctx, dir, project, false); err != nil {
		return fmt.Errorf("restore compose up: %w", err)
	}
	return nil
}

func dockerComposeUp(ctx context.Context, dir, project string, removeOrphans bool) error {
	args := []string{"compose", "-p", project, "up", "-d"}
	if removeOrphans {
		args = append(args, "--remove-orphans")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	slog.Info("docker compose up", "project", project, "output", string(out))
	return nil
}
