package agent

import (
	"context"
	"net/http"
	"time"
)

// waitHealthy polls url every interval until it returns 200 OK or total
// elapses. Returns true on success, false on timeout or cancellation.
func waitHealthy(ctx context.Context, url string, total, interval time.Duration) bool {
	deadline := time.Now().Add(total)
	client := &http.Client{Timeout: interval}
	for {
		if probe(ctx, client, url) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		select {
		case <-time.After(interval):
		case <-ctx.Done():
			return false
		}
	}
}

func probe(ctx context.Context, client *http.Client, url string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
