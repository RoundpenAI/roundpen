#!/bin/bash
# Wait for the LightDM/XFCE display, then run Chrome with DevTools on :9333.
set -euo pipefail
export DISPLAY="${DISPLAY:-:0}"
if [ -z "${XAUTHORITY:-}" ]; then
	export XAUTHORITY=/home/roundpen/.Xauthority
fi
for _ in $(seq 1 90); do
	if [ ! -f "$XAUTHORITY" ]; then
		for cand in /home/roundpen/.Xauthority /var/run/lightdm/roundpen/xauthority; do
			if [ -f "$cand" ]; then
				export XAUTHORITY="$cand"
				break
			fi
		done
	fi
	if [ -S /tmp/.X11-unix/X0 ] && [ -f "$XAUTHORITY" ]; then
		break
	fi
	sleep 1
done
exec /usr/bin/google-chrome-stable \
	--no-first-run \
	--no-default-browser-check \
	--no-sandbox \
	--disable-gpu \
	--disable-dev-shm-usage \
	--user-data-dir=/home/roundpen/.config/google-chrome-cdp \
	--remote-allow-origins='*' \
	--remote-debugging-address=127.0.0.1 \
	--remote-debugging-port=9333 \
	about:blank
