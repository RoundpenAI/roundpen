#!/usr/bin/env bash
# Ensure a local pg0 Postgres is running when DATABASE_URL points at localhost.
# Does not download pg0 — the binary must already be on PATH, at
# $ROUNDPEN_PG0_BIN, or (when packaged) next to this script as ./pg0.
#
# Usage:
#   source scripts/ensure-pg0.sh          # exports DATABASE_URL into the caller
#   ./scripts/ensure-pg0.sh               # print DATABASE_URL and exit 0
#
# Optional env:
#   DATABASE_URL                  existing DSN (remote host → skip pg0)
#   ROUNDPEN_PG0_BIN              absolute path to pg0
#   PG0_HOME                      pg0 state root (default: $HOME/.pg0; HOME may
#                                 be unset in package installers — set explicitly)
#   PG0_DATA_DIR                  instance data dir (passed as pg0 --data-dir)
#   PG0_NAME / PG0_PORT / PG0_USER / PG0_PASS / PG0_DB
#   ROUNDPEN_ENSURE_PG0_TEST_DB=1         also create roundpen_test (dev)
#   ROUNDPEN_ENSURE_PG0_MIGRATE_LEGACY=1  clear legacy kaniko settings (dev)
#   ROUNDPEN_ENSURE_PG0_ENV_FILE=/path    write/update DATABASE_URL in this file
set -uo pipefail

_ENSURE_PG0_SOURCED=0
if [[ "${BASH_SOURCE[0]}" != "${0}" ]]; then
	_ENSURE_PG0_SOURCED=1
fi

# Package installers often run with HOME unset; never expand bare $HOME under set -u.
if [[ -z "${HOME:-}" ]]; then
	if [[ -n "${PG0_HOME:-}" ]]; then
		HOME="$(dirname "${PG0_HOME}")"
	else
		HOME="/tmp"
	fi
	export HOME
fi

PG0_NAME="${PG0_NAME:-roundpen}"
PG0_PORT="${PG0_PORT:-5432}"
PG0_USER="${PG0_USER:-roundpen}"
PG0_PASS="${PG0_PASS:-roundpen}"
PG0_DB="${PG0_DB:-roundpen}"
PG0_HOME="${PG0_HOME:-${HOME}/.pg0}"
export PG0_HOME
PG0_DATA_DIR="${PG0_DATA_DIR:-${PG0_HOME}/instances/${PG0_NAME}/data}"
DSN_DEFAULT="postgres://${PG0_USER}:${PG0_PASS}@127.0.0.1:${PG0_PORT}/${PG0_DB}?sslmode=disable"
DATABASE_URL="${DATABASE_URL:-$DSN_DEFAULT}"

_pg0_bin() {
	if [[ -n "${ROUNDPEN_PG0_BIN:-}" && -x "${ROUNDPEN_PG0_BIN}" ]]; then
		printf '%s' "${ROUNDPEN_PG0_BIN}"
		return 0
	fi
	if command -v pg0 >/dev/null 2>&1; then
		command -v pg0
		return 0
	fi
	local here
	here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
	if [[ -x "${here}/pg0" ]]; then
		printf '%s' "${here}/pg0"
		return 0
	fi
	return 1
}

_pg0() {
	# pg0 roots installation/ metadata under $HOME/.pg0; keep HOME pointing at
	# a writable parent of PG0_HOME when we relocated state for packaging.
	HOME="$(dirname "${PG0_HOME}")" "$_PG0" "$@"
}

_pg0_home() {
	printf '%s' "${PG0_HOME}"
}

_dsn_host() {
	local h
	h="${DATABASE_URL#*@}"
	h="${h%%/*}"
	h="${h%:*}"
	printf '%s' "$h"
}

_pg0_status() {
	_pg0 list 2>/dev/null | awk -v n="$PG0_NAME" '
		$1 == n {
			if ($0 ~ /\(running\)/) { print "running"; exit }
			if ($0 ~ /\(stopped\)/) { print "stopped"; exit }
			print "unknown"; exit
		}
	'
}

