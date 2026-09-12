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
GITEA_REGISTRY_HOST="${ROUNDPEN_GITEA_REGISTRY_HOST:-git.eaxi.com}"
GITEA_REGISTRY_REPO="${ROUNDPEN_GITEA_REGISTRY_REPO:-sandbox/roundpen}"
GITEA_KANIKO_DEST="${GITEA_REGISTRY_HOST}/${GITEA_REGISTRY_REPO}"

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
	echo "note: Docker is not installed. Sandboxes run under QEMU (ROUNDPEN_BACKEND=qemu)."
	echo "      Docker is only needed to rebuild agent/browser qcow2 images."
fi

if ! need_cmd bwrap; then
	echo "note: bubblewrap (bwrap) is not installed; kaniko template builds need it for an isolated rootfs."
	echo "      Debian/Ubuntu: sudo apt install bubblewrap"
fi

if [[ "$CHECK_ONLY" -eq 0 ]]; then
	# Optional: template image builds only. Failure must not block make dev.
	if ! ./scripts/install-kaniko.sh; then
		echo "note: Kaniko download/install failed; continuing without local template builds."
		echo "      Configure Template builds in Settings (local Kaniko / Docker / remote CI) when needed."
		echo "      Or retry: ./scripts/install-kaniko.sh"
	fi
fi

if ! need_cmd executor; then
	echo "note: Kaniko executor not on PATH — local Kaniko builds unavailable until installed."
	echo "      make dev does not require it; set Template build engine in Settings when ready."
	# Avoid roundpend soft-warn spam when .env still says kaniko from older defaults.
	if [[ "${ROUNDPEN_TEMPLATE_BUILDER:-}" == "kaniko" ]] || grep -qE '^ROUNDPEN_TEMPLATE_BUILDER=kaniko$' .env 2>/dev/null; then
		echo "note: clearing ROUNDPEN_TEMPLATE_BUILDER=kaniko for this session (executor missing)."
		export ROUNDPEN_TEMPLATE_BUILDER=""
		if [[ -f .env ]]; then
			grep -v '^ROUNDPEN_TEMPLATE_BUILDER=' .env > .env.devtmp
			echo "ROUNDPEN_TEMPLATE_BUILDER=" >> .env.devtmp
			mv .env.devtmp .env
		fi
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
ensure_env_key ROUNDPEN_BACKEND "qemu"
ensure_env_key ROUNDPEN_DEFAULT_IMAGE "images/agent-qemu/out/agent.qcow2"
ensure_env_key ROUNDPEN_DEFAULT_AGENT_TEMPLATE "agent-claude"
ensure_env_key ROUNDPEN_DATA_ROOT "./data"
ensure_env_key ROUNDPEN_BOOTSTRAP_ADMIN "true"
ensure_env_key ROUNDPEN_PREVIEW_PUBLIC_URL "http://${LAN_IP}:${API_PORT}"
ensure_env_key ROUNDPEN_TEMPLATE_BUILDER ""
# Destination is still useful when the user later enables Kaniko in Settings.
ensure_env_key ROUNDPEN_KANIKO_DESTINATION "$GITEA_KANIKO_DEST"
ensure_env_key ROUNDPEN_KANIKO_INSECURE "false"
ensure_env_key ROUNDPEN_KANIKO_SKIP_TLS_VERIFY "false"
ensure_env_key ROUNDPEN_KANIKO_REGISTRY_MIRROR "https://docker.1ms.run"
ensure_env_key ROUNDPEN_GITEA_REGISTRY_HOST "$GITEA_REGISTRY_HOST"
ensure_env_key ROUNDPEN_GITEA_REGISTRY_USER "sandbox"

if grep -qE '^ROUNDPEN_KANIKO_DESTINATION=127\.0\.0\.1:5000/roundpen$' .env; then
	echo "note: migrating kaniko destination to Gitea (${GITEA_KANIKO_DEST})"
	grep -v '^ROUNDPEN_KANIKO_DESTINATION=' .env > .env.devtmp
	echo "ROUNDPEN_KANIKO_DESTINATION=${GITEA_KANIKO_DEST}" >> .env.devtmp
	mv .env.devtmp .env
fi
if grep -qE '^ROUNDPEN_KANIKO_INSECURE=true$' .env; then
	grep -v '^ROUNDPEN_KANIKO_INSECURE=' .env > .env.devtmp
	echo "ROUNDPEN_KANIKO_INSECURE=false" >> .env.devtmp
	mv .env.devtmp .env
fi
if grep -qE '^ROUNDPEN_KANIKO_SKIP_TLS_VERIFY=true$' .env; then
	grep -v '^ROUNDPEN_KANIKO_SKIP_TLS_VERIFY=' .env > .env.devtmp
	echo "ROUNDPEN_KANIKO_SKIP_TLS_VERIFY=false" >> .env.devtmp
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
export ROUNDPEN_BACKEND="${ROUNDPEN_BACKEND:-qemu}"
export ROUNDPEN_DATA_ROOT="${ROUNDPEN_DATA_ROOT:-./data}"
export ROUNDPEN_HTTP_ADDR="${ROUNDPEN_HTTP_ADDR:-0.0.0.0:${API_PORT}}"
export ROUNDPEN_PREVIEW_PUBLIC_URL="${ROUNDPEN_PREVIEW_PUBLIC_URL:-http://${LAN_IP}:${API_PORT}}"
export DOCKER_CONFIG="${DOCKER_CONFIG:-$ROOT/.docker}"
./scripts/gitea-registry-auth.sh

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

ensure_kaniko_db_settings() {
	local dest="${ROUNDPEN_KANIKO_DESTINATION:-$GITEA_KANIKO_DEST}"
	local builder="${ROUNDPEN_TEMPLATE_BUILDER:-kaniko}"
	local insecure="${ROUNDPEN_KANIKO_INSECURE:-false}"
	local skip_tls="${ROUNDPEN_KANIKO_SKIP_TLS_VERIFY:-false}"
	echo "pg0: ensuring dev kaniko settings in app_settings..."
	pg_exec "" -v ON_ERROR_STOP=1 -c "
UPDATE app_settings
SET payload = payload
  || jsonb_build_object(
       'templateBuilder', '${builder}',
       'kanikoDestination', '${dest}',
       'kanikoInsecure', ${insecure},
       'kanikoSkipTlsVerify', ${skip_tls}
     ),
    updated_at = now()
WHERE id = 'global'
  AND (
    COALESCE(payload->>'kanikoDestination', '') = ''
    OR payload->>'kanikoDestination' = '127.0.0.1:5000/roundpen'
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
