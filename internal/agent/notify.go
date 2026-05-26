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

// Notifier dispatches outbound webhook alerts. If url is empty, alerts are
// only logged.
type Notifier struct {
	url  string
	http *http.Client
}

func NewNotifier(url string) *Notifier {
	return &Notifier{
		url:  url,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

func (n *Notifier) Notify(ctx context.Context, kind, app, message string) {
	slog.Warn("argus alert", "kind", kind, "app", app, "message", message)
	if n.url == "" {
		return
	}
	payload, _ := json.Marshal(map[string]string{
		"kind":        kind,
		"application": app,
		"message":     message,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(payload))
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
