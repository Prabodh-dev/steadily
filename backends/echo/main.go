package main

import (
	"encoding/json"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"
)

type EchoResponse struct {
	Address      string            `json:"address"`
	RequestCount uint64            `json:"request_count"`
	Path         string            `json:"path"`
	Headers      map[string]string `json:"headers"`
}

type EchoServer struct {
	addr         string
	requestCount uint64
	isFailing    int32
}

func main() {
	addrFlag := flag.String("addr", ":8081", "address to listen on")
	flag.Parse()

	addr := *addrFlag
	if envAddr := os.Getenv("PORT"); envAddr != "" {
		if envAddr[0] != ':' {
			addr = ":" + envAddr
		} else {
			addr = envAddr
		}
	}

	server := &EchoServer{
		addr: addr,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", server.handleHealth)
	mux.HandleFunc("/fail", server.handleFail)
	mux.HandleFunc("/recover", server.handleRecover)
	mux.HandleFunc("/", server.handleEcho)

	slog.Info("starting echo backend server", "address", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("echo server stopped", "error", err)
		os.Exit(1)
	}
}

func (s *EchoServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if atomic.LoadInt32(&s.isFailing) == 1 {
		http.Error(w, "Unhealthy (simulated failure)", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func (s *EchoServer) handleFail(w http.ResponseWriter, r *http.Request) {
	atomic.StoreInt32(&s.isFailing, 1)
	slog.Warn("simulated health check failure enabled", "address", s.addr)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("failing mode enabled"))
}

func (s *EchoServer) handleRecover(w http.ResponseWriter, r *http.Request) {
	atomic.StoreInt32(&s.isFailing, 0)
	slog.Info("simulated health check failure disabled", "address", s.addr)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("failing mode disabled"))
}

func (s *EchoServer) handleEcho(w http.ResponseWriter, r *http.Request) {
	count := atomic.AddUint64(&s.requestCount, 1)

	headers := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	resp := EchoResponse{
		Address:      s.addr,
		RequestCount: count,
		Path:         r.URL.Path,
		Headers:      headers,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
