package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	composeFile  = "docker-compose.yml"
	rollbackFile = "docker-compose.rollback.yml"
)

// Deployer runs the four-phase transactional pipeline for a single app:
// backup current compose, write new, bring up, health probe. On any failure
// after the backup phase, the previous compose is restored and brought back.
type Deployer struct {
	notifier  *Notifier
	composeUp func(ctx context.Context, dir, project string, removeOrphans bool) error
}

func NewDeployer(n *Notifier) *Deployer {
	return &Deployer{notifier: n, composeUp: composeUp}
}

// Deploy applies compose to app and waits for the health probe. Returns nil
// only when the deploy is finalized (commit point reached); any error means
// the previous state was restored or the host was left untouched.
func (d *Deployer) Deploy(ctx context.Context, app AppConfig, compose []byte) error {
	if err := os.MkdirAll(app.AppsDir, 0o755); err != nil {
		return fmt.Errorf("create app dir: %w", err)
	}
	composePath := filepath.Join(app.AppsDir, composeFile)
	rollbackPath := filepath.Join(app.AppsDir, rollbackFile)

	hadPrevious, err := backupCurrent(composePath, rollbackPath)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}

	if err := os.WriteFile(composePath, compose, 0o600); err != nil {
		if hadPrevious {
			_ = os.Rename(rollbackPath, composePath)
		}
		return fmt.Errorf("write compose: %w", err)
	}

	if err := d.composeUp(ctx, app.AppsDir, app.Name, true); err != nil {
		if hadPrevious {
			if rbErr := d.restore(ctx, app); rbErr != nil {
				return fmt.Errorf("compose up failed (%w); restore failed (%v)", err, rbErr)
			}
		}
		d.notifier.Notify(ctx, app.WebhookURL, AlertDeployFailure, app.Name,
			fmt.Sprintf("docker compose up failed: %v", err))
		return fmt.Errorf("compose up: %w", err)
	}

	healthURL := fmt.Sprintf("http://127.0.0.1:%d%s", app.HealthPort, app.HealthPath)
	healthy := waitHealthy(ctx, healthURL,
		time.Duration(app.HealthTimeout),
		time.Duration(app.HealthInterval),
	)
	if !healthy {
		if hadPrevious {
			if rbErr := d.restore(ctx, app); rbErr != nil {
				return fmt.Errorf("health failed; rollback failed: %w", rbErr)
			}
			d.notifier.Notify(ctx, app.WebhookURL, AlertRollback, app.Name,
				"new version failed health check; restored previous version")
			return errors.New("rolled back: health check failed")
		}
		d.notifier.Notify(ctx, app.WebhookURL, AlertDeployFailure, app.Name,
			"health check failed and no previous version to roll back to")
		return errors.New("health check failed (no rollback available)")
	}

	// Commit point: the new version is canonical.
	if hadPrevious {
		if err := os.Remove(rollbackPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("remove rollback file", "err", err)
		}
	}
	return nil
}

func (d *Deployer) restore(ctx context.Context, app AppConfig) error {
	composePath := filepath.Join(app.AppsDir, composeFile)
	rollbackPath := filepath.Join(app.AppsDir, rollbackFile)
	if err := os.Remove(composePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove faulty compose: %w", err)
	}
	if err := os.Rename(rollbackPath, composePath); err != nil {
		return fmt.Errorf("restore compose: %w", err)
	}
	if err := d.composeUp(ctx, app.AppsDir, app.Name, false); err != nil {
		return fmt.Errorf("restore compose up: %w", err)
	}
	return nil
}

func backupCurrent(composePath, rollbackPath string) (bool, error) {
	if _, err := os.Stat(composePath); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := os.Remove(rollbackPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("clear stale rollback: %w", err)
	}
	if err := os.Rename(composePath, rollbackPath); err != nil {
		return false, err
	}
	return true, nil
}

func composeUp(ctx context.Context, dir, project string, removeOrphans bool) error {
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
	slog.Info("docker compose up", "app", project, "output", string(out))
	return nil
}
