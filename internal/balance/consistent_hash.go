package balance

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
)

type keyCtxKey struct{}

var KeyContextKey = keyCtxKey{}

func WithKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, KeyContextKey, key)
}

func KeyFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(KeyContextKey).(string); ok {
		return v
	}
	return ""
}

type vnode struct {
	hash    uint32
	backend *Backend
}

type ConsistentHash struct {
	replicas int
}

func NewConsistentHash(replicas ...int) *ConsistentHash {
	r := 160
	if len(replicas) > 0 && replicas[0] > 0 {
		r = replicas[0]
	}
	return &ConsistentHash{
		replicas: r,
	}
}

func (ch *ConsistentHash) Name() string {
	return "consistent_hashing"
}

func (ch *ConsistentHash) hashKey(key string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return h.Sum32()
}

func (ch *ConsistentHash) Next(ctx context.Context, backends []*Backend) (*Backend, error) {
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

	if len(healthy) == 1 {
		return healthy[0], nil
	}

	ring := make([]vnode, 0, len(healthy)*ch.replicas)
	for _, b := range healthy {
		for i := 0; i < ch.replicas; i++ {
			nodeKey := fmt.Sprintf("%s#%d", b.Address, i)
			ring = append(ring, vnode{
				hash:    ch.hashKey(nodeKey),
				backend: b,
			})
		}
	}

	sort.Slice(ring, func(i, j int) bool {
		return ring[i].hash < ring[j].hash
	})

	reqKey := KeyFromContext(ctx)
	reqHash := ch.hashKey(reqKey)

	idx := sort.Search(len(ring), func(i int) bool {
		return ring[i].hash >= reqHash
	})

	if idx == len(ring) {
		idx = 0
	}

	return ring[idx].backend, nil
}
