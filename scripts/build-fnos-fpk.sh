#!/usr/bin/env bash
# Builds the fnOS package in deploy/fnos into dist/fnos/*.fpk.
#
#   make fpk                          # x86 package; the image comes from ghcr
#   PLATFORM=arm make fpk             # arm64
#   WITH_IMAGE=1 make fpk             # offline package: build and bundle the image
#   WITH_IMAGE=1 IMAGE_SOURCE=pull make fpk   # bundle the already-pushed image
#
# fnpack is the official packaging tool (see deploy/fnos/README.md for the
# download URL); put it on PATH or point FNPACK at it.
set -euo pipefail

VERSION="${VERSION:-0.1.0}"
PLATFORM="${PLATFORM:-x86}"
WITH_IMAGE="${WITH_IMAGE:-0}"
# build: build the image here (needs Docker); pull: take the published image
# as-is. CI pulls what its image job just pushed instead of rebuilding it.
IMAGE_SOURCE="${IMAGE_SOURCE:-build}"
FNPACK="${FNPACK:-fnpack}"
# Building over the default bridge can crawl when the registry resolves to
# IPv6: the bridge has no IPv6 route, so every request waits for a timeout
# before falling back to IPv4. Host networking avoids that (BUILD_NETWORK=default
# restores the isolated bridge).
BUILD_NETWORK="${BUILD_NETWORK:-host}"

case "${VERSION}" in
*[!A-Za-z0-9._-]* | "")
    echo "VERSION must be a plain version string, got '${VERSION}'" >&2
    exit 1
    ;;
esac
case "${PLATFORM}" in
x86) DOCKER_PLATFORM="linux/amd64" ;;
arm) DOCKER_PLATFORM="linux/arm64" ;;
*)
    echo "PLATFORM must be x86 or arm, got '${PLATFORM}'" >&2
    exit 1
    ;;
esac

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE="${ROUNDPEN_IMAGE:-ghcr.io/roundpenai/roundpend:${VERSION}}"
OUT="${ROOT}/dist/fnos"
NAME="roundpen-${VERSION}-fnos-${PLATFORM}"
if [ "${WITH_IMAGE}" = "1" ]; then
    NAME="${NAME}-offline"
fi

command -v "${FNPACK}" >/dev/null 2>&1 || {
    echo "fnpack not found — download the official tool (deploy/fnos/README.md) or set FNPACK=" >&2
    exit 1
}

# Render into a staging copy: the package tree in the repo keeps its
# placeholders, so a build never rewrites tracked files.
stage="$(mktemp -d)"
trap 'rm -rf "${stage}"' EXIT
cp -a "${ROOT}/deploy/fnos/." "${stage}/"
rm -rf "${stage}/dist"

render() { # render <file> <sed-expr>
    sed -E "$2" "$1" >"$1.tmp"
    mv "$1.tmp" "$1"
}
render "${stage}/manifest" "s|^version[[:space:]]*=.*|version               = ${VERSION}|"
render "${stage}/manifest" "s|^platform[[:space:]]*=.*|platform              = ${PLATFORM}|"
render "${stage}/app/docker/docker-compose.yaml" \
    "s|ghcr.io/roundpenai/roundpend:[A-Za-z0-9._-]+|ghcr.io/roundpenai/roundpend:${VERSION}|"

if [ "${WITH_IMAGE}" = "1" ]; then
    if [ "${IMAGE_SOURCE}" = "pull" ]; then
        # CI: the image job already pushed the multi-arch image, so pulling
        # this platform's copy beats rebuilding it (and needs no qemu).
        docker pull --platform "${DOCKER_PLATFORM}" "${IMAGE}"
    else
        # The build containers do not inherit the host's npm / Go configuration, so
        # forward it (mirrors matter on a slow link; upstream defaults otherwise).
        npm_registry="${NPM_REGISTRY:-$(npm config get registry 2>/dev/null || true)}"
        npm_registry="${npm_registry:-https://registry.npmjs.org}"
        goproxy="${GOPROXY:-$(go env GOPROXY 2>/dev/null || true)}"
        goproxy="${goproxy:-https://proxy.golang.org,direct}"
        build_args=(
            --build-arg "NPM_REGISTRY=${npm_registry}"
            --build-arg "PLAYWRIGHT_NPM_REGISTRY=${npm_registry}"
            --build-arg "GOPROXY=${goproxy}"
        )
        echo "image build: ${IMAGE} (${DOCKER_PLATFORM}), npm=${npm_registry}, goproxy=${goproxy}"

        host_arch="$(docker version --format '{{.Server.Arch}}' 2>/dev/null || true)"
        if [ "${DOCKER_PLATFORM}" = "linux/${host_arch}" ]; then
            docker build --network="${BUILD_NETWORK}" "${build_args[@]}" \
                -t "${IMAGE}" -f "${ROOT}/deploy/Dockerfile" "${ROOT}"
        else
            # Cross-architecture build needs qemu/binfmt registered on the host.
            docker buildx build --platform "${DOCKER_PLATFORM}" -t "${IMAGE}" --load \
                "${build_args[@]}" -f "${ROOT}/deploy/Dockerfile" "${ROOT}"
        fi
    fi
    mkdir -p "${stage}/app/images"
    docker save -o "${stage}/app/images/roundpend.tar" "${IMAGE}"
else
    echo "image not bundled: the NAS pulls ${IMAGE} at install time"
fi

# fnpack writes <appname>.fpk into its working directory.
mkdir -p "${OUT}"
rm -f "${OUT}/roundpen.fpk"
(cd "${OUT}" && "${FNPACK}" build -d "${stage}")
mv "${OUT}/roundpen.fpk" "${OUT}/${NAME}.fpk"
(cd "${OUT}" && sha256sum "${NAME}.fpk" >"${NAME}.fpk.sha256")

echo
echo "built ${OUT}/${NAME}.fpk"
echo "install on the NAS: appcenter-cli install-fpk ${NAME}.fpk"
