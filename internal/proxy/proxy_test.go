package proxy_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"steadily/internal/config"
	"steadily/internal/proxy"
)

func TestFailover500ConcurrentRequests(t *testing.T) {
	var s1Alive int32 = 1

	s1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&s1Alive) == 0 {
			w.Header().Set("Connection", "close")
			http.Error(w, "backend dead", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("backend-1"))
	}))
	defer s1.Close()

	s2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("backend-2"))
	}))
	defer s2.Close()

	s3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("backend-3"))
	}))
	defer s3.Close()

	cfg := &config.Config{
		ListenAddress:   ":0",
		Mode:            "l7",
		Algorithm:       "round_robin",
		ShutdownTimeout: 5 * time.Second,
		HealthCheck: config.HealthCheckConfig{
			Path:               "/",
			Interval:           500 * time.Millisecond,
			Timeout:            200 * time.Millisecond,
			HealthyThreshold:   1,
			UnhealthyThreshold: 1,
		},
		Backends: []config.BackendConfig{
			{Name: "b1", Address: s1.Listener.Addr().String(), Weight: 1},
			{Name: "b2", Address: s2.Listener.Addr().String(), Weight: 1},
			{Name: "b3", Address: s3.Listener.Addr().String(), Weight: 1},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("cfg validate error: %v", err)
	}

	pool, err := proxy.NewPool(cfg)
	if err != nil {
		t.Fatalf("pool creation error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool.Start(ctx)
	defer pool.Stop()

	l7Proxy := proxy.NewL7Proxy(pool)
	proxyServer := httptest.NewServer(l7Proxy)
	defer proxyServer.Close()

	time.Sleep(100 * time.Millisecond)

	totalRequests := 500
	var wg sync.WaitGroup
	var successCount int64
	var failCount int64

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 100,
		},
	}

	sem := make(chan struct{}, 50)
	var errMu sync.Mutex
	var errDetails []string

	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		reqNum := i
		go func() {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			if reqNum == 100 {
				atomic.StoreInt32(&s1Alive, 0)
				s1.CloseClientConnections()
			}

			resp, err := client.Get(proxyServer.URL)
			if err != nil {
				errMu.Lock()
				errDetails = append(errDetails, fmt.Sprintf("req %d client.Get error: %v", reqNum, err))
				errMu.Unlock()
				atomic.AddInt64(&failCount, 1)
				return
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()

			if resp.StatusCode == http.StatusOK && (string(body) == "backend-1" || string(body) == "backend-2" || string(body) == "backend-3") {
				atomic.AddInt64(&successCount, 1)
			} else {
				errMu.Lock()
				errDetails = append(errDetails, fmt.Sprintf("req %d failed: status=%d body=%q", reqNum, resp.StatusCode, string(body)))
				errMu.Unlock()
				atomic.AddInt64(&failCount, 1)
			}
		}()
	}

	wg.Wait()

	if len(errDetails) > 0 {
		for _, e := range errDetails[:10] {
			t.Logf("SAMPLE FAIL: %s", e)
		}
	}

	if failCount > 0 {
		t.Fatalf("expected 0 failed requests during backend kill, got %d failed (success=%d)", failCount, successCount)
	}

	if successCount != int64(totalRequests) {
		t.Fatalf("expected %d successful requests, got %d", totalRequests, successCount)
	}
}

func TestL7PathRoutingAndHeaders(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "api-val")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("api-backend"))
	}))
	defer apiServer.Close()

	cfg := &config.Config{
		ListenAddress:   ":0",
		Mode:            "l7",
		Algorithm:       "round_robin",
		ShutdownTimeout: 2 * time.Second,
		HealthCheck: config.HealthCheckConfig{
			Path:               "/",
			Interval:           500 * time.Millisecond,
			Timeout:            200 * time.Millisecond,
			HealthyThreshold:   1,
			UnhealthyThreshold: 1,
		},
		Backends: []config.BackendConfig{
			{Name: "api1", Address: apiServer.Listener.Addr().String(), Weight: 1},
		},
		Groups: []config.GroupConfig{
			{Name: "api-group", Backends: []string{"api1"}},
		},
		Routes: []config.RouteConfig{
			{PathPrefix: "/api", Group: "api-group"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("cfg validate error: %v", err)
	}

	pool, err := proxy.NewPool(cfg)
	if err != nil {
		t.Fatalf("pool creation error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool.Start(ctx)
	defer pool.Stop()

	l7Proxy := proxy.NewL7Proxy(pool)
	ts := httptest.NewServer(l7Proxy)
	defer ts.Close()

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("%s/api/v1/resource", ts.URL))
	if err != nil {
		t.Fatalf("failed get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Custom-Header") != "api-val" {
		t.Errorf("expected X-Custom-Header api-val, got %s", resp.Header.Get("X-Custom-Header"))
	}
}
