# steadily

A load balancer that distributes traffic across backend servers, detects failure automatically, and removes/restores backends without dropping a single in-flight request.

## Hard rules

- No emojis anywhere: code, logs, docs, commit messages.
- No comments in code unless they explain a non-obvious why. No banner comments, no commented-out code, no TODOs.
- No placeholder, stubbed, or mocked implementations.
- Do not create files that are not needed. Do not add features that were not asked for.
- Go 1.22+, standard project layout (cmd/, internal/, pkg/ where genuinely public).
- Minimal dependencies. Standard library covers almost everything here (net, net/http, context, sync). Only add a dependency when the stdlib genuinely cannot do it well (YAML parsing, Prometheus client, fsnotify).
- Config is YAML, loaded from a file path given by flag or env var. No hardcoded backend lists.
- Every goroutine must have a clear owner and a clear shutdown path. No leaked goroutines, no goroutines without a way to stop them on SIGTERM.

## Stack

- Language: Go
- Config format: YAML (gopkg.in/yaml.v3)
- Metrics: prometheus/client_golang
- Logging: standard library slog, structured, no third-party logger unless justified
- Config hot reload: fsnotify watching the config file, or SIGHUP
- Tests: standard library testing, testify for assertions only
- Load testing: a small Go script or k6
- Containers: Docker Compose for the balancer plus several simple backend servers (basic HTTP servers that report their own identity and a simulated failure mode)

## Layout

- cmd/steadily: main entrypoint, flag parsing, startup, shutdown
- internal/proxy: the actual request forwarding, both L4 and L7
- internal/health: active and passive health checking
- internal/balance: load balancing algorithms (round robin, least connections, consistent hashing)
- internal/config: config loading, validation, hot reload
- internal/metrics: Prometheus metric definitions and registration
- backends/: simple test backend servers used for demos and tests
- scripts/: demo and load-test scripts
- docs/: design.md, benchmarks.md

## Commands

- go build ./...
- go vet ./...
- go test ./...
- docker compose up

Run vet and the relevant tests after every meaningful change. Fix failures before moving on.

## Correctness invariants

- A backend that fails its health check is removed from rotation before the next request is routed, not after.
- A backend that starts passing health checks again is only added back after a configurable number of consecutive successes, never on the first one (to avoid flapping).
- Config reload never drops a request that was already in flight, and never causes a moment where zero backends are considered healthy if at least one actually is.
- Connection draining: a backend marked for removal finishes its current in-flight requests but receives no new ones, and is only fully removed once those finish or a timeout is hit.
- The balancer itself never crashes or hangs due to a single misbehaving backend (timeouts on every backend call, no unbounded reads).
- Shutdown (SIGTERM) stops accepting new connections, drains in-flight ones up to a timeout, then exits.

## Code conventions

- Explicit error handling, no ignored errors. Wrap errors with context using fmt.Errorf and %w.
- Locking is explicit and documented at the point of use; prefer channels over shared mutable state where it simplifies reasoning.
- All timeouts are configurable, none are hardcoded magic numbers buried in logic.
- Structured logs carry backend address, request id, and outcome where relevant.

## Testing rules

- Health check and failover logic tested with real HTTP test servers (httptest), not mocks of the health checker itself.
- Concurrency correctness (many simultaneous requests during a backend failure) tested with real concurrent goroutines, not simulated sequentially.
- Every bug fix gets a regression test.

## Docs

- Plain technical language, no marketing tone.
- Benchmark numbers in docs come from actual measured runs, never estimated.