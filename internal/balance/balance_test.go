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

func TestConsistentHash(t *testing.T) {
	b1 := balance.NewBackend("b1", "127.0.0.1:8081", 1)
	b2 := balance.NewBackend("b2", "127.0.0.1:8082", 1)
	b1.SetState(balance.StateHealthy)
	b2.SetState(balance.StateHealthy)

	ch := balance.NewConsistentHash()
	backends := []*balance.Backend{b1, b2}

	ctx1 := balance.WithKey(context.Background(), "user-123")
	got1, err := ch.Next(ctx1, backends)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got2, err := ch.Next(ctx1, backends)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got1.Name != got2.Name {
		t.Errorf("expected same backend for same key, got %s and %s", got1.Name, got2.Name)
	}
}

func TestConsistentHashReshuffle(t *testing.T) {
	b1 := balance.NewBackend("b1", "127.0.0.1:8081", 1)
	b2 := balance.NewBackend("b2", "127.0.0.1:8082", 1)
	b3 := balance.NewBackend("b3", "127.0.0.1:8083", 1)
	b4 := balance.NewBackend("b4", "127.0.0.1:8084", 1)
	b5 := balance.NewBackend("b5", "127.0.0.1:8085", 1)

	b1.SetState(balance.StateHealthy)
	b2.SetState(balance.StateHealthy)
	b3.SetState(balance.StateHealthy)
	b4.SetState(balance.StateHealthy)
	b5.SetState(balance.StateHealthy)

	ch := balance.NewConsistentHash()

	initialPool := []*balance.Backend{b1, b2, b3, b4}
	expandedPool := []*balance.Backend{b1, b2, b3, b4, b5}

	numKeys := 1000
	initialAssignments := make(map[int]string)
	moduloInitial := make(map[int]int)

	for i := 0; i < numKeys; i++ {
		keyStr := balance.WithKey(context.Background(), "client-ip-"+string(rune(i)))
		b, err := ch.Next(keyStr, initialPool)
		if err != nil {
			t.Fatalf("failed initial assignment for key %d: %v", i, err)
		}
		initialAssignments[i] = b.Name
		moduloInitial[i] = i % 4
	}

	chReshufflings := 0
	moduloReshufflings := 0

	for i := 0; i < numKeys; i++ {
		keyStr := balance.WithKey(context.Background(), "client-ip-"+string(rune(i)))
		b, err := ch.Next(keyStr, expandedPool)
		if err != nil {
			t.Fatalf("failed expanded assignment for key %d: %v", i, err)
		}
		if b.Name != initialAssignments[i] {
			chReshufflings++
		}
		if (i % 5) != moduloInitial[i] {
			moduloReshufflings++
		}
	}

	chPct := float64(chReshufflings) / float64(numKeys) * 100
	modPct := float64(moduloReshufflings) / float64(numKeys) * 100

	t.Logf("Reshuffling percentage: Consistent Hashing = %.2f%%, Modulo Hashing = %.2f%%", chPct, modPct)

	if chPct >= modPct {
		t.Errorf("expected consistent hashing reshuffle (%.2f%%) to be lower than modulo hashing (%.2f%%)", chPct, modPct)
	}
	if chPct > 35.0 {
		t.Errorf("consistent hashing reshuffle rate higher than expected: %.2f%%", chPct)
	}
}

