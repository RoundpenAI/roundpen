# Browser QEMU image (XFCE + Chrome CDP)

Default Roundpen **Browser** slot disk. Guest runs systemd, LightDM autologin,
XFCE, and Google Chrome with remote debugging on `:9222`. Desktop display is
provided by **QEMU native VNC on a host Unix socket** — do **not** install
noVNC / websockify / x11vnc in the guest.

The Docker rootfs is **not** BIOS-bootable. `build.sh` extracts `vmlinuz` and
`initrd.img`; the QEMU backend boots with `-kernel` / `-initrd`.

## Output

```
images/browser-qemu/out/browser.qcow2
images/browser-qemu/out/vmlinuz
images/browser-qemu/out/initrd.img
images/browser-qemu/out/boot.json
```

Seeded template `browser-desktop` points at the qcow2 (override with
`ROUNDPEN_BROWSER_IMAGE`).

## Build

Needs Docker (and either `virt-make-fs`, `guestfish`, or privileged Docker to
pack the qcow2):

```bash
./images/browser-qemu/build.sh
# or: make browser-image
```

## Guest expectations

| Service | Port / path |
|---------|-------------|
| Chrome CDP | Guest socat `:9222` → Chrome `127.0.0.1:9333` (hostfwd by qemu backend) |
| Display | QEMU `-vnc unix:…/vnc.sock` (host side) |
| Init | systemd → LightDM autologin (`roundpen`) → XFCE |

Chrome flags (see `guest/chrome-cdp.desktop`):

```
--remote-debugging-port=9333 --user-data-dir=… --disable-gpu
```

A systemd `cdp-proxy` (`socat`) publishes that onto `0.0.0.0:9222` so QEMU hostfwd can reach it. Chrome 120+ refuses to bind DevTools on a public address.
```
