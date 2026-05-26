package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type Runner struct {
	cfg      *Config
	git      *Git
	state    *State
	deploy   *Deployer
	notifier *Notifier
}

// NewRunner builds a Runner. statePath defaults to DefaultStatePath when empty;
// gitBaseURL defaults to GitHub's public API when empty (override is for
// smoke/integration tests).
func NewRunner(cfg *Config, statePath, gitBaseURL string) (*Runner, error) {
	if statePath == "" {
		statePath = DefaultStatePath
	}
	state, err := LoadState(statePath)
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}
	git := NewGit()
	if gitBaseURL != "" {
		git.baseURL = gitBaseURL
	}
	notifier := NewNotifier()
	return &Runner{
		cfg:      cfg,
		git:      git,
		state:    state,
		deploy:   NewDeployer(notifier),
		notifier: notifier,
	}, nil
}

func (r *Runner) Run(ctx context.Context) {
	slog.Info("arguscd starting",
		"id", r.cfg.Agent.ID,
		"apps", appNames(r.cfg.Apps),
		"poll", time.Duration(r.cfg.Agent.PollInterval),
	)

	r.tick(ctx)
	t := time.NewTicker(time.Duration(r.cfg.Agent.PollInterval))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("arguscd stopping")
			return
		case <-t.C:
			r.tick(ctx)
		}
	}
}

func (r *Runner) tick(ctx context.Context) {
	for _, app := range r.cfg.Apps {
		if err := r.handle(ctx, app); err != nil {
			slog.Error("app tick failed", "app", app.Name, "err", err)
		}
	}
}

func (r *Runner) handle(ctx context.Context, app AppConfig) error {
	CheckTokenExpiry(app.Name, app.Git.TokenExpiresAt)

	sha, err := r.git.LatestSHA(ctx, app)
	if err != nil {
		return fmt.Errorf("latest sha: %w", err)
	}
	if r.state.Get(app.Name) == sha {
		return nil
	}

	compose, err := r.git.FetchCompose(ctx, app, sha)
	if err != nil {
		return fmt.Errorf("fetch compose: %w", err)
	}

	slog.Info("deploying", "app", app.Name, "sha", sha)
	if err := r.deploy.Deploy(ctx, app, compose); err != nil {
		return fmt.Errorf("deploy: %w", err)
	}
	if err := r.state.Set(app.Name, sha); err != nil {
		slog.Error("persist state", "err", err)
	}
	slog.Info("deploy finalized", "app", app.Name, "sha", sha)
	return nil
}

func appNames(apps []AppConfig) []string {
	out := make([]string, len(apps))
	for i, a := range apps {
		out[i] = a.Name
	}
	return out
}
