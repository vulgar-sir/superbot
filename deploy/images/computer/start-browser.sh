#!/bin/sh
# Browser computer: a virtual screen with Chromium, remote-controlled by the
# worker over CDP and watched by the user over noVNC (both behind gate.py).
. /usr/local/lib/superbot/computer-init.sh
set -eu

sb_prepare_home
sb_start_display
sb_start_chromium
sb_start_remote_access
sb_stop_on_term

wait
