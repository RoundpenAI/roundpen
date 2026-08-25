#!/usr/bin/env bash
# Ensure pg0 is available and start a local Postgres for Roundpen development.
set -euo pipefail

if ! command -v pg0 >/dev/null 2>&1; then
  echo "pg0 not found. Install: https://github.com/vectorize-io/pg0" >&2
  exit 1
fi

pg0 start
echo "Postgres ready via pg0. Set DATABASE_URL to the instance URI."
