#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

if [[ ! -f .env ]]; then
  echo ".env not found; copy .env.example to .env and fill it in" >&2
  exit 1
fi

docker compose --env-file .env -f deploy/docker-compose.yml build api web embedding tts
docker build -t superbot/computer:latest -f deploy/images/computer/Dockerfile deploy/images/computer
docker build -t superbot/computer-desktop:latest -f deploy/images/computer/Dockerfile.desktop deploy/images/computer
# The API and worker run as uid 1000; sandbox mount sources must stay out of
# their reach. Idempotent.
bash deploy/harden-data-permissions.sh "$(pwd)/data"
docker compose --env-file .env -f deploy/docker-compose.yml up -d
docker compose --env-file .env -f deploy/docker-compose.yml ps
# Keep Docker's disk in check: drop dangling images left by rebuilds and
# build cache older than a week (bot computers share one image, so this
# never touches their files).
docker image prune -f >/dev/null
docker builder prune -f --filter until=168h >/dev/null
