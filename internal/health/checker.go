package health

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"steadily/internal/balance"
	"steadily/internal/config"
	"steadily/internal/metrics"
)

type StateChangeCallback func(b *balance.Backend)

type Checker struct {
	cfg        config.HealthCheckConfig
	mode       string
	client     *http.Client
	onState    StateChangeCallback
	mu         sync.RWMutex
	checkCtx   context.Context
	cancelFunc context.CancelFunc
	wg         sync.WaitGroup
}

func NewChecker(cfg config.HealthCheckConfig, mode string, onState StateChangeCallback) *Checker {
	transport := &http.Transport{
		DisableKeepAlives: true,
		DialContext: (&net.Dialer{
			Timeout: cfg.Timeout,
		}).DialContext,
	}

	return &Checker{
		cfg:  cfg,
		mode: mode,
		client: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: transport,
		},
		onState: onState,
	}
}

func (c *Checker) Start(ctx context.Context, backends []*balance.Backend) {
	c.mu.Lock()
	checkCtx, cancel := context.WithCancel(ctx)
	c.checkCtx = checkCtx
	c.cancelFunc = cancel
	c.mu.Unlock()

	for _, b := range backends {
		c.wg.Add(1)
		go c.runLoop(checkCtx, b)
	}
}

func (c *Checker) AddBackend(b *balance.Backend) {
	c.mu.RLock()
	ctx := c.checkCtx
	c.mu.RUnlock()
	if ctx != nil {
		c.wg.Add(1)
		go c.runLoop(ctx, b)
	}
}

func (c *Checker) runLoop(ctx context.Context, b *balance.Backend) {
	defer c.wg.Done()

	c.checkBackend(ctx, b)

	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if b.State() == balance.StateDraining || b.State() == balance.StateRemoved {
				return
			}
			c.checkBackend(ctx, b)
		}
	}
}

func (c *Checker) checkBackend(ctx context.Context, b *balance.Backend) {
	if b.State() == balance.StateDraining || b.State() == balance.StateRemoved {
		return
	}

	start := time.Now()
	var err error

	if c.mode == "l7" && c.cfg.Path != "" {
		url := fmt.Sprintf("http://%s%s", b.Address, c.cfg.Path)
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if reqErr != nil {
			err = reqErr
		} else {
			req.Header.Set("X-Request-ID", "health-check")
			resp, respErr := c.client.Do(req)
			if respErr != nil {
				err = respErr
			} else {
				_ = resp.Body.Close()
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					err = fmt.Errorf("bad status code: %d", resp.StatusCode)
				}
			}
		}
	} else {
		dialer := net.Dialer{Timeout: c.cfg.Timeout}
		conn, dialErr := dialer.DialContext(ctx, "tcp", b.Address)
		if dialErr != nil {
			err = dialErr
		} else {
			_ = conn.Close()
		}
	}

	duration := time.Since(start)

	if err == nil {
		metrics.HealthCheckDuration.WithLabelValues(b.Address, "healthy").Observe(duration.Seconds())
		changed := b.MarkSuccess(c.cfg.HealthyThreshold)
		if changed {
			slog.Info("backend active health check passed, state changed", "backend", b.Name, "address", b.Address, "state", b.State().String())
			if c.onState != nil {
				c.onState(b)
			}
		}
	} else {
		metrics.HealthCheckFailuresTotal.WithLabelValues(b.Address).Inc()
		metrics.HealthCheckDuration.WithLabelValues(b.Address, "unhealthy").Observe(duration.Seconds())
		changed := b.MarkFailure(c.cfg.UnhealthyThreshold)
		if changed {
			slog.Warn("backend active health check failed, state changed", "backend", b.Name, "address", b.Address, "error", err, "state", b.State().String())
			if c.onState != nil {
				c.onState(b)
			}
		}
	}
}

func (c *Checker) CheckBackendForTest(ctx context.Context, b *balance.Backend) {
	c.checkBackend(ctx, b)
}

func (c *Checker) RecordPassiveFailure(b *balance.Backend) {
	if b.State() == balance.StateDraining || b.State() == balance.StateRemoved {
		return
	}
	metrics.HealthCheckFailuresTotal.WithLabelValues(b.Address).Inc()
	changed := b.MarkFailure(c.cfg.UnhealthyThreshold)
	if changed {
		slog.Warn("backend passive failure threshold reached, state changed", "backend", b.Name, "address", b.Address, "state", b.State().String())
		if c.onState != nil {
			c.onState(b)
		}
	}
}

func (c *Checker) Stop() {
	c.mu.Lock()
	if c.cancelFunc != nil {
		c.cancelFunc()
	}
	c.mu.Unlock()
	c.wg.Wait()
	if c.client != nil {
		c.client.CloseIdleConnections()
	}
}
