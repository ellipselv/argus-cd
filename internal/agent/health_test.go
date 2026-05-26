package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitHealthy_ImmediateOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	start := time.Now()
	ok := waitHealthy(context.Background(), srv.URL+"/health", time.Second, 50*time.Millisecond)
	if !ok {
		t.Fatal("expected healthy")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Errorf("took too long for immediate-OK: %v", time.Since(start))
	}
}

func TestWaitHealthy_NeverHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ok := waitHealthy(context.Background(), srv.URL+"/health", 200*time.Millisecond, 50*time.Millisecond)
	if ok {
		t.Fatal("expected unhealthy")
	}
}

func TestWaitHealthy_EventuallyHealthy(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ok := waitHealthy(context.Background(), srv.URL+"/health", time.Second, 50*time.Millisecond)
	if !ok {
		t.Fatal("expected eventual healthy")
	}
	if hits.Load() < 3 {
		t.Errorf("expected >=3 probes, got %d", hits.Load())
	}
}

func TestWaitHealthy_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	ok := waitHealthy(ctx, srv.URL+"/health", 5*time.Second, 25*time.Millisecond)
	if ok {
		t.Fatal("expected unhealthy after cancel")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Errorf("did not return promptly after cancel: %v", time.Since(start))
	}
}
