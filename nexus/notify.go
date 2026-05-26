package nexus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Notifier posts JSON alerts to a configured webhook (e.g. a GitHub repository
// dispatch endpoint or a chat webhook). If no URL is set, alerts are logged.
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

func (n *Notifier) Notify(ctx context.Context, kind, msg string) {
	slog.Warn("nexus alert", "kind", kind, "message", msg)
	if n.url == "" {
		return
	}
	payload, _ := json.Marshal(map[string]string{
		"kind":    kind,
		"message": msg,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(payload))
	if err != nil {
		slog.Error("notify build request", "err", err)
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
		return
	}
}

// AlertKind labels used in notifications.
const (
	AlertRollback         = "rollback"
	AlertSignatureFailure = "signature_failure"
	AlertDeployFailure    = "deploy_failure"
)

// fmtAlert is a small helper to keep alert wording consistent.
func fmtAlert(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
