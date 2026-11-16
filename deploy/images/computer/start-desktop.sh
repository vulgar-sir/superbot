#!/bin/sh
set -eu

export DISPLAY=:99
export HOME=/tmp
mkdir -p /tmp/.X11-unix

Xvfb :99 -screen 0 1440x900x24 -nolisten tcp &
sleep 1

dbus-launch --sh-syntax > /tmp/dbus.env 2>/dev/null || true
if [ -f /tmp/dbus.env ]; then
  . /tmp/dbus.env
fi

startxfce4 >/tmp/xfce.log 2>&1 &
sleep 3

# Keep Chromium available on the desktop and expose CDP for the browser tools.
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
  about:blank >/tmp/chromium.log 2>&1 &
sleep 2

python3 /usr/local/bin/cdp_proxy.py >/tmp/cdp_proxy.log 2>&1 &

x11vnc -display :99 -forever -shared -nopw -listen 0.0.0.0 -rfbport 5900 >/tmp/x11vnc.log 2>&1 &
websockify --web=/usr/share/novnc 6080 127.0.0.1:5900 >/tmp/websockify.log 2>&1 &

wait
