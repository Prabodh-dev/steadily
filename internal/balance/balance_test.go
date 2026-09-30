package balance_test

import (
	"context"
	"sync"
	"testing"

	"steadily/internal/balance"
)

func TestRoundRobin(t *testing.T) {
	b1 := balance.NewBackend("b1", "127.0.0.1:8081", 1)
	b2 := balance.NewBackend("b2", "127.0.0.1:8082", 1)
	b3 := balance.NewBackend("b3", "127.0.0.1:8083", 1)

	b1.SetState(balance.StateHealthy)
	b2.SetState(balance.StateHealthy)
	b3.SetState(balance.StateHealthy)

	backends := []*balance.Backend{b1, b2, b3}
	rr := balance.NewRoundRobin()
	ctx := context.Background()

	counts := make(map[string]int)
	totalRequests := 300

	for i := 0; i < totalRequests; i++ {
		b, err := rr.Next(ctx, backends)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		counts[b.Name]++
	}

	for _, b := range backends {
		if counts[b.Name] != 100 {
			t.Errorf("expected 100 requests for %s, got %d", b.Name, counts[b.Name])
		}
	}

	b2.SetState(balance.StateUnhealthy)
	counts = make(map[string]int)

	for i := 0; i < 200; i++ {
		b, err := rr.Next(ctx, backends)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		counts[b.Name]++
	}

	if counts["b2"] != 0 {
		t.Errorf("unhealthy backend b2 served %d requests", counts["b2"])
	}
	if counts["b1"] != 100 || counts["b3"] != 100 {
		t.Errorf("expected 100 each for b1 and b3, got b1=%d, b3=%d", counts["b1"], counts["b3"])
	}
}

func TestLeastConnections(t *testing.T) {
	b1 := balance.NewBackend("b1", "127.0.0.1:8081", 1)
	b2 := balance.NewBackend("b2", "127.0.0.1:8082", 1)

	b1.SetState(balance.StateHealthy)
	b2.SetState(balance.StateHealthy)

	backends := []*balance.Backend{b1, b2}
	lc := balance.NewLeastConnections()
	ctx := context.Background()

	b1.IncrConnections()
	b1.IncrConnections()

	var wg sync.WaitGroup
	var chosen []*balance.Backend
	var mu sync.Mutex

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, err := lc.Next(ctx, backends)
			if err != nil {
				return
			}
			mu.Lock()
			chosen = append(chosen, b)
			mu.Unlock()
		}()
	}
	wg.Wait()

	for _, b := range chosen {
		if b.Name != "b2" {
			t.Errorf("least connections selected %s instead of b2 (which had 0 conns)", b.Name)
		}
	}
}
