#!/bin/bash
# Container-smoke entrypoint. The QEMU guest boots systemd → lightdm instead.
set -euo pipefail
export DISPLAY="${DISPLAY:-:0}"
if ! pgrep -x Xorg >/dev/null 2>&1 && ! pgrep -x Xvfb >/dev/null 2>&1; then
  if command -v Xvfb >/dev/null 2>&1; then
    Xvfb "$DISPLAY" -screen 0 1280x800x24 &
    sleep 1
  fi
fi
if command -v startxfce4 >/dev/null 2>&1; then
  exec startxfce4
fi
exec sleep infinity
