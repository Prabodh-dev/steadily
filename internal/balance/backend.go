package balance

import (
	"sync/atomic"

	"steadily/internal/metrics"
)

type HealthState int32

const (
	StatePending   HealthState = 0
	StateHealthy   HealthState = 1
	StateUnhealthy HealthState = 2
)

func (s HealthState) String() string {
	switch s {
	case StatePending:
		return "pending"
	case StateHealthy:
		return "healthy"
	case StateUnhealthy:
		return "unhealthy"
	default:
		return "unknown"
	}
}

type Backend struct {
	Name                 string
	Address              string
	Weight               int
	state                int32
	activeConns          int64
	consecutiveSuccesses int32
	consecutiveFailures  int32
}

func NewBackend(name, address string, weight int) *Backend {
	if weight < 1 {
		weight = 1
	}
	return &Backend{
		Name:    name,
		Address: address,
		Weight:  weight,
		state:   int32(StatePending),
	}
}

func (b *Backend) IsHealthy() bool {
	return atomic.LoadInt32(&b.state) == int32(StateHealthy)
}

func (b *Backend) State() HealthState {
	return HealthState(atomic.LoadInt32(&b.state))
}

func (b *Backend) SetState(s HealthState) {
	atomic.StoreInt32(&b.state, int32(s))
}

func (b *Backend) MarkSuccess(healthyThreshold int) bool {
	atomic.StoreInt32(&b.consecutiveFailures, 0)
	succ := atomic.AddInt32(&b.consecutiveSuccesses, 1)

	if int(succ) >= healthyThreshold {
		oldState := atomic.SwapInt32(&b.state, int32(StateHealthy))
		return oldState != int32(StateHealthy)
	}
	return false
}

func (b *Backend) MarkFailure(unhealthyThreshold int) bool {
	atomic.StoreInt32(&b.consecutiveSuccesses, 0)
	fail := atomic.AddInt32(&b.consecutiveFailures, 1)

	if int(fail) >= unhealthyThreshold {
		oldState := atomic.SwapInt32(&b.state, int32(StateUnhealthy))
		return oldState != int32(StateUnhealthy)
	}
	return false
}

func (b *Backend) IncrConnections() {
	val := atomic.AddInt64(&b.activeConns, 1)
	metrics.InFlightRequests.WithLabelValues(b.Address).Set(float64(val))
}

func (b *Backend) DecrConnections() {
	val := atomic.AddInt64(&b.activeConns, -1)
	if val < 0 {
		atomic.StoreInt64(&b.activeConns, 0)
		val = 0
	}
	metrics.InFlightRequests.WithLabelValues(b.Address).Set(float64(val))
}

func (b *Backend) ActiveConnections() int64 {
	return atomic.LoadInt64(&b.activeConns)
}
