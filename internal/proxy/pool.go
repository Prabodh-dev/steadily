package proxy

import (
	"context"
	"fmt"
	"sync"

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
	mu        sync.RWMutex
	backends  map[string]*balance.Backend
	groups    map[string]*BackendGroup
	routes    []config.RouteConfig
	checker   *health.Checker
	algorithm string
}

func NewPool(cfg *config.Config) (*Pool, error) {
	var algo balance.Algorithm
	switch cfg.Algorithm {
	case "round_robin":
		algo = balance.NewRoundRobin()
	case "least_connections":
		algo = balance.NewLeastConnections()
	default:
		return nil, fmt.Errorf("unknown algorithm: %s", cfg.Algorithm)
	}

	backends := make(map[string]*balance.Backend)
	for _, bCfg := range cfg.Backends {
		backends[bCfg.Name] = balance.NewBackend(bCfg.Name, bCfg.Address, bCfg.Weight)
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
		backends:  backends,
		groups:    groups,
		routes:    cfg.Routes,
		algorithm: cfg.Algorithm,
	}

	checker := health.NewChecker(cfg.HealthCheck, cfg.Mode, p.onBackendStateChange)
	p.checker = checker

	return p, nil
}

func (p *Pool) Start(ctx context.Context) {
	var all []*balance.Backend
	p.mu.RLock()
	for _, b := range p.backends {
		all = append(all, b)
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

func (p *Pool) onBackendStateChange(b *balance.Backend) {
	p.updateMetrics()
}

func (p *Pool) updateMetrics() {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for groupName, group := range p.groups {
		healthyCount := 0
		for _, b := range group.Backends {
			if b.IsHealthy() {
				healthyCount++
			}
		}
		metrics.ActiveBackends.WithLabelValues(groupName).Set(float64(healthyCount))
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

func (p *Pool) UpdateConfig(cfg *config.Config) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var algo balance.Algorithm
	switch cfg.Algorithm {
	case "round_robin":
		algo = balance.NewRoundRobin()
	case "least_connections":
		algo = balance.NewLeastConnections()
	}

	newBackends := make(map[string]*balance.Backend)
	for _, bCfg := range cfg.Backends {
		if existing, ok := p.backends[bCfg.Name]; ok && existing.Address == bCfg.Address {
			newBackends[bCfg.Name] = existing
		} else {
			newBackends[bCfg.Name] = balance.NewBackend(bCfg.Name, bCfg.Address, bCfg.Weight)
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

	return nil
}
