# Steadily Development Guidelines

Steadily is an L4 (TCP) and L7 (HTTP) load balancer designed to distribute traffic across backend servers, automatically detect failures, and manage backend state transitions without dropping in-flight requests.

## Correctness Invariants

- **Immediate Removal**: A backend failing active or passive health checks is removed from rotation before subsequent requests are routed.
- **Flap Prevention**: A recovering backend is returned to rotation only after reaching a configurable number of consecutive health check successes.
- **Atomic Configuration Reload**: Configuration updates must never drop in-flight requests or cause moments where zero backends are considered healthy if healthy targets exist.
- **Connection Draining**: Backends marked for removal continue serving active in-flight requests until completion or until the configured drain timeout expires, while receiving no new connections.
- **Upstream Resilience**: Upstream failures, slow responses, or unexpected socket closes must never panic or crash the load balancer process. All network operations must enforce context timeouts.
- **Graceful Process Shutdown**: Upon receiving `SIGTERM` or `SIGINT`, the balancer stops accepting new incoming connections, drains active connections up to `shutdown_timeout`, and exits cleanly.

## Technology Stack

- **Language**: Go 1.22+
- **Configuration**: YAML parsed via `gopkg.in/yaml.v3`
- **Observability**: Prometheus metrics via `prometheus/client_golang`
- **Logging**: Structured logging using standard library `log/slog`
- **Hot Reload**: File watching via `fsnotify` and `SIGHUP` signal handling
- **Testing**: Standard library `testing` package with real HTTP/TCP test servers (`httptest`)
- **Containerization**: Multi-stage Docker build and Docker Compose orchestrating the proxy, backend echo instances, Prometheus, and Grafana

## Directory Layout

- `cmd/steadily`: Main binary entrypoint, flag parsing, startup, and graceful shutdown orchestration.
- `internal/balance`: Load balancing algorithms (`round_robin`, `least_connections`, `consistent_hashing`).
- `internal/config`: YAML configuration parsing, schema validation, and hot reload watching.
- `internal/health`: Active periodic health checking and passive transport failure detection.
- `internal/metrics`: Prometheus metric registry and HTTP exporter handlers.
- `internal/proxy`: L4 TCP streaming proxy, L7 HTTP reverse proxy, and connection pool management.
- `backends/echo`: Lightweight backend HTTP echo server for integration testing and containerized stack execution.
- `scripts/`: Benchmark suites (`bench`), chaos test scripts (`chaos`), and backend kill failure verification (`demo-kill`).
- `docs/`: System design documentation (`design.md`) and measured benchmark outputs (`benchmarks.md`).

## Code Conventions and Standards

- **Minimal External Dependencies**: Standard library packages (`net`, `net/http`, `context`, `sync`, `log/slog`) are preferred unless specialized functionality requires third-party support.
- **Goroutine Ownership**: Every goroutine must have a defined owner and an explicit cancellation path via context or shutdown channel. Goroutine leaks are unacceptable.
- **Error Handling**: Ignored errors are prohibited. Errors must be explicitly handled and wrapped with diagnostic context using `fmt.Errorf("...: %w", err)`.
- **Concurrency & Locking**: Shared state synchronization must use explicit `sync.Mutex` or `sync.RWMutex` primitives with mutex lock scope minimized.
- **Configurable Timeouts**: All network timeouts, drain durations, and probe intervals must be dynamically configurable via YAML and defaults established during validation.
- **No Code Noise**: Code must be concise and self-documenting. Comments are reserved for non-obvious operational rationale. No TODO comments, banner headers, or commented-out code blocks.
- **No Emojis**: Emojis are disallowed across source code, structured logs, commit messages, and documentation.

## Verification Commands

Run code analysis, format checking, and test suites prior to committing changes:

```bash
# Verify compilation
go build ./...

# Static analysis
go vet ./...

# Execute test suite
go test ./...

# Launch local stack
docker compose up -d
```

## Testing Standards

- **Integration Testing**: Health check and failover logic must be verified against real local HTTP test servers (`httptest.Server`), avoiding artificial mock objects.
- **Concurrent Failover Testing**: Concurrency correctness during backend failure must be tested using concurrent goroutines issuing requests while backends are terminated mid-stream.
- **Regression Tests**: Every bug fix must include a corresponding unit or integration test reproducing and preventing the issue.

## Technical Documentation

- Documentation must remain technical, factual, and direct.
- Benchmark values in documentation must reflect measured runs executed via benchmark scripts.