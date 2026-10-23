package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"steadily/internal/config"
	"steadily/internal/proxy"
)

func newAdminTestPool(t *testing.T) *proxy.Pool {
	t.Helper()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(backend.Close)

	cfg := &config.Config{
		ListenAddress: ":0",
		Mode:          "l7",
		Algorithm:     "round_robin",
		DrainTimeout:  time.Second,
		HealthCheck: config.HealthCheckConfig{
			Path:               "/",
			Interval:           500 * time.Millisecond,
			Timeout:            200 * time.Millisecond,
			HealthyThreshold:   1,
			UnhealthyThreshold: 1,
		},
		Backends: []config.BackendConfig{
			{Name: "b1", Address: backend.Listener.Addr().String(), Weight: 1},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("cfg validate error: %v", err)
	}

	pool, err := proxy.NewPool(cfg)
	if err != nil {
		t.Fatalf("pool creation error: %v", err)
	}
	return pool
}

func TestAdminDrainRequiresPost(t *testing.T) {
	handler := newAdminTestPool(t).AdminHandler("")

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(method, "/admin/drain?backend=b1", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: expected 405, got %d", method, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/drain?backend=b1", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("POST: expected 200, got %d", rec.Code)
	}
}

func TestAdminDrainTokenEnforced(t *testing.T) {
	pool := newAdminTestPool(t)
	handler := pool.AdminHandler("s3cret")

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"wrong", "Bearer nope", http.StatusUnauthorized},
		{"wrong scheme", "Basic s3cret", http.StatusUnauthorized},
		{"valid", "Bearer s3cret", http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/admin/drain?backend=b1", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("expected %d, got %d", tc.want, rec.Code)
			}
		})
	}
}
