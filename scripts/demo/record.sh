#!/usr/bin/env bash
# Regenerates docs/demo.cast and docs/demo.svg.
# Requires asciinema (pip install asciinema) and node (npx svg-term-cli).
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"

asciinema rec --overwrite --cols 110 --rows 36 \
	--command "bash $root/scripts/demo/demo.sh" \
	"$root/docs/demo.cast"

npx --yes svg-term-cli@2.1.1 --in "$root/docs/demo.cast" --out "$root/docs/demo.svg" \
	--window --no-cursor --width 110 --height 36
