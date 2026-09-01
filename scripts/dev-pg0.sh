#!/usr/bin/env bash
# Deprecated: use `make dev` / scripts/dev-up.sh (starts pg0 + API + Vite).
# Kept as a thin wrapper for older docs/muscle memory.
exec "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/dev-up.sh" "$@"
