#!/usr/bin/env bash
# Live end-to-end coding-agent smoke test against a running roundpend.
# Usage:
#   ./scripts/e2e-coding-agent.sh
#   BASE=http://127.0.0.1:9527 API_KEY=rp-dev-secret ./scripts/e2e-coding-agent.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

BASE="${BASE:-http://127.0.0.1:${ROUNDPEN_HTTP_ADDR#:}}"
BASE="${BASE/http:\/\//http://}"
if [[ "$BASE" != http* ]]; then
  BASE="http://127.0.0.1:${ROUNDPEN_HTTP_ADDR#:}"
fi
API_KEY="${API_KEY:-${ROUNDPEN_API_KEY:-}}"
if [[ -z "$API_KEY" ]] && command -v pg0 >/dev/null 2>&1; then
  API_KEY="$(pg0 psql --name roundpen -- -Atqc "SELECT api_key FROM users WHERE username='admin' LIMIT 1;" 2>/dev/null || true)"
fi
AUTH=()
if [[ -n "$API_KEY" ]]; then
  AUTH=(-H "X-API-Key: $API_KEY")
else
  echo "error: set API_KEY or ROUNDPEN_API_KEY (or run pg0 with admin user seeded)" >&2
  exit 1
fi

json() { jq -r "$@" 2>/dev/null || python3 -c "import sys,json; d=json.load(sys.stdin); print($1)"; }

echo "==> E2E coding agent @ $BASE"

echo "-- health"
curl -sf "$BASE/health" >/dev/null

echo "-- create sandbox"
CREATE=$(curl -sf "${AUTH[@]}" -H 'Content-Type: application/json' \
  -d '{"templateID":"host","timeout":600,"envVars":{"AGENT":"live-e2e"}}' \
  "$BASE/sandboxes")
SID=$(echo "$CREATE" | json '.sandboxID')
test -n "$SID"
echo "    sandboxID=$SID"

echo "-- connect"
curl -sf "${AUTH[@]}" -H 'Content-Type: application/json' -d '{}' \
  -X POST "$BASE/sandboxes/$SID/connect" >/dev/null

echo "-- exec"
EXEC=$(curl -sf "${AUTH[@]}" -H 'Content-Type: application/json' \
  -d '{"command":["/bin/sh","-c","echo hello-live > note.txt && cat note.txt"],"timeout":30}' \
  "$BASE/v1/sandboxes/$SID/exec")
echo "$EXEC" | json '.stdout' | grep -q hello-live

echo "-- write file"
curl -sf "${AUTH[@]}" -X POST --data-binary 'live readme' \
  "$BASE/v1/sandboxes/$SID/files?path=README.md" >/dev/null

echo "-- list files"
LIST=$(curl -sf "${AUTH[@]}" "$BASE/v1/sandboxes/$SID/files?path=.")
echo "$LIST" | grep -q note.txt
echo "$LIST" | grep -q README.md

echo "-- stat"
curl -sf "${AUTH[@]}" "$BASE/v1/sandboxes/$SID/files/stat?path=note.txt" >/dev/null

echo "-- refreshes"
curl -sf "${AUTH[@]}" -X POST "$BASE/sandboxes/$SID/refreshes" -o /dev/null -w '%{http_code}' | grep -q 204

echo "-- stop + exec blocked"
curl -sf "${AUTH[@]}" -X POST "$BASE/v1/sandboxes/$SID/stop" -o /dev/null
CODE=$(curl -s "${AUTH[@]}" -H 'Content-Type: application/json' \
  -d '{"command":["true"]}' -X POST "$BASE/v1/sandboxes/$SID/exec" -o /dev/null -w '%{http_code}')
test "$CODE" = "409"

echo "-- connect resume"
CODE=$(curl -s "${AUTH[@]}" -H 'Content-Type: application/json' -d '{}' \
  -X POST "$BASE/sandboxes/$SID/connect" -o /dev/null -w '%{http_code}')
test "$CODE" = "201"

echo "-- delete"
curl -sf "${AUTH[@]}" -X DELETE "$BASE/sandboxes/$SID" -o /dev/null -w '%{http_code}' | grep -q 204

echo "==> E2E PASS"
