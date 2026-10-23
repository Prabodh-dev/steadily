# Contributing to Steadily

Thanks for taking the time to contribute. This guide covers how to build and
test the project, how to propose a change, and where things live.

## Build and test

You need Go 1.26 or newer (see `go.mod`). Docker is only needed for the
Compose stack and the `demo-kill` script.

```bash
go build ./...
go vet ./...
gofmt -l .          # must print nothing
go test -race ./...
```

Run the balancer locally against the sample config. The sample points at
`echo1`..`echo3` hostnames from the Compose stack, so for a purely local run
start three echo backends and use a config that targets them:

```bash
go run ./backends/echo -addr :8081 &
go run ./backends/echo -addr :8082 &
go run ./backends/echo -addr :8083 &
sed 's/echo\([123]\):/127.0.0.1:/' steadily.yaml > /tmp/steadily.local.yaml
go run ./cmd/steadily -config /tmp/steadily.local.yaml
curl localhost:8080/
```

Other useful commands:

```bash
go test -run TestConnectionDraining -v ./internal/proxy   # one test
go test ./internal/config -run xxx -fuzz FuzzLoad -fuzztime 30s
go run ./scripts/chaos                                    # failure-injection suite
go run ./scripts/bench                                    # rewrites docs/benchmarks.md
docker compose up -d --build                              # full stack with Prometheus and Grafana
```

To check the release build without publishing anything, install
[goreleaser](https://goreleaser.com/install/) and run
`goreleaser build --snapshot --clean`.

## Codebase map

```
cmd/steadily/        Entry point: flags, config load, wiring, signal handling,
                     graceful shutdown. Starts the proxy and metrics listeners.
internal/config/     YAML schema (config.go), validation and defaults, and the
                     fsnotify/SIGHUP reload watcher (watcher.go).
internal/balance/    Backend type with health state machine (backend.go) and the
                     selection algorithms: round_robin, least_connections,
                     consistent_hash. All implement the Algorithm interface.
internal/health/     Active periodic probes (HTTP or TCP) and passive failure
                     recording. One goroutine per backend, cancelled via context.
internal/proxy/      pool.go    backend groups, routing, draining, hot reload
                     l7.go      HTTP reverse proxy with retries
                     l4.go      TCP proxy
                     admin.go   POST /admin/drain handler with optional token
internal/metrics/    Prometheus collectors and the /metrics handler.
backends/echo/       Tiny HTTP backend used by tests and the Compose stack.
                     /fail and /recover toggle its /health response.
scripts/             bench, chaos and demo-kill programs.
docs/                design.md (architecture) and benchmarks.md (measured results).
```

A request in L7 mode: `l7.go` picks a group by longest path prefix from the
`Pool`, asks the group's `Algorithm` for a healthy backend, proxies the request,
and on failure reports a passive failure to the health checker and retries on
another backend. State transitions on a `Backend` are atomic, which is what
keeps routing decisions race-free while health checks and reloads run.

## Correctness rules

Changes must not break these properties; they are what the project is for.

- A backend failing health checks leaves rotation before later requests are routed.
- A recovering backend rejoins only after `healthy_threshold` consecutive successes.
- Reloading config never drops in-flight requests, and never leaves an empty
  healthy set when healthy targets exist.
- A draining backend gets no new requests and keeps serving in-flight ones until
  they finish or `drain_timeout` expires.
- Upstream failures never panic or crash the process, and network calls have timeouts.
- On SIGINT or SIGTERM the process stops accepting connections, drains up to
  `shutdown_timeout`, and exits cleanly.

## Code conventions

- Prefer the standard library. Add a dependency only when it clearly earns its place.
- Every goroutine has an owner and a cancellation path (context or channel).
- Handle every error; wrap with context using `fmt.Errorf("...: %w", err)`.
- Keep mutex scope small. Use `sync.Mutex` or `sync.RWMutex` for shared state.
- Timeouts, intervals and drain durations are configurable in YAML with defaults
  set during validation.
- Comments explain non-obvious operational reasoning only. No commented-out
  code, no TODO comments, no emojis anywhere.
- Documentation is technical and factual. Benchmark numbers in docs must come
  from `go run ./scripts/bench`.

## Tests

- Test failover and health behavior against real `httptest.Server` instances,
  not mocks.
- Concurrency claims are tested with concurrent goroutines issuing requests
  while backends are shut down mid-stream (see `TestFailover500ConcurrentRequests`).
- Every bug fix includes a test that fails without the fix.
- Run `go test -race ./...` before pushing.

## Branches and commits

- Branch from `main`. Name branches `feature/<topic>`, `fix/<topic>` or
  `docs/<topic>`.
- Keep commits focused. Write the subject in the imperative mood, at most about
  72 characters ("Fix drain timeout on reload", not "fixed stuff"). Use the body
  to explain why when it is not obvious.
- Rebase on `main` before opening the PR if it has moved.

## Pull requests

A good pull request:

- does one thing and links the issue it addresses;
- explains the problem and the approach in the description;
- includes tests, including a regression test for bug fixes;
- passes `gofmt`, `go vet`, `go test -race ./...` and the CI checks, including `govulncheck`;
- updates the README, `docs/design.md` or `SECURITY.md` when behavior or trust
  assumptions change;
- is small enough to review in one sitting. For larger changes, open an issue
  first so we can agree on the approach.

Issues labeled `good first issue` are scoped for newcomers. Comment on one
before starting so work is not duplicated.

## Reporting security problems

Do not open a public issue. Follow [SECURITY.md](SECURITY.md).

## Code of conduct

Participation is governed by the [Code of Conduct](CODE_OF_CONDUCT.md).
