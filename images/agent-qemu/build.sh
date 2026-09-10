#!/usr/bin/env bash
# Build a kernel-bootable agent.qcow2 + vmlinuz/initrd sidecars.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
OUT="${ROUNDPEN_AGENT_OUT:-$ROOT/out}"
IMAGE_NAME="${ROUNDPEN_AGENT_DOCKER_IMAGE:-roundpen-agent-qemu:local}"
SIZE="${ROUNDPEN_AGENT_DISK:-8G}"
IMG="$OUT/agent.qcow2"
mkdir -p "$OUT"

echo "==> docker build $IMAGE_NAME"
docker build -t "$IMAGE_NAME" "$ROOT"

CID="$(docker create "$IMAGE_NAME")"
cleanup() { docker rm -f "$CID" >/dev/null 2>&1 || true; }
trap cleanup EXIT

TAR="$OUT/rootfs.tar"
echo "==> exporting rootfs"
docker export "$CID" > "$TAR"

echo "==> extracting kernel/initrd sidecars"
rm -f "$OUT/vmlinuz" "$OUT/initrd.img"
python3 - "$TAR" "$OUT" <<'PY'
import sys, tarfile
from pathlib import Path
tar_path, out_dir = Path(sys.argv[1]), Path(sys.argv[2])
kernels, initrds = [], []
with tarfile.open(tar_path, "r") as tf:
    for m in tf.getmembers():
        name = m.name.lstrip("./")
        if not m.isfile():
            continue
        if name.startswith("boot/vmlinuz-"):
            kernels.append(name)
        elif name.startswith("boot/initrd.img-"):
            initrds.append(name)
    if not kernels or not initrds:
        raise SystemExit("no kernel/initrd in rootfs; linux-image-virtual missing from Dockerfile")
    kernels.sort()
    initrds.sort()
    k, i = kernels[-1], initrds[-1]
    print(f"kernel {k}")
    print(f"initrd {i}")
    with tf.extractfile(k) as src, open(out_dir / "vmlinuz", "wb") as dst:
        dst.write(src.read())
    with tf.extractfile(i) as src, open(out_dir / "initrd.img", "wb") as dst:
        dst.write(src.read())
PY
chmod 644 "$OUT/vmlinuz" "$OUT/initrd.img"

CLAUDE_VER="$(docker run --rm --entrypoint claude "$IMAGE_NAME" --version 2>/dev/null || echo unknown)"
NODE_VER="$(docker run --rm --entrypoint node "$IMAGE_NAME" --version 2>/dev/null || echo unknown)"
cat > "$OUT/boot.json" <<EOF
{
  "kernel": "vmlinuz",
  "initrd": "initrd.img",
  "append": "root=/dev/vda rw console=tty0 console=ttyS0 systemd.unit=multi-user.target systemd.hostname=roundpen-agent",
  "claude_version": "${CLAUDE_VER}",
  "node_version": "${NODE_VER}"
}
EOF

pack_with_virt_make_fs() {
  echo "==> virt-make-fs → $IMG"
  virt-make-fs --format=qcow2 --type=ext4 --size="$SIZE" "$TAR" "$IMG"
}

pack_with_guestfish() {
  echo "==> guestfish → $IMG"
  qemu-img create -f qcow2 "$IMG" "$SIZE"
  guestfish -a "$IMG" <<EOF
run
mkfs ext4 /dev/sda
mount /dev/sda /
tar-in $TAR /
EOF
}

# Host mkfs + privileged Docker (same path as images/browser-qemu).
pack_with_host_tools() {
  local mkfs=""
  if command -v mkfs.ext4 >/dev/null 2>&1; then
    mkfs=mkfs.ext4
  elif [[ -x /usr/sbin/mkfs.ext4 ]]; then
    mkfs=/usr/sbin/mkfs.ext4
  else
    return 1
  fi
  command -v qemu-img >/dev/null 2>&1 || return 1
  command -v docker >/dev/null 2>&1 || return 1
  echo "==> host mkfs + privileged docker tar → $IMG"
  qemu-img create -f raw "$OUT/disk.raw" "$SIZE"
  "$mkfs" -F -L root "$OUT/disk.raw"
  docker run --rm --privileged \
    -v "$OUT:/out" \
    ubuntu:24.04 \
    bash -ceu 'mkdir -p /mnt/root && mount -o loop /out/disk.raw /mnt/root && tar -xf /out/rootfs.tar -C /mnt/root && sync && umount /mnt/root'
  qemu-img convert -f raw -O qcow2 "$OUT/disk.raw" "$IMG"
  rm -f "$OUT/disk.raw"
}

if command -v virt-make-fs >/dev/null 2>&1; then
  pack_with_virt_make_fs
elif command -v guestfish >/dev/null 2>&1 && command -v qemu-img >/dev/null 2>&1; then
  pack_with_guestfish
elif command -v qemu-img >/dev/null 2>&1 && command -v docker >/dev/null 2>&1 && { command -v mkfs.ext4 >/dev/null 2>&1 || [[ -x /usr/sbin/mkfs.ext4 ]]; }; then
  echo "    (install guestfs-tools for virt-make-fs)"
  pack_with_host_tools
else
  echo "ERROR: need virt-make-fs, guestfish, or qemu-img+mkfs.ext4+docker to pack the qcow2" >&2
  echo "  debian/ubuntu: sudo apt-get install -y guestfs-tools" >&2
  exit 1
fi

rm -f "$OUT/rootfs.tar" "$OUT/BUILD_INCOMPLETE.txt"
echo "OK: $IMG"
echo "    $OUT/vmlinuz"
echo "    $OUT/initrd.img"
echo "    $OUT/boot.json"
echo "    claude ${CLAUDE_VER}  node ${NODE_VER}"
echo
echo "Point the qemu backend at this image:"
echo "  ROUNDPEN_AGENT_IMAGE=$IMG"
echo "  ROUNDPEN_DEFAULT_AGENT_TEMPLATE=agent-claude"
