package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validConfig = `
[agent]
id = "test-node"
poll_interval = "15s"

[[apps]]
name = "myapp"
apps_dir = "/tmp/myapp"
health_port = 8080

  [apps.git]
  provider = "github"
  repo_url = "https://github.com/o/r"
  token = "ghp_xxx"
  token_expires_at = 2030-01-01T00:00:00Z
`

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfig_ValidWithDefaults(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, validConfig))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Agent.ID != "test-node" {
		t.Errorf("Agent.ID = %q", cfg.Agent.ID)
	}
	if cfg.Agent.Executor != "docker-compose" {
		t.Errorf("Agent.Executor default = %q, want docker-compose", cfg.Agent.Executor)
	}
	if got := time.Duration(cfg.Agent.PollInterval); got != 15*time.Second {
		t.Errorf("PollInterval = %v, want 15s", got)
	}
	if len(cfg.Apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(cfg.Apps))
	}
	a := cfg.Apps[0]
	if a.HealthPath != "/health" {
		t.Errorf("HealthPath default = %q, want /health", a.HealthPath)
	}
	if got := time.Duration(a.HealthTimeout); got != 90*time.Second {
		t.Errorf("HealthTimeout default = %v, want 90s", got)
	}
	if got := time.Duration(a.HealthInterval); got != 5*time.Second {
		t.Errorf("HealthInterval default = %v, want 5s", got)
	}
	if a.Git.Branch != "main" {
		t.Errorf("Git.Branch default = %q, want main", a.Git.Branch)
	}
	if a.Git.ComposePath != "docker-compose.yml" {
		t.Errorf("Git.ComposePath default = %q, want docker-compose.yml", a.Git.ComposePath)
	}
	if !a.Git.TokenExpiresAt.Equal(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("TokenExpiresAt = %v", a.Git.TokenExpiresAt)
	}
}

func TestLoadConfig_RejectsBadInputs(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing agent.id",
			body: strings.Replace(validConfig, `id = "test-node"`, ``, 1),
			want: "agent.id",
		},
		{
			name: "no apps",
			body: `[agent]
id = "x"`,
			want: "[[apps]]",
		},
		{
			name: "missing app name",
			body: strings.Replace(validConfig, `name = "myapp"`, ``, 1),
			want: "apps[0].name",
		},
		{
			name: "missing apps_dir",
			body: strings.Replace(validConfig, `apps_dir = "/tmp/myapp"`, ``, 1),
			want: "apps_dir",
		},
		{
			name: "invalid health_port",
			body: strings.Replace(validConfig, `health_port = 8080`, `health_port = 0`, 1),
			want: "health_port",
		},
		{
			name: "missing token",
			body: strings.Replace(validConfig, `token = "ghp_xxx"`, ``, 1),
			want: "token",
		},
		{
			name: "unsupported executor",
			body: strings.Replace(validConfig, `[agent]`, "[agent]\nexecutor = \"podman\"", 1),
			want: "docker-compose",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadConfig(writeConfig(t, tc.body))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestLoadConfig_DuplicateAppNames(t *testing.T) {
	body := validConfig + `

[[apps]]
name = "myapp"
apps_dir = "/tmp/other"
health_port = 9090

  [apps.git]
  repo_url = "https://github.com/o/r2"
  token = "ghp_yyy"
`
	_, err := LoadConfig(writeConfig(t, body))
	if err == nil {
		t.Fatal("expected duplicate name error, got nil")
	}
	if !strings.Contains(err.Error(), "duplicated") {
		t.Errorf("error %q does not mention duplication", err.Error())
	}
}

func TestLoadConfig_DurationParsing(t *testing.T) {
	body := strings.Replace(validConfig, `poll_interval = "15s"`, `poll_interval = "2m30s"`, 1)
	cfg, err := LoadConfig(writeConfig(t, body))
	if err != nil {
		t.Fatal(err)
	}
	want := 2*time.Minute + 30*time.Second
	if got := time.Duration(cfg.Agent.PollInterval); got != want {
		t.Errorf("PollInterval = %v, want %v", got, want)
	}
}
