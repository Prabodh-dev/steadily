package proxy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"steadily/internal/balance"
	"steadily/internal/metrics"
)

type L4Proxy struct {
	pool        *Pool
	dialTimeout time.Duration
	listener    net.Listener
	connsWg     sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
}

func NewL4Proxy(pool *Pool, dialTimeout time.Duration) *L4Proxy {
	if dialTimeout <= 0 {
		dialTimeout = 3 * time.Second
	}
	return &L4Proxy{
		pool:        pool,
		dialTimeout: dialTimeout,
	}
}

func (p *L4Proxy) ListenAndServe(ctx context.Context, listenAddr string) error {
	p.ctx, p.cancel = context.WithCancel(ctx)

	lc := net.ListenConfig{}
	listener, err := lc.Listen(p.ctx, "tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("l4 proxy listen error on %s: %w", listenAddr, err)
	}
	p.listener = listener

	slog.Info("l4 proxy listening", "address", listenAddr)

	go func() {
		<-p.ctx.Done()
		_ = p.listener.Close()
	}()

	for {
		clientConn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-p.ctx.Done():
				return nil
			default:
				slog.Error("l4 accept connection error", "error", err)
				continue
			}
		}

		p.connsWg.Add(1)
		go p.handleConn(clientConn)
	}
}

func (p *L4Proxy) handleConn(clientConn net.Conn) {
	defer p.connsWg.Done()
	defer clientConn.Close()

	reqID := fmt.Sprintf("tcp-%d", atomic.AddUint64(&requestIDCounter, 1))

	group, err := p.pool.GetDefaultGroup()
	if err != nil {
		slog.Error("l4 proxy has no default group", "request_id", reqID)
		return
	}

	maxRetries := len(group.Backends)
	if maxRetries == 0 {
		maxRetries = 1
	}

	var backendConn net.Conn
	var chosenBackend *balance.Backend

	triedBackends := make(map[string]bool)

	for attempt := 0; attempt < maxRetries; attempt++ {
		backend, nextErr := group.Algorithm.Next(p.ctx, group.Backends)
		if nextErr != nil {
			break
		}

		if triedBackends[backend.Address] && len(triedBackends) < len(group.Backends) {
			foundUntried := false
			for _, b := range group.Backends {
				if b.IsHealthy() && !triedBackends[b.Address] {
					backend = b
					foundUntried = true
					break
				}
			}
			if !foundUntried {
				break
			}
		}
		triedBackends[backend.Address] = true

		dialer := net.Dialer{Timeout: p.dialTimeout}
		conn, dialErr := dialer.DialContext(p.ctx, "tcp", backend.Address)
		if dialErr != nil {
			metrics.RequestsTotal.WithLabelValues(backend.Address, "failure").Inc()
			p.pool.RecordPassiveFailure(backend)
			slog.Warn("l4 backend tcp dial failed", "backend", backend.Address, "error", dialErr, "request_id", reqID)
			continue
		}

		backendConn = conn
		chosenBackend = backend
		metrics.RequestsTotal.WithLabelValues(backend.Address, "success").Inc()
		break
	}

	if backendConn == nil || chosenBackend == nil {
		slog.Error("l4 proxy failed to connect to any backend", "request_id", reqID)
		return
	}
	defer backendConn.Close()

	chosenBackend.IncrConnections()
	defer chosenBackend.DecrConnections()

	slog.Info("l4 connection established", "backend", chosenBackend.Address, "client", clientConn.RemoteAddr().String(), "request_id", reqID)

	var pipeWg sync.WaitGroup
	pipeWg.Add(2)

	go func() {
		defer pipeWg.Done()
		_, _ = io.Copy(backendConn, clientConn)
		if tcpConn, ok := backendConn.(*net.TCPConn); ok {
			_ = tcpConn.CloseWrite()
		}
	}()

	go func() {
		defer pipeWg.Done()
		_, _ = io.Copy(clientConn, backendConn)
		if tcpConn, ok := clientConn.(*net.TCPConn); ok {
			_ = tcpConn.CloseWrite()
		}
	}()

	pipeWg.Wait()
	slog.Info("l4 connection closed", "backend", chosenBackend.Address, "request_id", reqID)
}

func (p *L4Proxy) Close(ctx context.Context) error {
	if p.cancel != nil {
		p.cancel()
	}
	if p.listener != nil {
		_ = p.listener.Close()
	}

	done := make(chan struct{})
	go func() {
		p.connsWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