_pg_ready() {
	if command -v psql >/dev/null 2>&1; then
		if psql "$DATABASE_URL" -Atqc 'SELECT 1' >/dev/null 2>&1; then
			return 0
		fi
	fi
	local bin=""
	bin="$(ls -1 "$(_pg0_home)"/installation/*/bin/psql 2>/dev/null | sort -V | tail -1 || true)"
	if [[ -n "$bin" ]]; then
		if "$bin" "$DATABASE_URL" -Atqc 'SELECT 1' >/dev/null 2>&1; then
			return 0
		fi
	fi
	if _pg0 psql --name "$PG0_NAME" -- -Atqc 'SELECT 1' >/dev/null 2>&1; then
		return 0
	fi
	return 1
}

_pg_exec() {
	local db="${1:-}"
	shift || true
	local url="$DATABASE_URL"
	if [[ -n "$db" && "$db" != "$PG0_DB" ]]; then
		url="$(printf '%s' "$DATABASE_URL" | sed -E "s#/([^/?]+)(\\?|$)#/${db}\\2#")"
	fi
	if command -v psql >/dev/null 2>&1; then
		psql "$url" "$@"
		return $?
	fi
	local bin=""
	bin="$(ls -1 "$(_pg0_home)"/installation/*/bin/psql 2>/dev/null | sort -V | tail -1 || true)"
	if [[ -n "$bin" ]]; then
		"$bin" "$url" "$@"
		return $?
	fi
	if [[ -n "$db" && "$db" != "$PG0_DB" ]]; then
		_pg0 psql --name "$PG0_NAME" -- "$db" "$@"
	else
		_pg0 psql --name "$PG0_NAME" -- "$@"
	fi
}

_wait_pg0() {
	local i=0
	while [[ "$i" -lt 80 ]]; do
		if _pg_ready; then
			return 0
		fi
		sleep 0.25
		i=$((i + 1))
	done
	return 1
}

_align_pg0_port() {
	local meta="$(_pg0_home)/instances/${PG0_NAME}/instance.json"
	[[ -f "$meta" ]] || return 0
	local iport old_port
	iport="$(grep -o '"port": *[0-9]\+' "$meta" | grep -o '[0-9]\+' | head -1)"
	[[ -n "$iport" ]] || return 0
	if [[ "$DATABASE_URL" == *":${iport}/"* ]]; then
		PG0_PORT="$iport"
		return 0
	fi
	old_port="$(printf '%s' "$DATABASE_URL" | grep -oE ':[0-9]+/' | head -1 | tr -d ':/' || true)"
	if [[ -z "$old_port" || "$old_port" == "$iport" ]]; then
		PG0_PORT="$iport"
		return 0
	fi
	echo "pg0: instance port is :${iport}; aligning DATABASE_URL (was :${old_port})"
	DATABASE_URL="$(printf '%s' "$DATABASE_URL" | sed "s#:${old_port}/#:${iport}/#")"
	PG0_PORT="$iport"
}

_start_pg0() {
	if _pg_ready; then
		echo "pg0: postgres already accepting connections via DATABASE_URL"
		return 0
	fi
	mkdir -p "${PG0_HOME}" "${PG0_DATA_DIR}" || {
		echo "error: cannot create pg0 dirs under ${PG0_HOME}" >&2
		return 1
	}
	local st out
	st="$(_pg0_status)"
	case "$st" in
	stopped)
		echo "pg0: starting existing instance '${PG0_NAME}'..."
		out="$(_pg0 start --name "$PG0_NAME" 2>&1)" || true
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
		echo "pg0: creating instance '${PG0_NAME}' on :${PG0_PORT} (data=${PG0_DATA_DIR})..."
		out="$(_pg0 start --name "$PG0_NAME" --port "$PG0_PORT" \
			--data-dir "$PG0_DATA_DIR" \
			--username "$PG0_USER" --password "$PG0_PASS" --database "$PG0_DB" 2>&1)" || true
		if [[ -n "$out" ]]; then
			echo "$out"
		fi
		;;
	esac
	if _wait_pg0; then
		return 0
	fi
	echo "error: postgres for '${PG0_NAME}' did not become ready on port ${PG0_PORT}" >&2
	echo "  HOME=${HOME} PG0_HOME=${PG0_HOME} PG0_DATA_DIR=${PG0_DATA_DIR}" >&2
	echo "  DATABASE_URL=${DATABASE_URL}" >&2
	echo "  hint: verify with: psql \"\$DATABASE_URL\" -c 'SELECT 1'" >&2
	echo "  if pg0 metadata is stale: pg0 stop --name ${PG0_NAME}; pg0 start --name ${PG0_NAME}" >&2
	_pg0 list 2>&1 | sed 's/^/  /' >&2 || true
	return 1
}

