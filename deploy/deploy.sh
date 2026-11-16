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
docker compose --env-file .env -f deploy/docker-compose.yml up -d
docker compose --env-file .env -f deploy/docker-compose.yml ps
