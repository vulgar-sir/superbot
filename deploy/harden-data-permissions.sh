#!/usr/bin/env bash
# One-time (idempotent) permission layout for the data directory.
#
# The API and worker run as uid 1000, the same user as the bot sandboxes.
# Sandbox containers bind-mount data/workspaces/<bot> and
# data/teams/<team>/shared; Docker resolves those host paths on every start,
# including automatic restarts. If a service could replace one of them (or a
# parent) with a symlink, the next start would mount any host path. So:
#   - data/, data/workspaces, data/teams and data/teams/<team> are root-owned
#     and not writable by uid 1000;
#   - the leaf mount directories and everything else the services write are
#     owned by uid 1000.
# New leaf directories are created by docker-proxy (root) on request.
set -euo pipefail

data="${1:-/srv/superbot/data}"
cd "$data"

links=$(find . -maxdepth 3 -type l \( -path './workspaces/*' -o -path './teams/*' \) | head -5)
if [[ -n "$links" ]]; then
  echo "refusing: symlinks found where sandbox mounts come from:" >&2
  echo "$links" >&2
  exit 1
fi

mkdir -p workspaces teams avatars library snapshots skills devices
chown root:root . workspaces teams
chmod 0755 . workspaces teams
for team in teams/*/; do
  [[ -d "$team" ]] || continue
  chown root:root "$team"
  chmod 0755 "$team"
  mkdir -p "$team/shared"
  chown -R 1000:1000 "$team/shared"
  chmod 0777 "$team/shared"
done
for workspace in workspaces/*/; do
  [[ -d "$workspace" ]] || continue
  chown -R 1000:1000 "$workspace"
  chmod 0777 "$workspace"
done
for entry in * .[!.]*; do
  [[ -e "$entry" ]] || continue
  case "$entry" in
    workspaces|teams) ;;
    *) chown -R 1000:1000 "$entry" ;;
  esac
done
echo "data permissions hardened under $data"
