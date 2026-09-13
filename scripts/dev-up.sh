#!/usr/bin/env bash
# Host-side local preview: check tools, start pg0, then roundpend + Vite.
# Usage: scripts/dev-up.sh [--check-only]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

CHECK_ONLY=0
if [[ "${1:-}" == "--check-only" ]]; then
	CHECK_ONLY=1
fi

UI_PORT=19000
API_PORT=19001
# Bind dev servers on all interfaces so LAN clients can reach them.
DEV_BIND="${ROUNDPEN_DEV_BIND:-0.0.0.0}"
LAN_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
[[ -z "$LAN_IP" ]] && LAN_IP="127.0.0.1"
PG0_NAME=roundpen
PG0_PORT=5432
PG0_USER=roundpen
PG0_PASS=roundpen
PG0_DB=roundpen
DSN_DEFAULT="postgres://${PG0_USER}:${PG0_PASS}@127.0.0.1:${PG0_PORT}/${PG0_DB}?sslmode=disable"
TEST_DSN_DEFAULT="postgres://${PG0_USER}:${PG0_PASS}@127.0.0.1:${PG0_PORT}/roundpen_test?sslmode=disable"
need_cmd() { command -v "$1" >/dev/null 2>&1; }

fail=0

note_missing() {
	echo "  - $1" >&2
	echo "    $2" >&2
	echo >&2
	fail=1
}

go_new_enough() {
	local v maj min
	v="$(go env GOVERSION 2>/dev/null || true)"
	maj="$(echo "$v" | sed -n 's/^go\([0-9][0-9]*\)\.\([0-9][0-9]*\).*/\1/p')"
	min="$(echo "$v" | sed -n 's/^go\([0-9][0-9]*\)\.\([0-9][0-9]*\).*/\2/p')"
	[[ -n "$maj" && -n "$min" ]] || return 1
	[[ "$maj" -gt 1 || ( "$maj" -eq 1 && "$min" -ge 25 ) ]]
}

node_new_enough() {
	local maj
	maj="$(node -v 2>/dev/null | sed 's/^v//' | cut -d. -f1)"
	[[ -n "$maj" && "$maj" -ge 20 ]]
}

if ! need_cmd go; then
	note_missing "Go is not installed (need 1.25+)" \
		"Install from https://go.dev/dl/ and put it on PATH."
elif ! go_new_enough; then
	note_missing "Go is too old ($(go env GOVERSION 2>/dev/null || echo unknown); need 1.25+)" \
		"Upgrade from https://go.dev/dl/"
fi

if ! need_cmd node || ! need_cmd npm; then
	note_missing "Node.js / npm is not installed (need Node 20+)" \
		"Install Node 20+ from https://nodejs.org/ (or fnm/nvm)."
elif ! node_new_enough; then
	note_missing "Node.js is too old ($(node -v); need 20+)" \
		"Upgrade Node from https://nodejs.org/"
fi

if ! need_cmd pg0; then
	note_missing "pg0 is not installed (embedded Postgres for host-side make dev)" \
		"Download the binary into ~/.local/bin and ensure that directory is on PATH:
      mkdir -p \"\$HOME/.local/bin\"
      curl -fL -o \"\$HOME/.local/bin/pg0\" \\
        https://github.com/vectorize-io/pg0/releases/latest/download/pg0-linux-x86_64-gnu
      chmod +x \"\$HOME/.local/bin/pg0\"
    macOS: use pg0-darwin-arm64 or pg0-darwin-x64 from the same releases page."
fi

if [[ "$fail" -ne 0 ]]; then
	echo "Install the missing tools, then re-run: make dev" >&2
	exit 1
fi

if ! need_cmd docker; then
	echo "note: Docker is not installed. Agent slots require Docker (ROUNDPEN_BACKEND=docker)."
	echo "      Install Docker Engine, or Browser slots only will be available."
fi

if [[ "$CHECK_ONLY" -eq 1 ]]; then
	echo "make dev: tools ok (go $(go env GOVERSION), node $(node -v), pg0 present)"
	exit 0
fi

ensure_env_key() {
	local key="$1" val="$2" cur
	if [[ ! -f .env ]]; then
		return 1
	fi
	if grep -q "^${key}=" .env; then
		cur="$(grep "^${key}=" .env | tail -1 | cut -d= -f2-)"
		if [[ -z "$cur" ]]; then
			grep -v "^${key}=" .env > .env.devtmp
			echo "${key}=${val}" >> .env.devtmp
			mv .env.devtmp .env
		fi
	else
		echo "${key}=${val}" >> .env
	fi
}

