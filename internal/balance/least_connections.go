package balance

import (
	"context"
	"sync/atomic"
)

type LeastConnections struct {
	counter uint64
}

func NewLeastConnections() *LeastConnections {
	return &LeastConnections{}
}

func (lc *LeastConnections) Name() string {
	return "least_connections"
}

func (lc *LeastConnections) Next(ctx context.Context, backends []*Backend) (*Backend, error) {
	n := len(backends)
	if n == 0 {
		return nil, ErrNoHealthyBackends
	}

	healthy := make([]*Backend, 0, n)
	for _, b := range backends {
		if b.IsHealthy() {
			healthy = append(healthy, b)
		}
	}

	if len(healthy) == 0 {
		return nil, ErrNoHealthyBackends
	}

	var best *Backend
	var minConns int64 = -1

	startIdx := atomic.AddUint64(&lc.counter, 1)

	for i := 0; i < len(healthy); i++ {
		idx := int((startIdx + uint64(i)) % uint64(len(healthy)))
		b := healthy[idx]
		conns := b.ActiveConnections()
		if minConns == -1 || conns < minConns {
			minConns = conns
			best = b
		}
	}

	return best, nil
}
