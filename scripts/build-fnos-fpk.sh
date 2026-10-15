#!/usr/bin/env bash
# Builds the fnOS package in deploy/fnos into dist/fnos/*.fpk.
#
#   make fpk                     # x86: host roundpend + bundled pg0
#   PLATFORM=arm make fpk        # arm64
#
# The package runs roundpend on the NAS host (not in Docker). Postgres comes
# from the bundled pg0 binary. Agent/Browser still use the host Docker engine
# when those slots are used later.
#
# fnpack is the official packaging tool (see deploy/fnos/README.md for the
# download URL); put it on PATH or point FNPACK at it.
set -euo pipefail

VERSION="${VERSION:-0.1.0}"
PLATFORM="${PLATFORM:-x86}"
FNPACK="${FNPACK:-fnpack}"
# Pin pg0 so package contents stay reproducible across builds.
PG0_VERSION="${PG0_VERSION:-v0.15.1}"

case "${VERSION}" in
*[!A-Za-z0-9._-]* | "")
    echo "VERSION must be a plain version string, got '${VERSION}'" >&2
    exit 1
    ;;
esac
case "${PLATFORM}" in
x86)
    GOARCH="amd64"
    PG0_ASSET="pg0-linux-x86_64-gnu"
    ;;
arm)
    GOARCH="arm64"
    PG0_ASSET="pg0-linux-aarch64-gnu"
    ;;
*)
    echo "PLATFORM must be x86 or arm, got '${PLATFORM}'" >&2
    exit 1
    ;;
esac

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${ROOT}/dist/fnos"
NAME="roundpen-${VERSION}-fnos-${PLATFORM}"

command -v "${FNPACK}" >/dev/null 2>&1 || {
    echo "fnpack not found — download the official tool (deploy/fnos/README.md) or set FNPACK=" >&2
    exit 1
}
command -v go >/dev/null 2>&1 || {
    echo "go not found — needed to cross-compile roundpend" >&2
    exit 1
}
command -v npm >/dev/null 2>&1 || {
    echo "npm not found — needed to build the embedded Web UI" >&2
    exit 1
}

# Render into a staging copy: the package tree in the repo keeps its
# placeholders, so a build never rewrites tracked files.
stage="$(mktemp -d)"
trap 'rm -rf "${stage}"' EXIT
cp -a "${ROOT}/deploy/fnos/." "${stage}/"
rm -rf "${stage}/dist"
# Host binary package: no compose project.
rm -rf "${stage}/app/docker"

render() { # render <file> <sed-expr>
    sed -E "$2" "$1" >"$1.tmp"
    mv "$1.tmp" "$1"
}
render "${stage}/manifest" "s|^version[[:space:]]*=.*|version               = ${VERSION}|"
render "${stage}/manifest" "s|^platform[[:space:]]*=.*|platform              = ${PLATFORM}|"

mkdir -p "${stage}/app/bin"

# Embedded UI (end users never need Node on the NAS).
echo "building Web UI..."
(
    cd "${ROOT}/web"
    if [[ ! -d node_modules ]]; then
        npm_registry="${NPM_REGISTRY:-$(npm config get registry 2>/dev/null || true)}"
        npm_registry="${npm_registry:-https://registry.npmjs.org}"
        npm ci --registry="${npm_registry}"
    fi
    # The Web console is reached through the unified gateway at /app/roundpen,
    # so the SPA must be built for that base path.
    export VITE_BASE=/app/roundpen/
    npm run build
)
mkdir -p "${ROOT}/internal/ui/dist"
touch "${ROOT}/internal/ui/dist/.gitkeep"

echo "building roundpend (linux/${GOARCH})..."
goproxy="${GOPROXY:-$(go env GOPROXY 2>/dev/null || true)}"
goproxy="${goproxy:-https://proxy.golang.org,direct}"
# The fnos build tag compiles in the unified-gateway Socket listener +
# gateway identity middleware; other channels build without it.
GOPROXY="${goproxy}" CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH}" \
    go build -tags fnos -o "${stage}/app/bin/roundpend" "${ROOT}/cmd/roundpend"

# Keyed by version + asset so a pin bump downloads once, then later `make fpk`
# copies the cached binary. PG0_CACHE overrides the directory.
pg0_cache="${PG0_CACHE:-${XDG_CACHE_HOME:-${HOME}/.cache}/roundpen/pg0}"
pg0_cached="${pg0_cache}/${PG0_VERSION}/${PG0_ASSET}"
pg0_url="https://github.com/vectorize-io/pg0/releases/download/${PG0_VERSION}/${PG0_ASSET}"
if [[ -s "${pg0_cached}" && -x "${pg0_cached}" ]]; then
    echo "using cached pg0 ${PG0_VERSION} (${pg0_cached})"
else
    echo "fetching pg0 ${PG0_VERSION} (${PG0_ASSET})..."
    mkdir -p "$(dirname "${pg0_cached}")"
    pg0_tmp="$(mktemp "$(dirname "${pg0_cached}")/.${PG0_ASSET}.XXXXXX")"
    if ! curl -fL --retry 3 --retry-delay 2 -o "${pg0_tmp}" "${pg0_url}"; then
        rm -f "${pg0_tmp}"
        exit 1
    fi
    chmod +x "${pg0_tmp}"
    mv "${pg0_tmp}" "${pg0_cached}"
fi
cp -a "${pg0_cached}" "${stage}/app/bin/pg0"
chmod +x "${stage}/app/bin/pg0" "${stage}/app/bin/roundpend"

cp "${ROOT}/scripts/ensure-pg0.sh" "${stage}/app/bin/ensure-pg0.sh"
chmod +x "${stage}/app/bin/ensure-pg0.sh"

# fnpack writes <appname>.fpk into its working directory.
mkdir -p "${OUT}"
rm -f "${OUT}/roundpen.fpk"
(cd "${OUT}" && "${FNPACK}" build -d "${stage}")
mv "${OUT}/roundpen.fpk" "${OUT}/${NAME}.fpk"
(cd "${OUT}" && sha256sum "${NAME}.fpk" >"${NAME}.fpk.sha256")

echo
echo "built ${OUT}/${NAME}.fpk"
echo "install on the NAS: appcenter-cli install-fpk ${NAME}.fpk"
