#!/usr/bin/env bash
# Install Kaniko executor to a dedicated directory for Roundpen template builds.
# The executor must live alone in its install dir: modern Kaniko treats that
# directory as KanikoDir (copies Dockerfile there, etc.). Do not place it in
# ~/.local/bin next to other tools.
set -euo pipefail

KANIKO_VERSION="${ROUNDPEN_KANIKO_VERSION:-v1.27.2}"
INSTALL_DIR="${ROUNDPEN_KANIKO_INSTALL_DIR:-$HOME/.local/share/roundpen/kaniko}"
LINK_DIR="${ROUNDPEN_KANIKO_LINK_DIR:-$HOME/.local/bin}"
EXECUTOR="$INSTALL_DIR/executor"
TAG="osscontainertools%2Fkaniko%2F${KANIKO_VERSION}"
ARCH="$(uname -m)"
case "$ARCH" in
x86_64) GOARCH=amd64 ;;
aarch64|arm64) GOARCH=arm64 ;;
*)
	echo "error: unsupported arch ${ARCH} for kaniko install" >&2
	exit 1
	;;
esac

ASSET="osscontainertools-kaniko.executor.${KANIKO_VERSION#v}.${GOARCH}.tar"
URL="https://github.com/kaniko-build/builder/releases/download/${TAG}/${ASSET}"

mkdir -p "$INSTALL_DIR" "$LINK_DIR"

# Migrate a previous install that dropped the binary into ~/.local/bin.
if [[ -x "$LINK_DIR/executor" && ! -x "$EXECUTOR" ]]; then
	echo "kaniko: migrating executor from ${LINK_DIR}/executor -> ${EXECUTOR}"
	mv "$LINK_DIR/executor" "$EXECUTOR"
fi
# Kaniko may have left a Dockerfile next to a host-installed binary.
if [[ -f "$LINK_DIR/Dockerfile" ]]; then
	echo "kaniko: removing leftover ${LINK_DIR}/Dockerfile"
	rm -f "$LINK_DIR/Dockerfile"
fi

if [[ -x "$EXECUTOR" ]]; then
	ln -sfn "$EXECUTOR" "$LINK_DIR/executor"
	echo "kaniko: executor already installed at ${EXECUTOR}"
	echo "kaniko: PATH link ${LINK_DIR}/executor -> ${EXECUTOR}"
	exit 0
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

echo "kaniko: downloading ${KANIKO_VERSION} (${GOARCH})..."
curl -fL --retry 3 --retry-delay 2 -o "${tmpdir}/kaniko.tar" "$URL"

echo "kaniko: extracting executor from OCI image layout..."
EXECUTOR="$EXECUTOR" python3 - "$tmpdir" "$EXECUTOR" <<'PY'
import json, tarfile, gzip, os, io, shutil, sys

work, install = sys.argv[1], sys.argv[2]
oci = os.path.join(work, "oci")
os.makedirs(oci, exist_ok=True)
with tarfile.open(os.path.join(work, "kaniko.tar")) as outer:
    outer.extractall(oci, filter="data")
with open(os.path.join(oci, "manifest.json")) as f:
    layers = json.load(f)[0]["Layers"]
root = os.path.join(work, "root")
os.makedirs(root, exist_ok=True)
for layer in layers:
    path = os.path.join(oci, layer)
    with open(path, "rb") as f:
        data = f.read()
    try:
        data = gzip.decompress(data)
    except OSError:
        pass
    with tarfile.open(fileobj=io.BytesIO(data)) as tf:
        tf.extractall(root, filter="data")
src = os.path.join(root, "kaniko", "executor")
if not os.path.isfile(src):
    raise SystemExit("executor binary not found in kaniko image layers")
os.makedirs(os.path.dirname(install), exist_ok=True)
shutil.copy2(src, install)
os.chmod(install, 0o755)
PY

ln -sfn "$EXECUTOR" "$LINK_DIR/executor"
echo "kaniko: installed ${EXECUTOR}"
echo "kaniko: PATH link ${LINK_DIR}/executor -> ${EXECUTOR}"
echo "kaniko: template builds run the executor inside bubblewrap (bwrap)"
