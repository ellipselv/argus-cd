package nexus

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"log/slog"
	"time"

	"github.com/ellipselv/argus-cd/pkg/bundle"
)

// Runner ties config, the Occulus client, signature verification, the deploy
// pipeline, and version tracking into a single periodic loop.
type Runner struct {
	cfg      *Config
	client   *Client
	state    *State
	deploy   *Deployer
	notifier *Notifier
	pubKey   ed25519.PublicKey
}

func NewRunner(cfg *Config) (*Runner, error) {
	pub, err := bundle.LoadPublicKey(cfg.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("load public key: %w", err)
	}
	state, err := LoadState(cfg.AppsDir)
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}
	notifier := NewNotifier(cfg.NotifyURL)
	return &Runner{
		cfg:      cfg,
		client:   NewClient(cfg.OcculusURL, cfg.OcculusToken),
		state:    state,
		deploy:   NewDeployer(cfg, notifier),
		notifier: notifier,
		pubKey:   pub,
	}, nil
}

func (r *Runner) Run(ctx context.Context) {
	slog.Info("argus-nexus starting",
		"occulus", r.cfg.OcculusURL,
		"projects", r.cfg.Projects,
		"poll", r.cfg.PollInterval,
	)

	r.tick(ctx)

	t := time.NewTicker(r.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("argus-nexus stopping")
			return
		case <-t.C:
			r.tick(ctx)
		}
	}
}

func (r *Runner) tick(ctx context.Context) {
	for _, project := range r.cfg.Projects {
		if err := r.handleProject(ctx, project); err != nil {
			slog.Error("project tick failed", "project", project, "err", err)
		}
	}
}

func (r *Runner) handleProject(ctx context.Context, project string) error {
	b, err := r.client.FetchBundle(ctx, project)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	if b == nil {
		return nil
	}

	m, err := b.Verify(r.pubKey)
	if err != nil {
		// Spec: Drop bundle immediately and trigger security alert.
		r.notifier.Notify(ctx, AlertSignatureFailure, fmtAlert(
			"Dropped bundle for %s: %v. Possible MITM or misconfigured key.", project, err))
		return fmt.Errorf("verify: %w", err)
	}
	if m.Project != project {
		return fmt.Errorf("manifest project mismatch: got %q, expected %q", m.Project, project)
	}
	if r.state.Get(project) == m.Version {
		return nil
	}

	slog.Info("deploying", "project", m.Project, "version", m.Version, "health_port", m.HealthPort)
	if err := r.deploy.Deploy(ctx, m); err != nil {
		return fmt.Errorf("deploy: %w", err)
	}
	if err := r.state.Set(project, m.Version); err != nil {
		slog.Error("persist state", "err", err)
	}
	return nil
}
