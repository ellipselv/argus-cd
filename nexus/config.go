package nexus

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	OcculusURL     string
	OcculusToken   string
	PublicKey      string
	AppsDir        string
	PollInterval   time.Duration
	GracePeriod    time.Duration
	HealthInterval time.Duration
	NotifyURL      string
	Projects       []string
}

type rawConfig struct {
	OcculusURL     string   `yaml:"occulus_url"`
	OcculusToken   string   `yaml:"occulus_token"`
	PublicKey      string   `yaml:"public_key"`
	AppsDir        string   `yaml:"apps_dir"`
	PollInterval   string   `yaml:"poll_interval"`
	GracePeriod    string   `yaml:"grace_period"`
	HealthInterval string   `yaml:"health_interval"`
	NotifyURL      string   `yaml:"notify_url"`
	Projects       []string `yaml:"projects"`
}

func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var r rawConfig
	if err := yaml.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg := &Config{
		OcculusURL:   r.OcculusURL,
		OcculusToken: r.OcculusToken,
		PublicKey:    r.PublicKey,
		AppsDir:      r.AppsDir,
		NotifyURL:    r.NotifyURL,
		Projects:     r.Projects,
	}
	if cfg.PollInterval, err = parseDur(r.PollInterval, 30*time.Second); err != nil {
		return nil, fmt.Errorf("poll_interval: %w", err)
	}
	if cfg.GracePeriod, err = parseDur(r.GracePeriod, 90*time.Second); err != nil {
		return nil, fmt.Errorf("grace_period: %w", err)
	}
	if cfg.HealthInterval, err = parseDur(r.HealthInterval, 5*time.Second); err != nil {
		return nil, fmt.Errorf("health_interval: %w", err)
	}
	if cfg.AppsDir == "" {
		cfg.AppsDir = "/opt/argus/apps"
	}
	if cfg.OcculusURL == "" {
		return nil, errors.New("occulus_url is required")
	}
	if cfg.PublicKey == "" {
		return nil, errors.New("public_key is required")
	}
	if len(cfg.Projects) == 0 {
		return nil, errors.New("projects must not be empty")
	}
	return cfg, nil
}

func parseDur(s string, def time.Duration) (time.Duration, error) {
	if s == "" {
		return def, nil
	}
	return time.ParseDuration(s)
}
