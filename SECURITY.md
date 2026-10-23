# Security Policy

## Reporting a vulnerability

Please do not open a public issue for a security problem.

1. Preferred: use GitHub private vulnerability reporting. Go to the
   [Security tab](https://github.com/Prabodh-dev/steadily/security/advisories/new)
   of this repository and choose "Report a vulnerability".
2. Fallback: email prabodhrajmow@gmail.com with the subject line
   `steadily security`. Include the version or commit, your configuration
   (with secrets removed), and steps to reproduce.

You can expect an acknowledgement within 5 days and a status update within 14
days. Fixes are released as a new patch version and credited in the release
notes unless you prefer otherwise.

## Supported versions

Only the latest release and the `main` branch receive security fixes.

## What Steadily is, from a security point of view

Steadily is a network-facing proxy. It accepts connections from untrusted
clients and forwards them to backends you operate. This section describes
what the code does today, so you can judge where to put it in your network.

### Trust boundaries

| Boundary | Trust level | Notes |
| :--- | :--- | :--- |
| Clients to `listen_address` | Untrusted | Every request header, body, cookie and path is attacker-controlled. |
| Steadily to backends | Trusted | Backends are assumed to be operator-owned. Responses are streamed back to clients without inspection. |
| Config file and `STEADILY_CONFIG` | Trusted | Anyone who can write the config file controls routing. Protect it with file permissions. |
| `metrics_address` | Trusted network only | Serves `/metrics` and the admin endpoint. Bind it to loopback or a private interface. |

### Admin and control endpoint

There is exactly one control endpoint: `POST /admin/drain?backend=<name>`,
which puts a backend into the draining state.

- It is served only on `metrics_address`. It is not reachable through
  `listen_address`. If `metrics_address` is empty, the endpoint is disabled.
- It accepts `POST` only.
- Authentication is optional. Set `admin_token` in the config, or the
  `STEADILY_ADMIN_TOKEN` environment variable, to require
  `Authorization: Bearer <token>`. The token is compared in constant time and is
  read at startup, so changing it requires a restart. Without a token, anyone who
  can reach `metrics_address` can drain every backend; Steadily logs a warning at
  startup in that case.
- `/metrics` is unauthenticated. Metric labels include backend addresses.
- The metrics listener speaks plain HTTP, so the bearer token is sent in clear
  text. Keep it on a trusted network or put a TLS-terminating proxy in front.

Earlier development builds also exposed the drain endpoint on the public
listener, with any HTTP method and no authentication. That is fixed; see the
commit history for the change.

### TLS

Steadily does not terminate TLS and does not speak TLS to backends. Client
connections and backend connections are plain HTTP or plain TCP. Run it behind
a TLS terminator (or inside a private network) if you need encryption in
transit. Because the listener is never TLS, `X-Forwarded-Proto` is always set to
`http`; if a TLS terminator sits in front, the original value is overwritten.

### Configuration parsing and hot reload

- The file is parsed with `gopkg.in/yaml.v3` and then validated: mode,
  algorithm, durations, thresholds, `host:port` shape of backend addresses,
  unique backend and group names, and group and route references.
- Malformed input is rejected with an error. The test suite covers null
  entries, wrong types, negative and overflowing durations, bad addresses,
  dangling references and deeply nested YAML aliases, and `FuzzLoad` fuzzes the
  loader. No panic was found in the fuzzing run done for this policy.
- A failed reload leaves the running configuration untouched and logs the
  error. A reload never removes in-flight requests; removed backends drain.
- Validation does not set upper or lower bounds on operator-supplied values.
  A very small `health_check.interval` (the only constraint is that `timeout`
  is smaller) or a very long backend list increases CPU, sockets and goroutines
  proportionally, since each backend gets its own health check loop. These are
  operator-controlled and are not treated as vulnerabilities.
- Backend ports are checked for `host:port` shape only, not that the port is
  numeric, so a typo surfaces as a permanently unhealthy backend rather than a
  startup error.

### Known limitations of the data path

These are properties of the current implementation. They matter if the
proxy is exposed to the public internet; patches are welcome.

- No request size limits. In L7 mode the whole request body is read into memory
  so it can be replayed on retry. A client can make the process consume memory
  proportional to the body size it sends.
- No listener timeouts. The L7 `http.Server` has no read-header, read or idle
  timeout, and the L4 proxy has no idle timeout or connection cap, so slow
  clients can hold connections open. Per-backend dial and response-header
  timeouts are set.
- Headers are forwarded as-is, including hop-by-hop headers such as
  `Connection` and `Transfer-Encoding`.
- `X-Forwarded-For` is appended to any client-supplied value, so it can be
  spoofed. A client-supplied `X-Request-ID` is propagated as given. When
  `consistent_hash_key` names a header or cookie, the client chooses which
  backend it lands on.
- Failed requests are retried on another backend after a transport error or a
  502, 503 or 504 response regardless of method, so a non-idempotent request
  can be executed twice.
- Consistent hashing rebuilds and sorts its ring on every request. With many
  backends this is a CPU cost an attacker can amplify.

### Docker Compose stack

`docker-compose.yml` is a local demo. It publishes backend ports, runs Grafana
with anonymous Admin access and a default admin password, and runs Steadily
without an `admin_token`. Do not deploy it as is.

## Dependency and vulnerability scanning

`govulncheck ./...` runs in CI on every pull request and on every push to
`main` and fails the build on any reachable vulnerability in the module graph
or the Go standard library. Dependabot proposes updates to Go modules, GitHub
Actions and the Docker base image weekly.
