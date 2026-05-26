package nexus

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// waitHealthy polls url every interval until it returns 200 OK or grace
// expires. Returns true on success, false on timeout.
func waitHealthy(ctx context.Context, url string, grace, interval time.Duration) bool {
	deadline := time.Now().Add(grace)
	client := &http.Client{Timeout: interval}

	for {
		if ok := probe(ctx, client, url); ok {
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

func healthURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/health", port)
}
