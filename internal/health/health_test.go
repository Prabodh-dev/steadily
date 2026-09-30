package health_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"steadily/internal/balance"
	"steadily/internal/config"
	"steadily/internal/health"
)

func TestActiveAndPassiveHealthChecking(t *testing.T) {
	var isHealthy int32 = 1

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "close")
		if atomic.LoadInt32(&isHealthy) == 1 {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer ts.Close()

	hostPort := ts.Listener.Addr().String()
	b := balance.NewBackend("test-backend", hostPort, 1)

	if b.State() != balance.StatePending {
		t.Fatalf("expected initial state to be pending, got %s", b.State().String())
	}

	cfg := config.HealthCheckConfig{
		Path:               "/health",
		Interval:           1 * time.Second,
		Timeout:            50 * time.Millisecond,
		HealthyThreshold:   2,
		UnhealthyThreshold: 2,
	}

	checker := health.NewChecker(cfg, "l7", nil)
	defer checker.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	checker.CheckBackendForTest(ctx, b)
	if b.IsHealthy() {
		t.Fatalf("backend should not be healthy after 1 success when healthy_threshold=2")
	}

	checker.CheckBackendForTest(ctx, b)
	if !b.IsHealthy() {
		t.Fatalf("expected backend to be healthy after 2 successes, state is %s", b.State().String())
	}

	atomic.StoreInt32(&isHealthy, 0)
	checker.CheckBackendForTest(ctx, b)
	if !b.IsHealthy() {
		t.Fatalf("backend should remain healthy after only 1 failure when unhealthy_threshold=2")
	}

	checker.CheckBackendForTest(ctx, b)
	if b.IsHealthy() {
		t.Fatalf("expected backend to be unhealthy after 2 failures")
	}

	atomic.StoreInt32(&isHealthy, 1)
	checker.CheckBackendForTest(ctx, b)
	if b.IsHealthy() {
		t.Fatalf("backend should not be healthy after only 1 success when healthy_threshold=2")
	}

	checker.CheckBackendForTest(ctx, b)
	if !b.IsHealthy() {
		t.Fatalf("expected backend to be healthy after 2 consecutive successes")
	}

	checker.RecordPassiveFailure(b)
	if !b.IsHealthy() {
		t.Fatalf("expected backend to remain healthy after 1 passive failure when unhealthy_threshold=2")
	}

	checker.RecordPassiveFailure(b)
	if b.IsHealthy() {
		t.Fatalf("expected passive failure threshold to mark backend unhealthy")
	}
}