# Force-set a key in .env (ensure_env_key only fills missing/empty ones).
set_env_value() {
	local key="$1" val="$2" cur
	if [[ ! -f .env ]]; then
		return 1
	fi
	if grep -q "^${key}=" .env; then
		grep -v "^${key}=" .env > .env.devtmp
		echo "${key}=${val}" >> .env.devtmp
		mv .env.devtmp .env
	else
		echo "${key}=${val}" >> .env
	fi
}

# pg0 can come back on a different port than .env recorded (instance metadata
# wins on restart). The readiness check only trusts DATABASE_URL, so align both
# the session DSNs and .env with the instance port before starting.
align_pg0_port() {
	local meta="$HOME/.pg0/instances/${PG0_NAME}/instance.json"
	[[ -f "$meta" ]] || return 0
	local iport old_port
	iport="$(grep -o '"port": *[0-9]\+' "$meta" | grep -o '[0-9]\+' | head -1)"
	[[ -n "$iport" ]] || return 0
	if [[ "$DATABASE_URL" == *":${iport}/"* ]]; then
		return 0
	fi
	old_port="$(printf '%s' "$DATABASE_URL" | grep -oE ':[0-9]+/' | head -1 | tr -d ':/' || true)"
	if [[ -z "$old_port" || "$old_port" == "$iport" ]]; then
		return 0
	fi
	echo "pg0: instance port is :${iport}; aligning DATABASE_URL (was :${old_port})"
	DATABASE_URL="$(printf '%s' "$DATABASE_URL" | sed "s#:${old_port}/#:${iport}/#")"
	TEST_DSN_DEFAULT="$(printf '%s' "$TEST_DSN_DEFAULT" | sed "s#:${old_port}/#:${iport}/#")"
	PG0_PORT="$iport"
	export DATABASE_URL
	set_env_value DATABASE_URL "$DATABASE_URL"
	set_env_value ROUNDPEN_TEST_DATABASE_URL "$TEST_DSN_DEFAULT"
}

if [[ ! -f .env ]]; then
	echo "Creating .env from .env.example..."
	cp .env.example .env
fi
ensure_env_key DATABASE_URL "$DSN_DEFAULT"
ensure_env_key ROUNDPEN_TEST_DATABASE_URL "$TEST_DSN_DEFAULT"
ensure_env_key ROUNDPEN_HTTP_ADDR ":${API_PORT}"
ensure_env_key ROUNDPEN_BACKEND "docker"
ensure_env_key ROUNDPEN_DEFAULT_IMAGE "roundpen-code-agent:local"
ensure_env_key ROUNDPEN_AGENT_IMAGE "roundpen-code-agent:local"
ensure_env_key ROUNDPEN_DEFAULT_AGENT_TEMPLATE "code-agent"
ensure_env_key ROUNDPEN_DATA_ROOT "./data"
ensure_env_key ROUNDPEN_BOOTSTRAP_ADMIN "true"
ensure_env_key ROUNDPEN_PREVIEW_PUBLIC_URL "http://${LAN_IP}:${API_PORT}"
ensure_env_key ROUNDPEN_TEMPLATE_BUILDER ""

# Agent is Docker-only now. Migrate legacy kern/qemu settings left in .env.
if grep -qE '^ROUNDPEN_BACKEND=(kern|qemu)$' .env; then
	echo "note: Agent backend is Docker-only; setting ROUNDPEN_BACKEND=docker"
	grep -v '^ROUNDPEN_BACKEND=' .env > .env.devtmp
	echo "ROUNDPEN_BACKEND=docker" >> .env.devtmp
	mv .env.devtmp .env
fi
if grep -qE '^ROUNDPEN_DEFAULT_IMAGE=(host|.*\.qcow2)$' .env; then
	grep -v '^ROUNDPEN_DEFAULT_IMAGE=' .env > .env.devtmp
	echo "ROUNDPEN_DEFAULT_IMAGE=roundpen-code-agent:local" >> .env.devtmp
	mv .env.devtmp .env
fi
if grep -qE '^ROUNDPEN_AGENT_IMAGE=(.*\.qcow2|host)$' .env; then
	grep -v '^ROUNDPEN_AGENT_IMAGE=' .env > .env.devtmp
	echo "ROUNDPEN_AGENT_IMAGE=roundpen-code-agent:local" >> .env.devtmp
	mv .env.devtmp .env
