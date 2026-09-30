# Steadily Load Balancer Benchmarks

## Micro-Benchmark Performance

Measured on local Go test harness with 500 concurrent requests across 3 backends during live backend kill:

- Total Requests: 500
- Success Rate: 100.0% (0 dropped requests)
- Test Duration: 0.16 seconds
- Algorithm: Round-Robin with atomic health filtering

## High Concurrency Failover Verification

- 500 simultaneous concurrent goroutines with mid-stream container kill
- Passive failover latency: < 1ms
- Recovery threshold verification: 2 consecutive health checks before re-adding backend
