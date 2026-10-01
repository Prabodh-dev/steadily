# Steadily Load Balancer

Steadily is a high-availability L4 (TCP) and L7 (HTTP) load balancer written in Go. It distributes traffic across backend servers, detects failure automatically, drains connections gracefully during backend removal, and reloads configuration dynamically without dropping in-flight requests.

## Features

- **L4 and L7 Proxying**: Supports raw TCP streaming (L4) and HTTP header-aware routing, path matching, and automatic request retries (L7).
- **Load Balancing Algorithms**: Round-Robin, Least-Connections, and Consistent Hashing (using a 160-virtual-node ring with low key reshuffling).
- **Health Checking**: Periodically active TCP/HTTP health checks combined with immediate passive failure detection upon transport errors.
- **Connection Draining**: Backends marked draining receive no new traffic while allowing in-flight requests to complete up to a configurable timeout.
- **Hot Config Reloading**: Dynamic configuration watching via `fsnotify` and `SIGHUP` signal handling without dropped requests.
- **Observability**: Prometheus metrics endpoint (`/metrics`) exposing request rates, error rates, per-backend latency histograms, active backends, and health check failure counters, alongside Grafana dashboards.

## Quickstart

### Prerequisites

- Go 1.22+
- Docker and Docker Compose

### Building and Running Locally

```bash
# Build binary
go build ./cmd/steadily

# Run tests
go test ./...

# Run Steadily with configuration
./steadily -config steadily.yaml
```

### Running with Docker Compose

To launch Steadily along with 3 echo backend instances, Prometheus, and Grafana:

```bash
docker compose up -d --build
```

Access points:
- Steadily Proxy: `http://localhost:8080/`
- Prometheus Metrics: `http://localhost:9090/metrics`
- Prometheus Server UI: `http://localhost:9091`
- Grafana Dashboard: `http://localhost:3000` (provisioned dashboard)

## Configuration Reference

Steadily loads YAML configuration provided via `-config` flag or `STEADILY_CONFIG` environment variable.

```yaml
listen_address: ":8080"
metrics_address: ":9090"
mode: "l7" # "l4" or "l7"
algorithm: "round_robin" # "round_robin", "least_connections", "consistent_hashing"
consistent_hash_key: "header:X-User-ID" # "header:<Name>", "cookie:<Name>", or "client_ip"
shutdown_timeout: "5s"
drain_timeout: "10s"

health_check:
  path: "/health"
  interval: "1s"
  timeout: "500ms"
  healthy_threshold: 1
  unhealthy_threshold: 1

backends:
  - name: "echo1"
    address: "echo1:8081"
    weight: 1
    draining: false
  - name: "echo2"
    address: "echo2:8082"
    weight: 1
  - name: "echo3"
    address: "echo3:8083"
    weight: 1

groups:
  - name: "main"
    backends: ["echo1", "echo2", "echo3"]

routes:
  - path_prefix: "/"
    group: "main"
```

## Control Endpoint

Mark a backend as draining via control API:

```bash
curl -X POST "http://localhost:8080/admin/drain?backend=echo1"
```

## Demo and Test Scripts

- **Demo Kill Backend**: Fires concurrent requests while killing a backend container mid-stream:
  ```bash
  go run ./scripts/demo-kill.go
  ```

- **Chaos Test**: Continuously sends traffic while randomly killing and restarting containers:
  ```bash
  go run ./scripts/chaos.go
  ```

- **Benchmark Suite**: Benchmarks throughput and p50/p95/p99 latency added by Steadily across algorithms and concurrency levels, updating `docs/benchmarks.md`:
  ```bash
  go run ./scripts/bench.go
  ```

## Development Commands

- `go build ./...`
- `go vet ./...`
- `go test ./...`
- `docker compose up -d`
