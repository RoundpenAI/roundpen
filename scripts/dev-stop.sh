#!/usr/bin/env bash
# Stop leftover make-dev processes (roundpend / Vite). pg0 is left running.
# Usage: scripts/dev-stop.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

UI_PORT=19000
API_PORT=19001
if [[ -f .env ]]; then
	# shellcheck disable=SC1091
	set -a
	# Only pull listen addr if present; ignore parse noise.
	# shellcheck source=/dev/null
	source .env 2>/dev/null || true
	set +a
fi
# ROUNDPEN_HTTP_ADDR may be :19001 or 0.0.0.0:19001
if [[ "${ROUNDPEN_HTTP_ADDR:-}" =~ :([0-9]+)$ ]]; then
	API_PORT="${BASH_REMATCH[1]}"
fi

killed=0

log() { echo "$*"; }

# Kill PIDs listening on a TCP port (best-effort).
kill_port() {
	local port="$1"
	local pids=""
	if command -v fuser >/dev/null 2>&1; then
		# fuser -k is loud; collect first then kill.
		pids="$(fuser "${port}/tcp" 2>/dev/null | tr -s ' ' '\n' | grep -E '^[0-9]+$' || true)"
	fi
	if [[ -z "$pids" ]] && command -v lsof >/dev/null 2>&1; then
		pids="$(lsof -t -iTCP:"${port}" -sTCP:LISTEN 2>/dev/null || true)"
	fi
	if [[ -z "$pids" ]] && command -v ss >/dev/null 2>&1; then
		pids="$(ss -ltnp "sport = :${port}" 2>/dev/null | sed -n 's/.*pid=\([0-9][0-9]*\).*/\1/p' | sort -u || true)"
	fi
	if [[ -z "$pids" ]]; then
		return 0
	fi
	local pid
	for pid in $pids; do
		if kill -0 "$pid" 2>/dev/null; then
			log "  kill port :${port} pid=${pid}"
			kill "$pid" 2>/dev/null || true
			killed=1
		fi
	done
}

# Kill matching processes (pattern must be specific to this repo when possible).
kill_pattern() {
	local pat="$1"
	local label="$2"
	local pids
	pids="$(pgrep -f "$pat" 2>/dev/null || true)"
	[[ -z "$pids" ]] && return 0
	local pid
	for pid in $pids; do
		# Skip self / this script's shell.
		if [[ "$pid" -eq "$$" || "$pid" -eq "$PPID" ]]; then
			continue
		fi
		if kill -0 "$pid" 2>/dev/null; then
			log "  kill ${label} pid=${pid}"
			kill "$pid" 2>/dev/null || true
			killed=1
		fi
	done
}

log "Stopping Roundpen dev processes (UI :${UI_PORT}, API :${API_PORT})..."

# By listen port (covers orphans that outlived make/dev-up).
kill_port "$API_PORT"
kill_port "$UI_PORT"

# Processes whose basename is roundpend (./bin/roundpend or go-run temp binary).
while read -r pid; do
	[[ -z "$pid" ]] && continue
	if kill -0 "$pid" 2>/dev/null; then
		log "  kill roundpend pid=${pid}"
		kill "$pid" 2>/dev/null || true
		killed=1
	fi
done < <(pgrep -x roundpend 2>/dev/null || true)

# go run parent for this repo
kill_pattern "${ROOT}/cmd/roundpend" 'go run cmd/roundpend'
kill_pattern "go run ./cmd/roundpend" 'go run ./cmd/roundpend'

# Vite / npm for this web/ tree
kill_pattern "${ROOT}/web/.*[Vv]ite" 'vite'
kill_pattern "npm run dev -- --host .* --port ${UI_PORT}" 'npm run dev'

# Give them a moment, then SIGKILL leftovers on the ports.
sleep 0.4
force_port() {
	local port="$1"
	local pids=""
	if command -v lsof >/dev/null 2>&1; then
		pids="$(lsof -t -iTCP:"${port}" -sTCP:LISTEN 2>/dev/null || true)"
	elif command -v ss >/dev/null 2>&1; then
		pids="$(ss -ltnp "sport = :${port}" 2>/dev/null | sed -n 's/.*pid=\([0-9][0-9]*\).*/\1/p' | sort -u || true)"
	fi
	[[ -z "$pids" ]] && return 0
	local pid
	for pid in $pids; do
		log "  SIGKILL port :${port} pid=${pid}"
		kill -9 "$pid" 2>/dev/null || true
		killed=1
	done
}
force_port "$API_PORT"
force_port "$UI_PORT"

if [[ "$killed" -eq 0 ]]; then
	log "Nothing to stop (ports free)."
else
	log "Done. pg0 left running (use: pg0 stop --name roundpen)."
fi
