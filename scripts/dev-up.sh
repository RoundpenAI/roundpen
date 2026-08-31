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
	echo "note: Docker is not installed. Default ROUNDPEN_BACKEND=kern still works;"
	echo "      set ROUNDPEN_BACKEND=docker when you have a local/remote engine."
fi

if [[ "$CHECK_ONLY" -eq 0 ]] && ! need_cmd executor; then
	echo "kaniko: installing executor for template builds..."
	./scripts/install-kaniko.sh
fi

if ! need_cmd executor; then
	if [[ "$CHECK_ONLY" -eq 1 ]]; then
		echo "note: Kaniko executor not on PATH; run ./scripts/install-kaniko.sh for template builds."
	else
		note_missing "Kaniko executor is not installed (needed for template builds on kern)" \
			"Run: ./scripts/install-kaniko.sh"
	fi
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

if [[ ! -f .env ]]; then
	echo "Creating .env from .env.example..."
	cp .env.example .env
fi
ensure_env_key DATABASE_URL "$DSN_DEFAULT"
ensure_env_key ROUNDPEN_TEST_DATABASE_URL "$TEST_DSN_DEFAULT"
ensure_env_key ROUNDPEN_HTTP_ADDR ":${API_PORT}"
ensure_env_key ROUNDPEN_BACKEND "kern"
ensure_env_key ROUNDPEN_DEFAULT_IMAGE "host"
ensure_env_key ROUNDPEN_DATA_ROOT "./data"
ensure_env_key ROUNDPEN_BOOTSTRAP_ADMIN "true"
ensure_env_key ROUNDPEN_PREVIEW_PUBLIC_URL "http://${LAN_IP}:${API_PORT}"
ensure_env_key ROUNDPEN_TEMPLATE_BUILDER "kaniko"
ensure_env_key ROUNDPEN_KANIKO_DESTINATION "127.0.0.1:5000/roundpen"
ensure_env_key ROUNDPEN_KANIKO_INSECURE "true"
ensure_env_key ROUNDPEN_KANIKO_SKIP_TLS_VERIFY "true"

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
export ROUNDPEN_BACKEND="${ROUNDPEN_BACKEND:-kern}"
export ROUNDPEN_DATA_ROOT="${ROUNDPEN_DATA_ROOT:-./data}"
export ROUNDPEN_HTTP_ADDR="${ROUNDPEN_HTTP_ADDR:-0.0.0.0:${API_PORT}}"
export ROUNDPEN_PREVIEW_PUBLIC_URL="${ROUNDPEN_PREVIEW_PUBLIC_URL:-http://${LAN_IP}:${API_PORT}}"

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

wait_pg0() {
	local i
	for i in $(seq 1 40); do
		if pg0 psql --name "$PG0_NAME" -- -Atqc 'SELECT 1' >/dev/null 2>&1; then
			return 0
		fi
		sleep 0.25
	done
	echo "error: pg0 instance '${PG0_NAME}' did not become ready on port ${PG0_PORT}" >&2
	return 1
}

start_pg0() {
	if pg0 psql --name "$PG0_NAME" -- -Atqc 'SELECT 1' >/dev/null 2>&1; then
		echo "pg0: instance '${PG0_NAME}' already running"
		return 0
	fi
	local st
	st="$(pg0_status)"
	case "$st" in
	stopped)
		echo "pg0: starting existing instance '${PG0_NAME}'..."
		pg0 start --name "$PG0_NAME" || true
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
	wait_pg0
}

ensure_extensions() {
	echo "pg0: ensuring pgcrypto + vector extensions..."
	pg0 install-extension --name "$PG0_NAME" vector >/dev/null 2>&1 || true
	pg0 psql --name "$PG0_NAME" -- -v ON_ERROR_STOP=1 -c 'CREATE EXTENSION IF NOT EXISTS pgcrypto;' >/dev/null
	pg0 psql --name "$PG0_NAME" -- -v ON_ERROR_STOP=1 -c 'CREATE EXTENSION IF NOT EXISTS vector;' >/dev/null || true
}

ensure_test_db() {
	local test_db=roundpen_test
	if ! pg0 psql --name "$PG0_NAME" -- -Atqc "SELECT 1 FROM pg_database WHERE datname='${test_db}'" | grep -q 1; then
		echo "pg0: creating test database '${test_db}'..."
		pg0 psql --name "$PG0_NAME" -- -v ON_ERROR_STOP=1 -c "CREATE DATABASE ${test_db} OWNER ${PG0_USER};"
	fi
	pg0 psql --name "$PG0_NAME" -- "$test_db" -v ON_ERROR_STOP=1 -c 'CREATE EXTENSION IF NOT EXISTS pgcrypto;' >/dev/null
	pg0 psql --name "$PG0_NAME" -- "$test_db" -v ON_ERROR_STOP=1 -c 'CREATE EXTENSION IF NOT EXISTS vector;' >/dev/null || true
}

ensure_kaniko_db_settings() {
	local dest="${ROUNDPEN_KANIKO_DESTINATION:-127.0.0.1:5000/roundpen}"
	local builder="${ROUNDPEN_TEMPLATE_BUILDER:-kaniko}"
	echo "pg0: ensuring dev kaniko settings in app_settings..."
	pg0 psql --name "$PG0_NAME" -- -v ON_ERROR_STOP=1 -c "
UPDATE app_settings
SET payload = payload
  || jsonb_build_object(
       'templateBuilder', '${builder}',
       'kanikoDestination', '${dest}',
       'kanikoInsecure', true,
       'kanikoSkipTlsVerify', true
     ),
    updated_at = now()
WHERE id = 'global'
  AND (
    COALESCE(payload->>'kanikoDestination', '') = ''
    OR COALESCE(payload->>'templateBuilder', '') IN ('', 'auto', 'docker')
  );
" >/dev/null 2>&1 || true
}

case "$dsn_host" in
127.0.0.1|localhost|"")
	start_pg0
	ensure_extensions
	ensure_test_db
	ensure_kaniko_db_settings
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
	if [[ -n "${WEB_PID}" ]]; then kill "${WEB_PID}" 2>/dev/null || true; fi
	if [[ -n "${API_PID}" ]]; then kill "${API_PID}" 2>/dev/null || true; fi
	wait >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

echo "Starting roundpend on ${ROUNDPEN_HTTP_ADDR} ..."
go run ./cmd/roundpend &
API_PID=$!

echo "Starting Vite UI on ${DEV_BIND}:${UI_PORT} (proxy → 127.0.0.1:${API_PORT}) ..."
(cd web && npm run dev -- --host "${DEV_BIND}" --port "${UI_PORT}") &
WEB_PID=$!

echo
echo "Dev preview (LAN bind ${DEV_BIND}, advertised as ${LAN_IP}):"
echo "  UI  http://${LAN_IP}:${UI_PORT}/"
echo "  API http://${LAN_IP}:${API_PORT}/"
echo "  UI  http://127.0.0.1:${UI_PORT}/  (local)"
echo "Ctrl+C stops API and Vite (pg0 stays running)."
echo

wait
