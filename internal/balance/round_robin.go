package balance

import (
	"context"
	"sync/atomic"
)

type RoundRobin struct {
	counter uint64
}

func NewRoundRobin() *RoundRobin {
	return &RoundRobin{}
}

func (rr *RoundRobin) Name() string {
	return "round_robin"
}

func (rr *RoundRobin) Next(ctx context.Context, backends []*Backend) (*Backend, error) {
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

	idx := (atomic.AddUint64(&rr.counter, 1) - 1) % uint64(len(healthy))
	return healthy[idx], nil
}
