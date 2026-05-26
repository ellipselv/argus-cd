package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Git is a minimal client for fetching the latest commit and reading file
// content at a specific commit. Provider selection (github/gitlab) is via
// the per-app config; gitlab support is not yet implemented.
type Git struct {
	baseURL string
	http    *http.Client
}

func NewGit() *Git {
	return &Git{
		baseURL: "https://api.github.com",
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (g *Git) LatestSHA(ctx context.Context, app AppConfig) (string, error) {
	switch app.Git.Provider {
	case "github":
		return g.githubSHA(ctx, app)
	case "gitlab":
		return "", fmt.Errorf("gitlab provider not yet implemented")
	default:
		return "", fmt.Errorf("unknown provider %q", app.Git.Provider)
	}
}

func (g *Git) FetchCompose(ctx context.Context, app AppConfig, sha string) ([]byte, error) {
	switch app.Git.Provider {
	case "github":
		return g.githubFetch(ctx, app, sha)
	case "gitlab":
		return nil, fmt.Errorf("gitlab provider not yet implemented")
	default:
		return nil, fmt.Errorf("unknown provider %q", app.Git.Provider)
	}
}

func (g *Git) githubSHA(ctx context.Context, app AppConfig) (string, error) {
	owner, repo, err := parseGitHubRepo(app.Git.RepoURL)
	if err != nil {
		return "", err
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/commits/%s", g.baseURL, owner, repo, app.Git.Branch)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+app.Git.Token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := g.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<14))
		return "", fmt.Errorf("github commits %d: %s", resp.StatusCode, body)
	}
	var out struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode commits: %w", err)
	}
	if out.SHA == "" {
		return "", fmt.Errorf("github commits: empty sha")
	}
	return out.SHA, nil
}

func (g *Git) githubFetch(ctx context.Context, app AppConfig, sha string) ([]byte, error) {
	owner, repo, err := parseGitHubRepo(app.Git.RepoURL)
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s",
		g.baseURL, owner, repo, app.Git.ComposePath, sha)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+app.Git.Token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := g.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<14))
		return nil, fmt.Errorf("github contents %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode contents: %w", err)
	}
	if out.Encoding != "base64" {
		return nil, fmt.Errorf("unexpected content encoding %q", out.Encoding)
	}
	raw, err := base64.StdEncoding.DecodeString(stripNewlines(out.Content))
	if err != nil {
		return nil, fmt.Errorf("decode content base64: %w", err)
	}
	return raw, nil
}

func parseGitHubRepo(repoURL string) (owner, repo string, err error) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return "", "", fmt.Errorf("parse repo url: %w", err)
	}
	if u.Host != "github.com" {
		return "", "", fmt.Errorf("expected github.com host, got %q", u.Host)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("repo url path %q: expected owner/repo", u.Path)
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
}

// CheckTokenExpiry logs a structured warning when the configured PAT is
// expired or within a week of expiring. No-op if the timestamp is zero.
func CheckTokenExpiry(appName string, expiresAt time.Time) {
	if expiresAt.IsZero() {
		return
	}
	now := time.Now()
	if !expiresAt.After(now) {
		slog.Error("github token expired", "app", appName, "expired_at", expiresAt)
		return
	}
	if expiresAt.Sub(now) < 7*24*time.Hour {
		slog.Warn("github token nearing expiration",
			"app", appName,
			"expires_at", expiresAt,
			"expires_in", expiresAt.Sub(now).String(),
		)
	}
}

func stripNewlines(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' || s[i] == '\r' {
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}
