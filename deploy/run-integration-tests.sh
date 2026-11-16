#!/usr/bin/env bash
# Runs the Go unit and integration tests against a throwaway database on the
# local Postgres container. Production data is never touched: the tests use
# their own database (superbot_test), which is dropped and recreated here.
#
# Usage: deploy/run-integration-tests.sh [path-to-env-file]
set -euo pipefail
cd "$(dirname "$0")/.."

env_file="${1:-/srv/superbot/.env}"
if [[ ! -f "$env_file" ]]; then
  echo "env file not found: $env_file" >&2
  exit 1
fi
pg_password="$(grep -E '^POSTGRES_PASSWORD=' "$env_file" | head -1 | cut -d= -f2- | sed -e 's/^"//' -e 's/"$//')"
if [[ -z "$pg_password" ]]; then
  echo "POSTGRES_PASSWORD not found in $env_file" >&2
  exit 1
fi
encoded="$(python3 -c 'import sys, urllib.parse; print(urllib.parse.quote(sys.argv[1], safe=""))' "$pg_password")"

docker exec superbot-postgres psql -U superbot -d postgres -v ON_ERROR_STOP=1 \
  -c "DROP DATABASE IF EXISTS superbot_test WITH (FORCE)" \
  -c "CREATE DATABASE superbot_test"

docker build -q -t superbot/test:latest -f deploy/test.Dockerfile .

docker run --rm --network superbot_default \
  -e SUPERBOT_TEST_DATABASE_URL="postgres://superbot:${encoded}@postgres:5432/superbot_test?sslmode=disable" \
  -e SUPERBOT_TEST_VERBOSE="${SUPERBOT_TEST_VERBOSE:-}" \
  superbot/test:latest "${@:2}"

docker exec superbot-postgres psql -U superbot -d postgres -c "DROP DATABASE IF EXISTS superbot_test WITH (FORCE)" >/dev/null
echo "integration tests finished"
