# Superbot computer environment.
#
# Sourced by login shells (/etc/profile.d/superbot.sh), interactive bash
# (/etc/bash.bashrc) and the computer's start scripts, so the shell tool, the
# desktop session and the terminal all see the same environment. It must stay
# POSIX sh: the shell tool runs `sh -lc` (dash).
#
# The bot has no root. Its home, and everything it installs for itself, lives
# in the persistent workspace:
#   $HOME                      /workspace/.home (settings, app data, pip --user)
#   $HOME/.local/bin           pip --user, npm -g, AppImage launchers
#   $HOME/.local/apt           Debian packages unpacked by `userapt`
# Caches stay on the container's tmpfs so they never grow the workspace.

: "${HOME:=/workspace/.home}"
SB_APT_ROOT="$HOME/.local/apt"
: "${XDG_CACHE_HOME:=/tmp/.cache}"
: "${XDG_RUNTIME_DIR:=/tmp/.runtime}"
: "${PIP_BREAK_SYSTEM_PACKAGES:=1}"
: "${NPM_CONFIG_PREFIX:=$HOME/.local}"
: "${NPM_CONFIG_CACHE:=/tmp/.npm}"
: "${APPIMAGE_EXTRACT_AND_RUN:=1}"
: "${NO_AT_BRIDGE:=1}"
: "${DISPLAY:=:99}"
export HOME SB_APT_ROOT XDG_CACHE_HOME XDG_RUNTIME_DIR PIP_BREAK_SYSTEM_PACKAGES \
    NPM_CONFIG_PREFIX NPM_CONFIG_CACHE APPIMAGE_EXTRACT_AND_RUN NO_AT_BRIDGE DISPLAY

[ -d "$HOME" ] || mkdir -p "$HOME" 2>/dev/null || true

# Programs started from a shell join the desktop session's D-Bus (written
# by start-desktop.sh), as programs started from the desktop menu do.
if [ -z "${DBUS_SESSION_BUS_ADDRESS:-}" ] && [ -r /tmp/dbus.env ]; then
    . /tmp/dbus.env
    export DBUS_SESSION_BUS_ADDRESS
fi
[ -d "$XDG_RUNTIME_DIR" ] || { mkdir -p "$XDG_RUNTIME_DIR" && chmod 700 "$XDG_RUNTIME_DIR"; } 2>/dev/null || true

# sb_prepend VAR DIR puts DIR in front of the colon-separated VAR unless it
# is already there.
sb_prepend() {
    eval "sb_cur=\${$1:-}"
    case ":$sb_cur:" in
        *":$2:"*) ;;
        *) if [ -n "$sb_cur" ]; then eval "$1=\"\$2:\$sb_cur\""; else eval "$1=\"\$2\""; fi ;;
    esac
}

# Debian's /etc/profile resets PATH, so the user directories go back in front.
sb_prepend PATH "$SB_APT_ROOT/usr/games"
sb_prepend PATH "$SB_APT_ROOT/bin"
sb_prepend PATH "$SB_APT_ROOT/usr/bin"
sb_prepend PATH "$HOME/.local/bin"
export PATH

# userapt never unpacks a package the image already has, so these hold only
# libraries the system lacks and cannot shadow a system library.
sb_prepend LD_LIBRARY_PATH "$SB_APT_ROOT/usr/lib"
sb_prepend LD_LIBRARY_PATH "$SB_APT_ROOT/lib/x86_64-linux-gnu"
sb_prepend LD_LIBRARY_PATH "$SB_APT_ROOT/usr/lib/x86_64-linux-gnu"
export LD_LIBRARY_PATH

# Menus, icons and .desktop files of unpacked packages.
[ -n "${XDG_DATA_DIRS:-}" ] || XDG_DATA_DIRS=/usr/local/share:/usr/share
sb_prepend XDG_DATA_DIRS "$SB_APT_ROOT/usr/share"
export XDG_DATA_DIRS

# Further search paths only once a package needs them, so an empty install
# changes nothing.
if [ -d "$SB_APT_ROOT/usr/lib/x86_64-linux-gnu/girepository-1.0" ]; then
    sb_prepend GI_TYPELIB_PATH "$SB_APT_ROOT/usr/lib/x86_64-linux-gnu/girepository-1.0"
    export GI_TYPELIB_PATH
fi
for sb_dir in "$SB_APT_ROOT/usr/share/perl5" "$SB_APT_ROOT/usr/lib/x86_64-linux-gnu/perl5/5.36"; do
    if [ -d "$sb_dir" ]; then sb_prepend PERL5LIB "$sb_dir"; export PERL5LIB; fi
done
for sb_dir in "$SB_APT_ROOT/usr/lib/x86_64-linux-gnu/qt5/plugins" "$SB_APT_ROOT/usr/lib/x86_64-linux-gnu/qt6/plugins"; do
    if [ -d "$sb_dir" ]; then sb_prepend QT_PLUGIN_PATH "$sb_dir"; export QT_PLUGIN_PATH; fi
done
for sb_dir in "$SB_APT_ROOT/usr/share/pkgconfig" "$SB_APT_ROOT/usr/lib/x86_64-linux-gnu/pkgconfig"; do
    if [ -d "$sb_dir" ]; then sb_prepend PKG_CONFIG_PATH "$sb_dir"; export PKG_CONFIG_PATH; fi
done
# userapt compiles the system's GSettings schemas together with those of
# unpacked packages here; GTK apps abort when a schema they use is missing.
if [ -f "$HOME/.local/share/glib-2.0/schemas/gschemas.compiled" ]; then
    GSETTINGS_SCHEMA_DIR="$HOME/.local/share/glib-2.0/schemas"
    export GSETTINGS_SCHEMA_DIR
fi

unset sb_cur sb_dir
unset -f sb_prepend 2>/dev/null || true
