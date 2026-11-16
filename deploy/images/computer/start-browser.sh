#!/bin/sh
set -eu

export DISPLAY=:99
mkdir -p /tmp/.X11-unix

Xvfb :99 -screen 0 1440x900x24 -nolisten tcp &
sleep 1

# A container can be recreated while the old Chromium still owns the
# persistent profile; clear the stale singleton locks before launch.
rm -f /workspace/.chromium/SingletonLock /workspace/.chromium/SingletonCookie \
      /workspace/.chromium/SingletonSocket

chromium \
  --no-sandbox \
  --disable-dev-shm-usage \
  --disable-gpu \
  --user-data-dir=/workspace/.chromium \
  --remote-debugging-address=0.0.0.0 \
  --remote-debugging-port=9222 \
  --no-first-run \
  --no-default-browser-check \
  --disable-features=Translate \
  about:blank &
sleep 2

# Chromium binds CDP to its loopback address even with --remote-debugging-address.
# Expose the CDP endpoint on the container network through a tiny TCP proxy.
python3 /usr/local/bin/cdp_proxy.py >/tmp/cdp_proxy.log 2>&1 &

x11vnc -display :99 -forever -shared -nopw -listen 0.0.0.0 -rfbport 5900 >/tmp/x11vnc.log 2>&1 &

# noVNC/websockify serves the static UI and proxies WebSocket to VNC.
websockify --web=/usr/share/novnc 6080 127.0.0.1:5900 >/tmp/websockify.log 2>&1 &

wait
