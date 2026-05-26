package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

const (
	AlertDeployFailure = "deploy_failure"
	AlertRollback      = "rollback"
)

// Notifier dispatches outbound webhook alerts. URL is per-call so the same
// notifier serves apps with different alert endpoints. An empty URL skips
// the HTTP call but still emits a structured log.
type Notifier struct {
	http *http.Client
}

func NewNotifier() *Notifier {
	return &Notifier{http: &http.Client{Timeout: 10 * time.Second}}
}

func (n *Notifier) Notify(ctx context.Context, url, kind, app, message string) {
	slog.Warn("arguscd alert", "kind", kind, "app", app, "message", message)
	if url == "" {
		return
	}
	payload, _ := json.Marshal(map[string]string{
		"kind":        kind,
		"application": app,
		"message":     message,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		slog.Error("notify build", "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.http.Do(req)
	if err != nil {
		slog.Error("notify post", "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		slog.Error("notify non-2xx", "status", resp.StatusCode)
	}
}