fi
if grep -qE '^ROUNDPEN_DEFAULT_AGENT_TEMPLATE=(agent-claude|host)$' .env; then
	grep -v '^ROUNDPEN_DEFAULT_AGENT_TEMPLATE=' .env > .env.devtmp
	echo "ROUNDPEN_DEFAULT_AGENT_TEMPLATE=code-agent" >> .env.devtmp
	mv .env.devtmp .env
fi

# Kaniko was removed; drop its stale keys (harmless but confusing in .env).
if grep -qE '^ROUNDPEN_KANIKO_' .env; then
	echo "note: removing legacy ROUNDPEN_KANIKO_* keys from .env (kaniko removed)"
	grep -vE '^ROUNDPEN_KANIKO_' .env > .env.devtmp
	mv .env.devtmp .env
fi
# Vite proxies UI :19000 → API :19001; bump away from :9527 if left from docs.
if grep -qE '^ROUNDPEN_HTTP_ADDR=:9527$' .env; then
	echo "note: Vite proxies UI :${UI_PORT} → API :${API_PORT}; setting ROUNDPEN_HTTP_ADDR=:${API_PORT}"
	grep -v '^ROUNDPEN_HTTP_ADDR=' .env > .env.devtmp
	echo "ROUNDPEN_HTTP_ADDR=:${API_PORT}" >> .env.devtmp
	mv .env.devtmp .env
fi
if grep -qE '^ROUNDPEN_PREVIEW_PUBLIC_URL=http://127\.0\.0\.1:9527' .env; then
	grep -v '^ROUNDPEN_PREVIEW_PUBLIC_URL=' .env > .env.devtmp
	echo "ROUNDPEN_PREVIEW_PUBLIC_URL=http://127.0.0.1:${API_PORT}" >> .env.devtmp
	mv .env.devtmp .env
fi

set -a
# shellcheck disable=SC1091
. ./.env
set +a

export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
export ROUNDPEN_HTTP_ADDR="${ROUNDPEN_HTTP_ADDR:-:${API_PORT}}"
export DATABASE_URL="${DATABASE_URL:-$DSN_DEFAULT}"
export ROUNDPEN_BACKEND="${ROUNDPEN_BACKEND:-docker}"
export ROUNDPEN_DATA_ROOT="${ROUNDPEN_DATA_ROOT:-./data}"
export ROUNDPEN_HTTP_ADDR="${ROUNDPEN_HTTP_ADDR:-0.0.0.0:${API_PORT}}"
export ROUNDPEN_PREVIEW_PUBLIC_URL="${ROUNDPEN_PREVIEW_PUBLIC_URL:-http://${LAN_IP}:${API_PORT}}"
export DOCKER_CONFIG="${DOCKER_CONFIG:-$ROOT/.docker}"

if [[ "${ROUNDPEN_BACKEND}" == "docker" ]] && need_cmd docker; then
	if ! docker image inspect roundpen-code-agent:local >/dev/null 2>&1; then
		echo "Building roundpen-code-agent:local (git/ssh/curl) ..."
		docker build -t roundpen-code-agent:local images/code-agent
	fi
fi

dsn_host="${DATABASE_URL#*@}"
dsn_host="${dsn_host%%/*}"
dsn_host="${dsn_host%:*}"

pg0_status() {
	pg0 list 2>/dev/null | awk -v n="$PG0_NAME" '
		$1 == n {
			if ($0 ~ /\(running\)/) { print "running"; exit }
			if ($0 ~ /\(stopped\)/) { print "stopped"; exit }
			print "unknown"; exit
		}
	'
}

# True when DATABASE_URL (or pg0 CLI) can SELECT 1.
# pg0 list/psql can be stale (shows stopped / wrong role) while postgres is up.
pg_ready() {
	if command -v psql >/dev/null 2>&1; then
		if psql "$DATABASE_URL" -Atqc 'SELECT 1' >/dev/null 2>&1; then
			return 0
		fi
	fi
	# Fallback: pg0's bundled psql via URI (avoids pg0 CLI's stored role mismatch).
	local bin=""
	bin="$(ls -1 "$HOME"/.pg0/installation/*/bin/psql 2>/dev/null | sort -V | tail -1 || true)"
	if [[ -n "$bin" ]]; then
		if "$bin" "$DATABASE_URL" -Atqc 'SELECT 1' >/dev/null 2>&1; then
			return 0
		fi
	fi
	if pg0 psql --name "$PG0_NAME" -- -Atqc 'SELECT 1' >/dev/null 2>&1; then
		return 0
	fi
	return 1
}

