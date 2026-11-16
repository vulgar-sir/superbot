#!/usr/bin/env bash
# Publish an electron-builder release directory to the update feed served by Caddy.
#
# Usage (on the server, or anywhere that can write the feed directory):
#   deploy/publish-desktop-release.sh /path/to/desktop/release
#   SUPERBOT_DESKTOP_RELEASE_DIR=/srv/superbot/deploy/desktop-release \
#     deploy/publish-desktop-release.sh /path/to/desktop/release
#
# Caddy serves `/desktop/*` from the directory bind-mounted in
# deploy/docker-compose.yml, i.e. `/srv/superbot/deploy/desktop-release`
# when the project lives in /srv/superbot. Do not upload to
# /srv/superbot/desktop-release: that path is not mounted.
set -euo pipefail

src=${1:?usage: publish-desktop-release.sh <release-dir>}
dst=${SUPERBOT_DESKTOP_RELEASE_DIR:-/srv/superbot/deploy/desktop-release}

if [[ ! -f "$src/latest.yml" ]]; then
  echo "latest.yml not found in $src" >&2
  exit 1
fi

version=$(awk '/^version:/{print $2; exit}' "$src/latest.yml")
installer="superbot Setup ${version}.exe"
blockmap="${installer}.blockmap"

for file in "latest.yml" "$installer" "$blockmap"; do
  if [[ ! -f "$src/$file" ]]; then
    echo "missing artifact: $src/$file" >&2
    exit 1
  fi
done

mkdir -p "$dst"
install -m 0644 "$src/latest.yml" "$dst/latest.yml"
install -m 0644 "$src/$installer" "$dst/$installer"
install -m 0644 "$src/$blockmap" "$dst/$blockmap"

# electron-updater resolves Windows channel files as <channel>.yml. The app's
# stable/beta settings therefore need stable.yml and beta.yml; without them the
# update check fails with ERR_UPDATER_CHANNEL_FILE_NOT_FOUND. Until a separate
# beta build exists, both channels point at the same published version.
cp -f "$dst/latest.yml" "$dst/stable.yml"
cp -f "$dst/latest.yml" "$dst/beta.yml"

echo "published ${version} to ${dst}"
