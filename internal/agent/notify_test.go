package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestNotifier_EmptyURL_NoNetwork(t *testing.T) {
	// With no URL, the Notifier should silently log and not panic.
	NewNotifier().Notify(context.Background(), "", AlertRollback, "app", "msg")
}

func TestNotifier_PayloadShape(t *testing.T) {
	var got struct {
		Kind        string `json:"kind"`
		Application string `json:"application"`
		Message     string `json:"message"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	NewNotifier().Notify(context.Background(), srv.URL, AlertDeployFailure, "backend", "compose up failed")

	if got.Kind != AlertDeployFailure {
		t.Errorf("kind = %q", got.Kind)
	}
	if got.Application != "backend" {
		t.Errorf("application = %q", got.Application)
	}
	if got.Message != "compose up failed" {
		t.Errorf("message = %q", got.Message)
	}
}

func TestNotifier_TolerantOfNon2xx(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	NewNotifier().Notify(context.Background(), srv.URL, AlertRollback, "app", "x")
	if hits.Load() != 1 {
		t.Errorf("expected 1 webhook hit, got %d", hits.Load())
	}
}