_ensure_extensions() {
	echo "pg0: ensuring pgcrypto + vector extensions..."
	_pg0 install-extension --name "$PG0_NAME" vector >/dev/null 2>&1 || true
	_pg_exec "" -v ON_ERROR_STOP=1 -c 'CREATE EXTENSION IF NOT EXISTS pgcrypto;' >/dev/null
	_pg_exec "" -v ON_ERROR_STOP=1 -c 'CREATE EXTENSION IF NOT EXISTS vector;' >/dev/null || true
}

_ensure_test_db() {
	local test_db=roundpen_test
	if ! _pg_exec "" -Atqc "SELECT 1 FROM pg_database WHERE datname='${test_db}'" | grep -q 1; then
		echo "pg0: creating test database '${test_db}'..."
		_pg_exec "" -v ON_ERROR_STOP=1 -c "CREATE DATABASE ${test_db} OWNER ${PG0_USER};"
	fi
	_pg_exec "$test_db" -v ON_ERROR_STOP=1 -c 'CREATE EXTENSION IF NOT EXISTS pgcrypto;' >/dev/null
	_pg_exec "$test_db" -v ON_ERROR_STOP=1 -c 'CREATE EXTENSION IF NOT EXISTS vector;' >/dev/null || true
}

_migrate_legacy_state() {
	echo "pg0: clearing legacy kaniko settings in app_settings..."
	_pg_exec "" -v ON_ERROR_STOP=1 -c "
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
	_pg_exec "" -v ON_ERROR_STOP=1 -c "DROP TABLE IF EXISTS user_runtime;" >/dev/null 2>&1 || true
}

_write_env_file() {
	local file="$1" key=DATABASE_URL
	[[ -n "$file" ]] || return 0
	mkdir -p "$(dirname "$file")"
	if [[ -f "$file" ]] && grep -q "^${key}=" "$file"; then
		grep -v "^${key}=" "$file" >"${file}.tmp"
		echo "${key}=${DATABASE_URL}" >>"${file}.tmp"
		mv "${file}.tmp" "$file"
	else
		echo "${key}=${DATABASE_URL}" >>"$file"
	fi
}

_ensure_pg0_main() {
	local host
	host="$(_dsn_host)"
	case "$host" in
	127.0.0.1|localhost|"")
		;;
	*)
		echo "note: DATABASE_URL host is '${host}', not starting local pg0"
		export DATABASE_URL
		return 0
		;;
	esac

	if ! _PG0="$(_pg0_bin)"; then
		echo "error: pg0 binary not found" >&2
		echo "  Install pg0 onto PATH, set ROUNDPEN_PG0_BIN, or ship it next to this script." >&2
		echo "  Or set DATABASE_URL to an existing Postgres (with pgvector)." >&2
		echo "  Releases: https://github.com/vectorize-io/pg0/releases" >&2
		return 1
	fi

	_align_pg0_port
	_start_pg0 || return 1
	_ensure_extensions || return 1
	if [[ "${ROUNDPEN_ENSURE_PG0_TEST_DB:-0}" = "1" ]]; then
		_ensure_test_db || return 1
	fi
	if [[ "${ROUNDPEN_ENSURE_PG0_MIGRATE_LEGACY:-0}" = "1" ]]; then
		_migrate_legacy_state || true
	fi
	if [[ -n "${ROUNDPEN_ENSURE_PG0_ENV_FILE:-}" ]]; then
		_write_env_file "${ROUNDPEN_ENSURE_PG0_ENV_FILE}" || return 1
	fi
	export DATABASE_URL
	export PG0_PORT
	return 0
}

if ! _ensure_pg0_main; then
	if [[ "${_ENSURE_PG0_SOURCED}" -eq 1 ]]; then
		return 1
	fi
	exit 1
fi

# When executed (not sourced), print the DSN for callers that capture stdout.
if [[ "${_ENSURE_PG0_SOURCED}" -eq 0 ]]; then
	printf '%s\n' "$DATABASE_URL"
fi
