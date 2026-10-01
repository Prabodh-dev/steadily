package balance

import (
	"log/slog"
	"sync/atomic"

	"steadily/internal/metrics"
)

type HealthState int32

const (
	StatePending   HealthState = 0
	StateHealthy   HealthState = 1
	StateUnhealthy HealthState = 2
	StateDraining  HealthState = 3
	StateRemoved   HealthState = 4
)

func (s HealthState) String() string {
	switch s {
	case StatePending:
		return "pending"
	case StateHealthy:
		return "healthy"
	case StateUnhealthy:
		return "unhealthy"
	case StateDraining:
		return "draining"
	case StateRemoved:
		return "removed"
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

func (b *Backend) IsDraining() bool {
	return atomic.LoadInt32(&b.state) == int32(StateDraining)
}

func (b *Backend) State() HealthState {
	return HealthState(atomic.LoadInt32(&b.state))
}

func (b *Backend) SetState(s HealthState) {
	oldState := HealthState(atomic.SwapInt32(&b.state, int32(s)))
	if oldState != s {
		slog.Info("backend state changed", "backend", b.Name, "address", b.Address, "from", oldState.String(), "to", s.String())
	}
}

func (b *Backend) MarkSuccess(healthyThreshold int) bool {
	if b.IsDraining() || b.State() == StateRemoved {
		return false
	}
	atomic.StoreInt32(&b.consecutiveFailures, 0)
	succ := atomic.AddInt32(&b.consecutiveSuccesses, 1)

	if int(succ) >= healthyThreshold {
		oldState := HealthState(atomic.SwapInt32(&b.state, int32(StateHealthy)))
		if oldState != StateHealthy {
			slog.Info("backend state changed", "backend", b.Name, "address", b.Address, "from", oldState.String(), "to", "healthy")
			return true
		}
	}
	return false
}

func (b *Backend) MarkFailure(unhealthyThreshold int) bool {
	if b.IsDraining() || b.State() == StateRemoved {
		return false
	}
	atomic.StoreInt32(&b.consecutiveSuccesses, 0)
	fail := atomic.AddInt32(&b.consecutiveFailures, 1)

	if int(fail) >= unhealthyThreshold {
		oldState := HealthState(atomic.SwapInt32(&b.state, int32(StateUnhealthy)))
		if oldState != StateUnhealthy {
			slog.Warn("backend state changed", "backend", b.Name, "address", b.Address, "from", oldState.String(), "to", "unhealthy")
			return true
		}
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

