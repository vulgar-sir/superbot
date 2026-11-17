#!/bin/sh
# Desktop computer: the browser computer plus an XFCE session, so the bot can
# run and operate ordinary Linux GUI programs.
. /usr/local/lib/superbot/computer-init.sh
set -eu

sb_prepare_home
sb_start_display

dbus-launch --sh-syntax > /tmp/dbus.env 2>/dev/null || true
if [ -f /tmp/dbus.env ]; then
  . /tmp/dbus.env
  export DBUS_SESSION_BUS_ADDRESS
fi

# The panel asks on its very first start whether to use the default layout,
# and that dialog would sit on top of everything the bot does. Settings now
# persist in the bot's home, so the default layout is seeded once.
panel="$HOME/.config/xfce4/xfconf/xfce-perchannel-xml/xfce4-panel.xml"
if [ ! -f "$panel" ] && [ -f /etc/xdg/xfce4/panel/default.xml ]; then
  mkdir -p "$(dirname "$panel")"
  cp /etc/xdg/xfce4/panel/default.xml "$panel"
fi

startxfce4 >/tmp/xfce.log 2>&1 &
sleep 3

# Keep Chromium available on the desktop and expose CDP for the browser tools.
sb_start_chromium
sb_start_remote_access
sb_stop_on_term

wait
