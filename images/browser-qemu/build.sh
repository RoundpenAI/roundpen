#!/usr/bin/env bash
# Build a kernel-bootable Browser qcow2 + vmlinuz/initrd sidecars.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
OUT="$ROOT/out"
IMG="$OUT/browser.qcow2"
SIZE="${ROUNDPEN_BROWSER_DISK_SIZE:-20G}"
mkdir -p "$OUT"

echo "==> building rootfs image roundpen-browser-rootfs"
docker build -t roundpen-browser-rootfs "$ROOT"

CID="$(docker create roundpen-browser-rootfs)"
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

cat > "$OUT/boot.json" <<'EOF'
{
  "kernel": "vmlinuz",
  "initrd": "initrd.img",
  "append": "root=/dev/vda rw console=tty0 console=ttyS0 systemd.unit=graphical.target"
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

pack_with_docker_loop() {
  echo "==> privileged docker loop+mkfs → $IMG"
  docker run --rm --privileged \
    -v "$OUT:/out" \
    ubuntu:24.04 \
    bash -ceu '
      export DEBIAN_FRONTEND=noninteractive
      apt-get update -qq
      apt-get install -y -qq qemu-utils e2fsprogs >/dev/null
      qemu-img create -f raw /out/disk.raw '"$SIZE"'
      mkfs.ext4 -F -L root /out/disk.raw
      mkdir -p /mnt/root
      mount -o loop /out/disk.raw /mnt/root
      tar -xf /out/rootfs.tar -C /mnt/root
      umount /mnt/root
      qemu-img convert -f raw -O qcow2 /out/disk.raw /out/browser.qcow2
      rm -f /out/disk.raw
    '
}

if command -v virt-make-fs >/dev/null 2>&1; then
  pack_with_virt_make_fs
elif command -v guestfish >/dev/null 2>&1 && command -v qemu-img >/dev/null 2>&1; then
  pack_with_guestfish
elif command -v docker >/dev/null 2>&1; then
  pack_with_docker_loop
else
  echo "ERROR: need virt-make-fs, guestfish, or docker to pack the qcow2" >&2
  exit 1
fi

rm -f "$OUT/rootfs.tar" "$OUT/BUILD_INCOMPLETE.txt"
echo "OK: $IMG"
echo "    $OUT/vmlinuz"
echo "    $OUT/initrd.img"
echo "    $OUT/boot.json"
