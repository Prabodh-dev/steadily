# Steadily Load Balancer Design

Steadily is a high-availability L4 (TCP) and L7 (HTTP-aware) load balancer designed for zero-downtime operation and mid-traffic failover.

## Core Architecture

- **cmd/steadily**: Application entrypoint handling configuration loading, logger initialization, metrics registration, and graceful shutdown orchestration.
- **internal/config**: YAML parser, validator, and fsnotify file watcher supporting hot configuration reloading without connection drops.
- **internal/balance**: Load balancing algorithm implementations (Round-Robin and Least-Connections) backed by thread-safe atomic counters and state filtering.
- **internal/health**: Active periodically-ticking HTTP/TCP health checker goroutines combined with immediate passive failure detection on request transport errors.
- **internal/proxy**: L4 raw TCP proxying (bidirectional socket streaming) and L7 HTTP reverse proxying (header preservation, request-id propagation, path-prefix routing, and transparent request retries).
- **internal/metrics**: Prometheus metrics exporter for request counts, active backends, health check durations, and in-flight connection tracking.

## Failure Isolation and Zero Dropped Requests

1. **Passive Health Check Escalation**: When a backend connection fails or returns 502/503/504 errors during request execution, passive failure counters immediately increment. When `unhealthy_threshold` is met, the backend transitions to `unhealthy` state instantly.
2. **Transparent L7 Retry**: If a backend fails before response headers are written to the client, the L7 proxy selects the next healthy backend from the routing group and retries the request seamlessly.
3. **Flap Prevention**: A failed backend only transitions back to `healthy` state after completing `healthy_threshold` consecutive successful active health checks.
