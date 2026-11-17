# Shared start-up steps of the browser and desktop computers. Sourced by
# start-browser.sh and start-desktop.sh; POSIX sh.

. /usr/local/lib/superbot/env.sh

sb_prepare_home() {
    mkdir -p "$HOME/.config" "$XDG_CACHE_HOME" /tmp/.X11-unix
    # Files the browser downloads land in the visible workspace instead of a
    # hidden folder. Written once; the bot may change it.
    if [ ! -f "$HOME/.config/user-dirs.dirs" ]; then
        cat > "$HOME/.config/user-dirs.dirs" <<'EOF'
XDG_DESKTOP_DIR="$HOME/Desktop"
XDG_DOWNLOAD_DIR="/workspace/Downloads"
XDG_DOCUMENTS_DIR="/workspace"
XDG_TEMPLATES_DIR="$HOME/Templates"
XDG_PUBLICSHARE_DIR="$HOME/Public"
XDG_MUSIC_DIR="$HOME/Music"
XDG_PICTURES_DIR="$HOME/Pictures"
XDG_VIDEOS_DIR="$HOME/Videos"
EOF
    fi
}

sb_start_display() {
    # -s 0 / -dpms: the screen never blanks, so screenshots and OCR always
    # see the desktop.
    Xvfb :99 -screen 0 1440x900x24 -nolisten tcp -s 0 -dpms >/tmp/xvfb.log 2>&1 &
    i=0
    while [ ! -S /tmp/.X11-unix/X99 ] && [ "$i" -lt 50 ]; do
        sleep 0.2
        i=$((i + 1))
    done
}

sb_start_chromium() {
    # A container can be recreated while the old Chromium still owns the
    # persistent profile; clear the stale singleton locks before launch.
    rm -f /workspace/.chromium/SingletonLock /workspace/.chromium/SingletonCookie \
          /workspace/.chromium/SingletonSocket

    # Logins, cookies and history persist in the profile on disk, but caches
    # do not: Chromium's caches go to the container's tmpfs, capped, so
    # browsing does not grow the bot's workspace. Caches an older profile
    # left on disk are dropped.
    rm -rf /workspace/.chromium/Default/Cache "/workspace/.chromium/Default/Code Cache" \
           /workspace/.chromium/Default/GPUCache /workspace/.chromium/Default/DawnCache \
           /workspace/.chromium/Default/DawnGraphiteCache /workspace/.chromium/Default/DawnWebGPUCache \
           /workspace/.chromium/GrShaderCache /workspace/.chromium/ShaderCache \
           /workspace/.chromium/GraphiteDawnCache

    # DevTools listens on loopback only; gate.py exposes it to the worker.
    # --test-type drops the "unsupported command-line flag: --no-sandbox"
    # bar, which the user sees and the bot's screen reading would read.
    chromium \
      --no-sandbox \
      --disable-dev-shm-usage \
      --disable-gpu \
      --user-data-dir=/workspace/.chromium \
      --disk-cache-dir=/tmp/chromium-cache \
      --disk-cache-size=104857600 \
      --remote-debugging-port=9222 \
      --no-first-run \
      --no-default-browser-check \
      --disable-features=Translate \
      --hide-crash-restore-bubble \
      --test-type \
      about:blank >/tmp/chromium.log 2>&1 &
    SB_CHROMIUM_PID=$!
}

# sb_stop_on_term lets Chromium save its session when the computer is put
# to sleep (docker stop allows the container a few seconds).
sb_stop_on_term() {
    trap 'kill -TERM "${SB_CHROMIUM_PID:-}" 2>/dev/null; sleep 2; exit 0' TERM INT
}

sb_start_remote_access() {
    # VNC and websockify listen on loopback only. The gate is the only thing
    # on the container network and requires the computer's secret:
    #   9223 -> Chromium DevTools (127.0.0.1:9222)
    #   6080 -> noVNC/websockify  (127.0.0.1:6081)
    x11vnc -display :99 -forever -shared -nopw -localhost -rfbport 5900 >/tmp/x11vnc.log 2>&1 &
    websockify --web=/usr/share/novnc 127.0.0.1:6081 127.0.0.1:5900 >/tmp/websockify.log 2>&1 &
    python3 /usr/local/lib/superbot/gate.py 9223:9222 6080:6081 >/tmp/gate.log 2>&1 &
}
