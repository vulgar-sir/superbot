#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

stamp="$(date +%Y%m%d-%H%M%S)"
mkdir -p backups

docker compose --env-file .env -f deploy/docker-compose.yml exec -T postgres \
  pg_dump -U superbot -Fc superbot > "backups/superbot-${stamp}.dump"

tar -czf "backups/data-${stamp}.tar.gz" \
  --exclude='data/**/.chromium/**' \
  --exclude='data/**/artifacts/**' \
  data 2>/dev/null || true

# Keep 7 days of backups.
find backups -type f -mtime +7 -delete

echo "backup written: backups/superbot-${stamp}.dump"
echo "remember: SUPERBOT_SECRET_KEY is NOT in the dump and must be kept separately"
