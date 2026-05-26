package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseGitHubRepo(t *testing.T) {
	cases := []struct {
		in      string
		owner   string
		repo    string
		wantErr bool
	}{
		{"https://github.com/octocat/hello", "octocat", "hello", false},
		{"https://github.com/octocat/hello.git", "octocat", "hello", false},
		{"https://github.com/o/r/extra/path", "o", "r", false},
		{"https://gitlab.com/o/r", "", "", true},
		{"https://github.com/onlyOwner", "", "", true},
		{"https://github.com/", "", "", true},
		{"::not a url", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			owner, repo, err := parseGitHubRepo(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if owner != tc.owner || repo != tc.repo {
				t.Errorf("got (%q, %q), want (%q, %q)", owner, repo, tc.owner, tc.repo)
			}
		})
	}
}

func TestCheckTokenExpiry(t *testing.T) {
	// Smoke check: doesn't panic and doesn't error for known states. We're
	// only verifying it accepts each shape; the log output isn't easily
	// asserted without intercepting slog.
	CheckTokenExpiry("app", time.Time{})                            // zero — no-op
	CheckTokenExpiry("app", time.Now().Add(30*24*time.Hour))        // well in future — no warn
	CheckTokenExpiry("app", time.Now().Add(2*24*time.Hour))         // <7 days — warn
	CheckTokenExpiry("app", time.Now().Add(-1*time.Hour))           // expired — error log
}

func TestGitHub_LatestSHA(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		if r.URL.Path != "/repos/o/r/commits/main" {
			t.Errorf("path = %q", r.URL.Path)
		}
		fmt.Fprintln(w, `{"sha": "abc123"}`)
	}))
	defer srv.Close()

	g := NewGit()
	g.baseURL = srv.URL
	sha, err := g.LatestSHA(context.Background(), AppConfig{
		Git: GitConfig{
			Provider: "github",
			RepoURL:  "https://github.com/o/r",
			Branch:   "main",
			Token:    "test-token",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sha != "abc123" {
		t.Errorf("sha = %q, want abc123", sha)
	}
}

func TestGitHub_LatestSHA_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(w, `{"message": "Not Found"}`)
	}))
	defer srv.Close()

	g := NewGit()
	g.baseURL = srv.URL
	_, err := g.LatestSHA(context.Background(), AppConfig{
		Git: GitConfig{
			Provider: "github",
			RepoURL:  "https://github.com/o/r",
			Branch:   "main",
			Token:    "t",
		},
	})
	if err == nil {
		t.Fatal("expected error on 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error %q does not mention 404", err.Error())
	}
}

func TestGitHub_FetchCompose(t *testing.T) {
	expected := []byte("services:\n  api:\n    image: x\n")
	// GitHub returns content with embedded newlines per RFC 4648.
	chunked := chunkBase64(base64.StdEncoding.EncodeToString(expected))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ref") != "deadbeef" {
			t.Errorf("ref = %q", r.URL.Query().Get("ref"))
		}
		fmt.Fprintf(w, `{"content": %q, "encoding": "base64"}`, chunked)
	}))
	defer srv.Close()

	g := NewGit()
	g.baseURL = srv.URL
	body, err := g.FetchCompose(context.Background(), AppConfig{
		Git: GitConfig{
			Provider:    "github",
			RepoURL:     "https://github.com/o/r",
			ComposePath: "docker-compose.yml",
			Token:       "t",
		},
	}, "deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(expected) {
		t.Errorf("body = %q, want %q", body, expected)
	}
}

func TestGitHub_FetchCompose_WrongEncoding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"content": "raw bytes", "encoding": "utf-8"}`)
	}))
	defer srv.Close()

	g := NewGit()
	g.baseURL = srv.URL
	_, err := g.FetchCompose(context.Background(), AppConfig{
		Git: GitConfig{
			Provider:    "github",
			RepoURL:     "https://github.com/o/r",
			ComposePath: "x.yml",
			Token:       "t",
		},
	}, "sha")
	if err == nil {
		t.Fatal("expected error on non-base64 encoding")
	}
	if !strings.Contains(err.Error(), "encoding") {
		t.Errorf("error %q does not mention encoding", err.Error())
	}
}

func TestGit_UnknownProvider(t *testing.T) {
	g := NewGit()
	_, err := g.LatestSHA(context.Background(), AppConfig{Git: GitConfig{Provider: "bitbucket"}})
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("LatestSHA bitbucket err = %v", err)
	}
	_, err = g.FetchCompose(context.Background(), AppConfig{Git: GitConfig{Provider: "bitbucket"}}, "sha")
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("FetchCompose bitbucket err = %v", err)
	}
}

func TestGit_GitLabNotImplemented(t *testing.T) {
	g := NewGit()
	_, err := g.LatestSHA(context.Background(), AppConfig{Git: GitConfig{Provider: "gitlab"}})
	if err == nil || !strings.Contains(err.Error(), "gitlab") {
		t.Errorf("LatestSHA gitlab err = %v", err)
	}
}

// chunkBase64 inserts newlines every 60 chars to mimic GitHub's Contents API
// response formatting (RFC 4648 §3.3).
func chunkBase64(s string) string {
	const width = 60
	var b strings.Builder
	for i := 0; i < len(s); i += width {
		end := min(i+width, len(s))
		b.WriteString(s[i:end])
		b.WriteByte('\n')
	}
	return b.String()
}
