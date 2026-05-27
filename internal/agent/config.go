package agent

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Agent AgentConfig `toml:"agent"`
	Apps  []AppConfig `toml:"apps"`
}

type AgentConfig struct {
	ID           string   `toml:"id"`
	Executor     string   `toml:"executor"`
	PollInterval Duration `toml:"poll_interval"`
}

type AppConfig struct {
	Name           string    `toml:"name"`
	AppsDir        string    `toml:"apps_dir"`
	HealthPort     int       `toml:"health_port"`
	HealthPath     string    `toml:"health_path"`
	HealthTimeout  Duration  `toml:"health_timeout"`
	HealthInterval Duration  `toml:"health_interval"`
	WebhookURL     string    `toml:"webhook_url"`
	Git            GitConfig `toml:"git"`
}

type GitConfig struct {
	Provider        string    `toml:"provider"`
	RepoURL         string    `toml:"repo_url"`
	Branch          string    `toml:"branch"`
	ComposePath     string    `toml:"compose_path"`
	Token           string    `toml:"token"`
	TokenObtainedAt time.Time `toml:"token_obtained_at"`
	TokenExpiresAt  time.Time `toml:"token_expires_at"`
}

// Duration is a TOML-friendly wrapper around time.Duration. Accepts strings
// like "30s" via TextUnmarshaler.
type Duration time.Duration

func (d *Duration) UnmarshalText(b []byte) error {
	parsed, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := toml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Agent.ID == "" {
		return nil, errors.New("agent.id is required")
	}
	if cfg.Agent.Executor == "" {
		cfg.Agent.Executor = "docker-compose"
	}
	if cfg.Agent.Executor != "docker-compose" {
		return nil, fmt.Errorf(`agent.executor: only "docker-compose" supported, got %q`, cfg.Agent.Executor)
	}
	if cfg.Agent.PollInterval == 0 {
		cfg.Agent.PollInterval = Duration(30 * time.Second)
	}

	if len(cfg.Apps) == 0 {
		return nil, errors.New("at least one [[apps]] entry required")
	}
	seen := map[string]bool{}
	for i := range cfg.Apps {
		a := &cfg.Apps[i]
		if a.Name == "" {
			return nil, fmt.Errorf("apps[%d].name is required", i)
		}
		if seen[a.Name] {
			return nil, fmt.Errorf("apps[%d].name %q duplicated", i, a.Name)
		}
		seen[a.Name] = true
		if a.AppsDir == "" {
			return nil, fmt.Errorf("apps[%d].apps_dir is required", i)
		}
		if a.HealthPort <= 0 || a.HealthPort > 65535 {
			return nil, fmt.Errorf("apps[%d].health_port must be 1..65535", i)
		}
		if a.HealthPath == "" {
			a.HealthPath = "/health"
		}
		if a.HealthTimeout == 0 {
			a.HealthTimeout = Duration(90 * time.Second)
		}
		if a.HealthInterval == 0 {
			a.HealthInterval = Duration(5 * time.Second)
		}
		if a.Git.Provider == "" {
			a.Git.Provider = "github"
		}
		switch a.Git.Provider {
		case "github", "gitlab":
		default:
			return nil, fmt.Errorf(`apps[%d].git.provider: must be "github" or "gitlab", got %q`, i, a.Git.Provider)
		}
		if a.Git.RepoURL == "" {
			return nil, fmt.Errorf("apps[%d].git.repo_url is required", i)
		}
		if a.Git.Branch == "" {
			a.Git.Branch = "main"
		}
		if a.Git.ComposePath == "" {
			a.Git.ComposePath = "docker-compose.yml"
		}
		if a.Git.Token == "" {
			return nil, fmt.Errorf("apps[%d].git.token is required", i)
		}
	}
	return &cfg, nil
}
