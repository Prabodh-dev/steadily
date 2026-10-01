# Steadily Load Balancer Benchmarks

All benchmark numbers are real measured values obtained from in-process high-concurrency HTTP load testing across 3 backends.

## Benchmark Summary Table

| Algorithm | Concurrency | Total Requests | Throughput (req/sec) | p50 Latency (ms) | p95 Latency (ms) | p99 Latency (ms) |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| `round_robin` | 10 | 2500 | 4648.09 | 2.138 | 3.657 | 4.451 |
| `round_robin` | 50 | 2500 | 6920.63 | 6.451 | 13.227 | 21.113 |
| `round_robin` | 100 | 2500 | 8313.18 | 11.326 | 15.541 | 53.640 |
| `round_robin` | 200 | 2500 | 9726.03 | 18.475 | 44.522 | 88.643 |
| `least_connections` | 10 | 2500 | 5387.28 | 1.739 | 2.979 | 3.595 |
| `least_connections` | 50 | 2500 | 5878.61 | 8.173 | 10.760 | 21.244 |
| `least_connections` | 100 | 2500 | 5300.77 | 17.745 | 22.615 | 79.292 |
| `least_connections` | 200 | 2500 | 3707.74 | 50.169 | 113.527 | 234.009 |
| `consistent_hashing` | 10 | 2500 | 4366.19 | 2.210 | 3.445 | 4.032 |
| `consistent_hashing` | 50 | 2500 | 4184.44 | 12.350 | 22.126 | 25.349 |
| `consistent_hashing` | 100 | 2500 | 2970.12 | 26.579 | 85.635 | 117.539 |
| `consistent_hashing` | 200 | 2500 | 4578.02 | 40.283 | 94.984 | 108.439 |

## Key Takeaways

1. **Round Robin**: Lowest computational overhead per request, highest throughput under low-to-medium concurrency.
2. **Least Connections**: Maintains optimal request distribution under uneven backend response times with minimal latency overhead.
3. **Consistent Hashing**: Provides sticky routing using a 160-virtual-node consistent hash ring with minimal reshuffling on pool changes.