# Run SQL against DATABASE_URL (preferred) or pg0 psql.
pg_exec() {
	local db="${1:-}"
	shift || true
	local url="$DATABASE_URL"
	if [[ -n "$db" && "$db" != "$PG0_DB" ]]; then
		# Swap DB name in URI: .../roundpen?... → .../other?...
		url="$(printf '%s' "$DATABASE_URL" | sed -E "s#/([^/?]+)(\\?|$)#/${db}\\2#")"
	fi
	if command -v psql >/dev/null 2>&1; then
		psql "$url" "$@"
		return $?
	fi
	local bin=""
	bin="$(ls -1 "$HOME"/.pg0/installation/*/bin/psql 2>/dev/null | sort -V | tail -1 || true)"
	if [[ -n "$bin" ]]; then
		"$bin" "$url" "$@"
		return $?
	fi
	if [[ -n "$db" && "$db" != "$PG0_DB" ]]; then
		pg0 psql --name "$PG0_NAME" -- "$db" "$@"
	else
		pg0 psql --name "$PG0_NAME" -- "$@"
	fi
}

wait_pg0() {
	local i
	for i in $(seq 1 40); do
		if pg_ready; then
			return 0
		fi
		sleep 0.25
	done
	return 1
}

start_pg0() {
	if pg_ready; then
		echo "pg0: postgres already accepting connections via DATABASE_URL"
		return 0
	fi
	local st out
	st="$(pg0_status)"
	case "$st" in
	stopped)
		echo "pg0: starting existing instance '${PG0_NAME}'..."
		# Metadata can say stopped while a postmaster still holds the port.
		# "already running" is OK if DATABASE_URL then becomes ready.
		out="$(pg0 start --name "$PG0_NAME" 2>&1)" || true
		if echo "$out" | grep -qi 'already running'; then
			echo "pg0: instance reports already running; checking DATABASE_URL..."
		elif [[ -n "$out" ]]; then
			echo "$out"
		fi
		;;
	running)
		echo "pg0: list says running; waiting for connections..."
		;;
	*)
		echo "pg0: creating instance '${PG0_NAME}' on :${PG0_PORT}..."
		pg0 start --name "$PG0_NAME" --port "$PG0_PORT" \
			--username "$PG0_USER" --password "$PG0_PASS" --database "$PG0_DB" || true
		;;
	esac
	if wait_pg0; then
		return 0
	fi
	echo "error: postgres for '${PG0_NAME}' did not become ready on port ${PG0_PORT}" >&2
	echo "  hint: verify with: psql \"\$DATABASE_URL\" -c 'SELECT 1'" >&2
	echo "  if pg0 metadata is stale: pg0 stop --name ${PG0_NAME}; pg0 start --name ${PG0_NAME}" >&2
	pg0 list 2>&1 | sed 's/^/  /' >&2 || true
	return 1
}

ensure_extensions() {
	echo "pg0: ensuring pgcrypto + vector extensions..."
	pg0 install-extension --name "$PG0_NAME" vector >/dev/null 2>&1 || true
	pg_exec "" -v ON_ERROR_STOP=1 -c 'CREATE EXTENSION IF NOT EXISTS pgcrypto;' >/dev/null
	pg_exec "" -v ON_ERROR_STOP=1 -c 'CREATE EXTENSION IF NOT EXISTS vector;' >/dev/null || true
}

ensure_test_db() {
	local test_db=roundpen_test
	if ! pg_exec "" -Atqc "SELECT 1 FROM pg_database WHERE datname='${test_db}'" | grep -q 1; then
		echo "pg0: creating test database '${test_db}'..."
		pg_exec "" -v ON_ERROR_STOP=1 -c "CREATE DATABASE ${test_db} OWNER ${PG0_USER};"
	fi
	pg_exec "$test_db" -v ON_ERROR_STOP=1 -c 'CREATE EXTENSION IF NOT EXISTS pgcrypto;' >/dev/null
	pg_exec "$test_db" -v ON_ERROR_STOP=1 -c 'CREATE EXTENSION IF NOT EXISTS vector;' >/dev/null || true
}

