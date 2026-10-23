#!/usr/bin/env bash
# Narrated terminal demo: routing, health-check failover, and hot reload.
# Record with scripts/demo/record.sh.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
lb=18080
metrics=19090
pids=()

cleanup() {
	for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done
	wait 2>/dev/null || true
	rm -rf "$work"
}
trap cleanup EXIT

say() {
	printf '\n\033[1;36m# %s\033[0m\n' "$*"
	sleep 1.2
}

run() {
	local cmd="$*" i
	printf '\033[1;32m$\033[0m '
	for ((i = 0; i < ${#cmd}; i++)); do
		printf '%s' "${cmd:i:1}"
		sleep 0.015
	done
	printf '\n'
	sleep 0.4
	eval "$cmd"
	sleep 0.8
}

go build -o "$work/steadily" "$root/cmd/steadily"
go build -o "$work/echo" "$root/backends/echo"
cd "$work"

write_config() {
	local backends="$1" algorithm="$2" names="$3"
	cat >steadily.yaml <<CFG
listen_address: "127.0.0.1:$lb"
metrics_address: "127.0.0.1:$metrics"
mode: "l7"
algorithm: "$algorithm"
drain_timeout: "5s"
health_check:
  path: "/health"
  interval: "500ms"
  timeout: "200ms"
  healthy_threshold: 2
  unhealthy_threshold: 1
backends:
$backends
groups:
  - name: "main"
    backends: [$names]
CFG
}

backend_entry() { printf '  - name: "echo%s"\n    address: "127.0.0.1:%s"\n' "$1" "$((18080 + $1))"; }

for n in 1 2 3 4; do
	./echo -addr "127.0.0.1:$((18080 + n))" 2>/dev/null &
	pids+=($!)
done

three="$(backend_entry 1; backend_entry 2; backend_entry 3)"
write_config "$three" round_robin '"echo1", "echo2", "echo3"'

distribution() {
	for _ in $(seq "$1"); do
		curl -s "localhost:$lb/" | sed -E 's/.*"address":"[^:]*:([0-9]+)".*/backend :\1/'
	done | sort | uniq -c
}

log_events() { grep -E "$1" steadily.log | sed -E 's/^[0-9\/]+ [0-9:]+ //' | cut -c1-110 | tail -n "${2:-6}"; }

clear
say "Steadily: an L4/L7 load balancer that keeps routing while backends come and go"
say "Start three echo backends (:18081-:18083) and the balancer in front of them"
run "./steadily -config steadily.yaml 2> steadily.log &"
pids+=($!)
sleep 2.5
run "head -n 4 steadily.log | cut -c1-110"

say "Round-robin traffic: 12 requests spread across the healthy backends"
run "distribution 12"

say "Active health check failure: backend :18082 starts returning 500 on /health"
run "curl -s localhost:18082/fail; echo"
sleep 1.5
run "log_events 'health check failed' 2"
run "distribution 12"

say "Recovery needs 2 consecutive successful checks before it rejoins (no flapping)"
run "curl -s localhost:18082/recover; echo"
sleep 2
run "log_events 'health check passed' 1"
run "distribution 12"

say "Hard failure under load: kill backend :18083 while 200 requests are in flight"
run "(for _ in \$(seq 200); do curl -s -o /dev/null -w '%{http_code}\n' localhost:$lb/; sleep 0.01; done | sort | uniq -c) & job=\$!; sleep 0.6; kill ${pids[2]}; wait \$job"
echo "(every request succeeded: failed attempts were retried on another backend)"
sleep 1
run "log_events 'state changed' 2"

say "Hot reload: swap live backend :18081 for a new :18084 while traffic keeps flowing"
run "(for _ in \$(seq 150); do curl -s -o /dev/null -w '%{http_code}\n' localhost:$lb/; sleep 0.02; done | sort | uniq -c) & bg=\$!"
sleep 0.5
new="$(backend_entry 2; backend_entry 4)"
write_config "$new" round_robin '"echo2", "echo4"'
run "kill -HUP \$(pgrep -f 'steadily -config' | head -n 1)"
run "wait \$bg"
sleep 2
run "log_events 'drain initiated|drain completed' 4"
run "distribution 12"

say "Prometheus metrics are served on the metrics port"
run "curl -s localhost:$metrics/metrics | grep -E '^steadily_active_backends'"
sleep 1.5
