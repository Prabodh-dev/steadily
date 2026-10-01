package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"steadily/internal/balance"
	"steadily/internal/config"
	"steadily/internal/health"
	"steadily/internal/metrics"
)

type BackendGroup struct {
	Name      string
	Backends  []*balance.Backend
	Algorithm balance.Algorithm
}

type Pool struct {
	mu                sync.RWMutex
	backends          map[string]*balance.Backend
	groups            map[string]*BackendGroup
	routes            []config.RouteConfig
	checker           *health.Checker
	algorithm         string
	consistentHashKey string
	drainTimeout      time.Duration
}

func NewPool(cfg *config.Config) (*Pool, error) {
	algo, err := createAlgorithm(cfg.Algorithm)
	if err != nil {
		return nil, err
	}

	backends := make(map[string]*balance.Backend)
	for _, bCfg := range cfg.Backends {
		b := balance.NewBackend(bCfg.Name, bCfg.Address, bCfg.Weight)
		if bCfg.Draining {
			b.SetState(balance.StateDraining)
		}
		backends[bCfg.Name] = b
	}

	groups := make(map[string]*BackendGroup)
	for _, gCfg := range cfg.Groups {
		var gBackends []*balance.Backend
		for _, bName := range gCfg.Backends {
			if b, ok := backends[bName]; ok {
				gBackends = append(gBackends, b)
			}
		}
		groups[gCfg.Name] = &BackendGroup{
			Name:      gCfg.Name,
			Backends:  gBackends,
			Algorithm: algo,
		}
	}

	p := &Pool{
		backends:          backends,
		groups:            groups,
		routes:            cfg.Routes,
		algorithm:         cfg.Algorithm,
		consistentHashKey: cfg.ConsistentHashKey,
		drainTimeout:      cfg.DrainTimeout,
	}

	checker := health.NewChecker(cfg.HealthCheck, cfg.Mode, p.onBackendStateChange)
	p.checker = checker

	return p, nil
}

func createAlgorithm(name string) (balance.Algorithm, error) {
	switch name {
	case "round_robin":
		return balance.NewRoundRobin(), nil
	case "least_connections":
		return balance.NewLeastConnections(), nil
	case "consistent_hashing":
		return balance.NewConsistentHash(), nil
	default:
		return nil, fmt.Errorf("unknown algorithm: %s", name)
	}
}

func (p *Pool) Start(ctx context.Context) {
	var all []*balance.Backend
	p.mu.RLock()
	for _, b := range p.backends {
		if !b.IsDraining() && b.State() != balance.StateRemoved {
			all = append(all, b)
		}
	}
	p.mu.RUnlock()

	p.checker.Start(ctx, all)
	p.updateMetrics()
}

func (p *Pool) Stop() {
	if p.checker != nil {
		p.checker.Stop()
	}
}

func (p *Pool) AlgorithmName() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.algorithm
}

func (p *Pool) ConsistentHashKey() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.consistentHashKey
}

func (p *Pool) onBackendStateChange(b *balance.Backend) {
	p.updateMetrics()
}

func (p *Pool) updateMetrics() {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for groupName, group := range p.groups {
		healthyCount := 0
		drainingCount := 0
		for _, b := range group.Backends {
			if b.IsHealthy() {
				healthyCount++
			}
			if b.IsDraining() {
				drainingCount++
			}
		}
		metrics.ActiveBackends.WithLabelValues(groupName).Set(float64(healthyCount))
		metrics.DrainingBackends.WithLabelValues(groupName).Set(float64(drainingCount))
	}
}

func (p *Pool) GetGroupForPath(path string) (*BackendGroup, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var bestMatch *config.RouteConfig
	longestLen := -1

	for i := range p.routes {
		r := &p.routes[i]
		if len(r.PathPrefix) > longestLen {
			if path == r.PathPrefix || (len(path) > len(r.PathPrefix) && path[:len(r.PathPrefix)] == r.PathPrefix) || r.PathPrefix == "/" {
				bestMatch = r
				longestLen = len(r.PathPrefix)
			}
		}
	}

	if bestMatch != nil {
		if g, ok := p.groups[bestMatch.Group]; ok {
			return g, nil
		}
	}

	for _, g := range p.groups {
		return g, nil
	}

	return nil, balance.ErrNoHealthyBackends
}