migrate_legacy_state() {
	# Older make dev seeded kaniko fields; drop them and map a stale kaniko
	# engine to docker (the only local builder).
	echo "pg0: clearing legacy kaniko settings in app_settings..."
	pg_exec "" -v ON_ERROR_STOP=1 -c "
UPDATE app_settings
SET payload = (
      payload
      - 'kanikoDestination' - 'kanikoExecutor' - 'kanikoRegistryMirrors'
      - 'kanikoInsecure' - 'kanikoSkipTlsVerify' - 'kanikoExtraArgs'
    )
  || CASE
       WHEN COALESCE(payload->>'templateBuilder', '') = 'kaniko'
         THEN jsonb_build_object('templateBuilder', 'docker')
       ELSE '{}'::jsonb
     END,
    updated_at = now()
WHERE id = 'global';
" >/dev/null 2>&1 || true
	# user_runtime only held the removed per-user agent engine picker.
	pg_exec "" -v ON_ERROR_STOP=1 -c "DROP TABLE IF EXISTS user_runtime;" >/dev/null 2>&1 || true
}

case "$dsn_host" in
127.0.0.1|localhost|"")
	align_pg0_port
	start_pg0
	ensure_extensions
	ensure_test_db
	migrate_legacy_state
	;;
*)
	echo "note: DATABASE_URL host is '${dsn_host}', not starting local pg0"
	;;
esac

mkdir -p "${ROUNDPEN_DATA_ROOT}"
# Ensure embed compiles even before first production UI build.
mkdir -p internal/ui/dist
touch internal/ui/dist/.gitkeep

if [[ ! -d web/node_modules ]]; then
	echo "Installing web npm dependencies..."
	(cd web && npm install)
fi

API_PID=""
WEB_PID=""
cleanup() {
	trap - EXIT INT TERM
	echo
	echo "stopping preview processes..."
	# Kill process groups so `go run` / npm child binaries die too.
	if [[ -n "${WEB_PID}" ]]; then kill -- "-${WEB_PID}" 2>/dev/null || kill "${WEB_PID}" 2>/dev/null || true; fi
	if [[ -n "${API_PID}" ]]; then kill -- "-${API_PID}" 2>/dev/null || kill "${API_PID}" 2>/dev/null || true; fi
	wait >/dev/null 2>&1 || true
	# Sweep ports in case a child outlived the group (same as make stop).
	"$ROOT/scripts/dev-stop.sh" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

# Clear stale listeners before bind (common after crashed make dev).
"$ROOT/scripts/dev-stop.sh"

# go run writes the binary under /tmp; that tmpfs often hits quota while Vite
# still starts, leaving the UI up with ECONNREFUSED on :19001.
export GOTMPDIR="${GOTMPDIR:-$HOME/.cache/roundpen-gotmp}"
mkdir -p "$GOTMPDIR" bin
echo "Building bin/roundpend ..."
if ! go build -o bin/roundpend ./cmd/roundpend; then
	echo "error: go build ./cmd/roundpend failed" >&2
	exit 1
fi

echo "Starting roundpend on ${ROUNDPEN_HTTP_ADDR} ..."
# New session so Ctrl+C / cleanup can signal the whole tree.
setsid ./bin/roundpend &
API_PID=$!

wait_api() {
	local i
	for i in $(seq 1 80); do
		if ! kill -0 "$API_PID" 2>/dev/null; then
			return 1
		fi
		if curl -sf --max-time 0.4 "http://127.0.0.1:${API_PORT}/health" >/dev/null 2>&1 \
			|| curl -sf --max-time 0.4 "http://127.0.0.1:${API_PORT}/v1/ready" >/dev/null 2>&1; then
			return 0
		fi
		sleep 0.25
	done
	return 1
}
if ! wait_api; then
	echo "error: roundpend did not become ready on :${API_PORT} (Vite not started)." >&2
	echo "  hint: if the build said disk quota, /tmp is full; this script now builds to bin/." >&2
	exit 1
fi

echo "Starting Vite UI on ${DEV_BIND}:${UI_PORT} (proxy → 127.0.0.1:${API_PORT}) ..."
setsid bash -c "cd web && npm run dev -- --host '${DEV_BIND}' --port '${UI_PORT}'" &
WEB_PID=$!

echo
echo "Dev preview (LAN bind ${DEV_BIND}, advertised as ${LAN_IP}):"
echo "  UI  http://${LAN_IP}:${UI_PORT}/"
echo "  API http://${LAN_IP}:${API_PORT}/"
echo "  UI  http://127.0.0.1:${UI_PORT}/  (local)"
echo "Ctrl+C or make stop → API/Vite (pg0 stays running)."
echo

wait
