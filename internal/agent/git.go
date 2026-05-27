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
// the per-app config.
type Git struct {
	// baseURL, if non-empty, overrides the per-provider API base for all
	// providers. Used by tests and the smoke harness.
	baseURL string
	http    *http.Client
}

func NewGit() *Git {
	return &Git{
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

func (g *Git) LatestSHA(ctx context.Context, app AppConfig) (string, error) {
	switch app.Git.Provider {
	case "github":
		return g.githubSHA(ctx, app)
	case "gitlab":
		return g.gitlabSHA(ctx, app)
	default:
		return "", fmt.Errorf("unknown provider %q", app.Git.Provider)
	}
}

func (g *Git) FetchCompose(ctx context.Context, app AppConfig, sha string) ([]byte, error) {
	switch app.Git.Provider {
	case "github":
		return g.githubFetch(ctx, app, sha)
	case "gitlab":
		return g.gitlabFetch(ctx, app, sha)
	default:
		return nil, fmt.Errorf("unknown provider %q", app.Git.Provider)
	}
}

func (g *Git) githubBase() string {
	if g.baseURL != "" {
		return g.baseURL
	}
	return "https://api.github.com"
}

func (g *Git) gitlabBase(host string) string {
	if g.baseURL != "" {
		return g.baseURL
	}
	return "https://" + host + "/api/v4"
}

func (g *Git) githubSHA(ctx context.Context, app AppConfig) (string, error) {
	owner, repo, err := parseGitHubRepo(app.Git.RepoURL)
	if err != nil {
		return "", err
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/commits/%s", g.githubBase(), owner, repo, app.Git.Branch)
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
		g.githubBase(), owner, repo, app.Git.ComposePath, sha)
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
	return decodeBase64Content(resp.Body)
}

func (g *Git) gitlabSHA(ctx context.Context, app AppConfig) (string, error) {
	host, project, err := parseGitLabRepo(app.Git.RepoURL)
	if err != nil {
		return "", err
	}
	endpoint := fmt.Sprintf("%s/projects/%s/repository/commits/%s",
		g.gitlabBase(host),
		url.PathEscape(project),
		url.PathEscape(app.Git.Branch),
	)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("PRIVATE-TOKEN", app.Git.Token)

	resp, err := g.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<14))
		return "", fmt.Errorf("gitlab commits %d: %s", resp.StatusCode, body)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode commits: %w", err)
	}
	if out.ID == "" {
		return "", fmt.Errorf("gitlab commits: empty sha")
	}
	return out.ID, nil
}

func (g *Git) gitlabFetch(ctx context.Context, app AppConfig, sha string) ([]byte, error) {
	host, project, err := parseGitLabRepo(app.Git.RepoURL)
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/projects/%s/repository/files/%s?ref=%s",
		g.gitlabBase(host),
		url.PathEscape(project),
		url.PathEscape(app.Git.ComposePath),
		url.QueryEscape(sha),
	)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("PRIVATE-TOKEN", app.Git.Token)

	resp, err := g.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<14))
		return nil, fmt.Errorf("gitlab contents %d: %s", resp.StatusCode, body)
	}
	return decodeBase64Content(resp.Body)
}

// decodeBase64Content reads a {"content": "<b64>", "encoding": "base64"} body,
// which both the GitHub Contents API and GitLab Repository Files API return.
func decodeBase64Content(body io.Reader) ([]byte, error) {
	var out struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.NewDecoder(body).Decode(&out); err != nil {
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

// parseGitLabRepo extracts the host and the full project path (which may be
// nested through subgroups, e.g. "group/subgroup/repo") from a GitLab repo URL.
// The path is returned unescaped; callers must url.PathEscape it before
// embedding in the API URL.
func parseGitLabRepo(repoURL string) (host, project string, err error) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return "", "", fmt.Errorf("parse repo url: %w", err)
	}
	if u.Host == "" {
		return "", "", fmt.Errorf("repo url missing host: %q", repoURL)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" {
		return "", "", fmt.Errorf("repo url path %q: expected at least group/repo", u.Path)
	}
	parts[len(parts)-1] = strings.TrimSuffix(parts[len(parts)-1], ".git")
	return u.Host, strings.Join(parts, "/"), nil
}

// CheckTokenExpiry logs a structured warning when the configured PAT is
// expired or within a week of expiring. No-op if the timestamp is zero.
func CheckTokenExpiry(appName string, expiresAt time.Time) {
	if expiresAt.IsZero() {
		return
	}
	now := time.Now()
	if !expiresAt.After(now) {
		slog.Error("git token expired", "app", appName, "expired_at", expiresAt)
		return
	}
	if expiresAt.Sub(now) < 7*24*time.Hour {
		slog.Warn("git token nearing expiration",
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