func (p *Pool) GetDefaultGroup() (*BackendGroup, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, g := range p.groups {
		return g, nil
	}

	return nil, balance.ErrNoHealthyBackends
}

func (p *Pool) RecordPassiveFailure(b *balance.Backend) {
	if p.checker != nil {
		p.checker.RecordPassiveFailure(b)
	}
}

func (p *Pool) DrainBackend(name string, timeout time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.drainBackendLocked(name, timeout)
}

func (p *Pool) drainBackendLocked(name string, timeout time.Duration) error {
	b, ok := p.backends[name]
	if !ok {
		return fmt.Errorf("backend '%s' not found", name)
	}

	if b.IsDraining() || b.State() == balance.StateRemoved {
		return nil
	}

	b.SetState(balance.StateDraining)
	slog.Info("backend connection drain initiated", "backend", b.Name, "address", b.Address, "timeout", timeout.String())

	if timeout <= 0 {
		timeout = p.drainTimeout
	}

	go func(target *balance.Backend, drainMax time.Duration) {
		deadline := time.After(drainMax)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-deadline:
				slog.Warn("backend connection drain timeout reached, force removing", "backend", target.Name, "address", target.Address, "remaining_conns", target.ActiveConnections())
				p.removeBackend(target)
				return
			case <-ticker.C:
				if target.ActiveConnections() == 0 {
					slog.Info("backend connection drain completed gracefully", "backend", target.Name, "address", target.Address)
					p.removeBackend(target)
					return
				}
			}
		}
	}(b, timeout)

	return nil
}

func (p *Pool) removeBackend(b *balance.Backend) {
	b.SetState(balance.StateRemoved)
	p.mu.Lock()
	delete(p.backends, b.Name)
	p.mu.Unlock()
	p.updateMetrics()
}

func (p *Pool) HandleDrain(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("backend")
	if name == "" {
		name = r.URL.Query().Get("name")
	}
	if name == "" {
		http.Error(w, "missing 'backend' or 'name' parameter", http.StatusBadRequest)
		return
	}

	err := p.DrainBackend(name, p.drainTimeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "draining",
		"backend": name,
	})
}

func (p *Pool) UpdateConfig(cfg *config.Config) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	algo, err := createAlgorithm(cfg.Algorithm)
	if err != nil {
		return fmt.Errorf("failed to create algorithm on config update: %w", err)
	}

	configuredNames := make(map[string]config.BackendConfig)
	for _, bCfg := range cfg.Backends {
		configuredNames[bCfg.Name] = bCfg
	}

	for name, existingB := range p.backends {
		bCfg, inNewConfig := configuredNames[name]
		if !inNewConfig || bCfg.Draining {
			if !existingB.IsDraining() && existingB.State() != balance.StateRemoved {
				_ = p.drainBackendLocked(name, cfg.DrainTimeout)
			}
		}
	}

	newBackends := make(map[string]*balance.Backend)
	for name, existingB := range p.backends {
		newBackends[name] = existingB
	}

	for _, bCfg := range cfg.Backends {
		if bCfg.Draining {
			continue
		}
		if existing, ok := p.backends[bCfg.Name]; ok {
			newBackends[bCfg.Name] = existing
		} else {
			newB := balance.NewBackend(bCfg.Name, bCfg.Address, bCfg.Weight)
			newBackends[bCfg.Name] = newB
			if p.checker != nil {
				p.checker.AddBackend(newB)
			}
		}
	}

	newGroups := make(map[string]*BackendGroup)
	for _, gCfg := range cfg.Groups {
		var gBackends []*balance.Backend
		for _, bName := range gCfg.Backends {
			if b, ok := newBackends[bName]; ok {
				gBackends = append(gBackends, b)
			}
		}
		newGroups[gCfg.Name] = &BackendGroup{
			Name:      gCfg.Name,
			Backends:  gBackends,
			Algorithm: algo,
		}
	}

	p.backends = newBackends
	p.groups = newGroups
	p.routes = cfg.Routes
	p.algorithm = cfg.Algorithm
	p.consistentHashKey = cfg.ConsistentHashKey
	p.drainTimeout = cfg.DrainTimeout

	return nil
}
