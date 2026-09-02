#!/usr/bin/env bash
# Write Kaniko/docker registry credentials for Gitea (reads token from .env).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REGISTRY_HOST="${ROUNDPEN_GITEA_REGISTRY_HOST:-git.eaxi.com}"
REGISTRY_USER="${ROUNDPEN_GITEA_REGISTRY_USER:-sandbox}"
REGISTRY_TOKEN="${ROUNDPEN_GITEA_REGISTRY_TOKEN:-}"
DOCKER_DIR="${DOCKER_CONFIG:-$ROOT/.docker}"
CONFIG="${DOCKER_DIR}/config.json"

if [[ -z "$REGISTRY_TOKEN" ]]; then
	echo "note: set ROUNDPEN_GITEA_REGISTRY_TOKEN in .env for Gitea registry push" >&2
	exit 0
fi

mkdir -p "$DOCKER_DIR"
REGISTRY_HOST="$REGISTRY_HOST" REGISTRY_USER="$REGISTRY_USER" REGISTRY_TOKEN="$REGISTRY_TOKEN" CONFIG="$CONFIG" python3 <<'PY'
import base64, json, os, pathlib

host = os.environ["REGISTRY_HOST"]
user = os.environ["REGISTRY_USER"]
token = os.environ["REGISTRY_TOKEN"]
path = pathlib.Path(os.environ["CONFIG"])
cfg = {}
if path.is_file():
    with path.open() as f:
        cfg = json.load(f)
cfg.setdefault("auths", {})[host] = {
    "auth": base64.b64encode(f"{user}:{token}".encode()).decode()
}
path.parent.mkdir(parents=True, exist_ok=True)
with path.open("w") as f:
    json.dump(cfg, f, indent=2)
    f.write("\n")
path.chmod(0o600)
PY

echo "gitea: registry auth configured for ${REGISTRY_HOST} (${CONFIG})"
