package proxy

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"steadily/internal/balance"
	"steadily/internal/metrics"
)

var requestIDCounter uint64

type L7Proxy struct {
	pool      *Pool
	transport *http.Transport
}

func NewL7Proxy(pool *Pool) *L7Proxy {
	transport := &http.Transport{
		DisableKeepAlives: true,
		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          1000,
		MaxIdleConnsPerHost:   1000,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}

	return &L7Proxy{
		pool:      pool,
		transport: transport,
	}
}

func (p *L7Proxy) extractKey(r *http.Request) string {
	hashKeySpec := p.pool.ConsistentHashKey()
	if hashKeySpec != "" {
		if len(hashKeySpec) > 7 && (hashKeySpec[:7] == "header:" || hashKeySpec[:7] == "Header:") {
			headerName := hashKeySpec[7:]
			if val := r.Header.Get(headerName); val != "" {
				return val
			}
		} else if len(hashKeySpec) > 7 && (hashKeySpec[:7] == "cookie:" || hashKeySpec[:7] == "Cookie:") {
			cookieName := hashKeySpec[7:]
			if cookie, err := r.Cookie(cookieName); err == nil && cookie.Value != "" {
				return cookie.Value
			}
		} else if val := r.Header.Get(hashKeySpec); val != "" {
			return val
		}
	}

	clientIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && clientIP != "" {
		return clientIP
	}
	return r.RemoteAddr
}

func (p *L7Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reqID := r.Header.Get("X-Request-ID")
	if reqID == "" {
		reqID = fmt.Sprintf("req-%d", atomic.AddUint64(&requestIDCounter, 1))
	}

	group, err := p.pool.GetGroupForPath(r.URL.Path)
	if err != nil {
		slog.Error("no backend group for path", "path", r.URL.Path, "request_id", reqID)
		http.Error(w, "503 Service Unavailable", http.StatusServiceUnavailable)
		return
	}

	var bodyBytes []byte
	if r.Body != nil {
		b, readErr := io.ReadAll(r.Body)
		if readErr == nil {
			bodyBytes = b
			_ = r.Body.Close()
		}
	}

	maxRetries := len(group.Backends)
	if maxRetries == 0 {
		maxRetries = 1
	}

	triedBackends := make(map[string]bool)
	key := p.extractKey(r)
	reqCtx := balance.WithKey(r.Context(), key)

	for attempt := 0; attempt < maxRetries; attempt++ {
		backend, nextErr := group.Algorithm.Next(reqCtx, group.Backends)
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

		backend.IncrConnections()
		success := p.proxyToBackend(w, r, backend, reqID, bodyBytes)
		backend.DecrConnections()

		if success {
			return
		}
	}

	slog.Error("all backend retries failed", "group", group.Name, "path", r.URL.Path, "request_id", reqID)
	http.Error(w, "503 Service Unavailable", http.StatusServiceUnavailable)
}

func (p *L7Proxy) proxyToBackend(w http.ResponseWriter, r *http.Request, b *balance.Backend, reqID string, bodyBytes []byte) bool {
	start := time.Now()
	defer func() {
		metrics.RequestDuration.WithLabelValues(b.Address).Observe(time.Since(start).Seconds())
	}()

	targetURL := fmt.Sprintf("http://%s%s", b.Address, r.URL.RequestURI())

	var bodyReader io.Reader
	if len(bodyBytes) > 0 {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bodyReader)
	if err != nil {
		slog.Error("failed to create proxy request", "backend", b.Address, "error", err, "request_id", reqID)
		return false
	}

	for k, vv := range r.Header {
		for _, v := range vv {
			outReq.Header.Add(k, v)
		}
	}

	clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	if clientIP == "" {
		clientIP = r.RemoteAddr
	}
	if prior := r.Header.Get("X-Forwarded-For"); prior != "" {
		clientIP = prior + ", " + clientIP
	}
	outReq.Header.Set("X-Forwarded-For", clientIP)
	outReq.Header.Set("X-Forwarded-Host", r.Host)
	if r.TLS != nil {
		outReq.Header.Set("X-Forwarded-Proto", "https")
	} else {
		outReq.Header.Set("X-Forwarded-Proto", "http")
	}
	outReq.Header.Set("X-Request-ID", reqID)

	resp, err := p.transport.RoundTrip(outReq)
	algoName := p.pool.AlgorithmName()
	if err != nil || resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusGatewayTimeout {
		if resp != nil {
			_ = resp.Body.Close()
		}
		metrics.RequestsTotal.WithLabelValues(b.Address, algoName, "failure").Inc()
		p.pool.RecordPassiveFailure(b)
		if err != nil {
			slog.Warn("proxy request to backend failed", "backend", b.Address, "error", err, "request_id", reqID)
		} else {
			slog.Warn("proxy request returned error status from backend", "backend", b.Address, "status", resp.StatusCode, "request_id", reqID)
		}
		return false
	}
	defer resp.Body.Close()

	metrics.RequestsTotal.WithLabelValues(b.Address, algoName, "success").Inc()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	_, copyErr := io.Copy(w, resp.Body)
	if copyErr != nil {
		slog.Warn("error copying response body from backend", "backend", b.Address, "error", copyErr, "request_id", reqID)
	}

	slog.Info("request proxied successfully", "backend", b.Address, "status", resp.StatusCode, "request_id", reqID)
	return true
}
