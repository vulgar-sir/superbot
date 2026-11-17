# Deployment

This directory contains the production Docker Compose stack, the Caddy entry point, sandbox images and operational scripts. The main [README](../README.md) has the product overview; the [AI-assisted deployment guide](../docs/deploy-with-ai.md) has a copy-paste prompt and runbook for handing the deployment to an AI agent.

> 中文：本目录包含生产 Compose 栈、Caddy 入口、沙箱镜像和运维脚本。让 AI 代为部署时，请使用 [`docs/deploy-with-ai.zh-CN.md`](../docs/deploy-with-ai.zh-CN.md)。

## Files

| Path | Purpose |
|---|---|
| `docker-compose.yml` | Production stack: PostgreSQL, API, worker, docker-proxy, web, Caddy, optional Dex/STT/TTS/embedding. |
| `docker-compose.dev.yml` | Local development PostgreSQL only. |
| `.env.production.example` | Production environment template. Copy to `.env` at the repository root. |
| `Caddyfile` | HTTP entry point; `/api/*` to the Go API, `/healthz`, `/desktop/*` static feed, `/dex/*`, everything else to Next.js. |
| `Dockerfile.api` | Go API/worker image. |
| `Dockerfile.web` | Next.js image. |
| `images/computer/` | Persistent Chromium and XFCE desktop images (`Dockerfile`, `Dockerfile.desktop`). |
| `images/embedding/`, `images/tts/` | Self-hosted embedding and TTS services. |
| `harden-data-permissions.sh` | One-time, idempotent data-directory permission layout. |
| `deploy.sh` | Build and upgrade helper. |
| `backup.sh` | PostgreSQL and data backup helper. |
| `run-integration-tests.sh` | Runs unit and integration tests against a disposable `superbot_test` database on the server. |
| `soak.py` | Long-running health/soak checks for Web, API, memory and SSE. |
| `acceptance_runtime.py` | Runtime acceptance checks against a live instance. |
| `dex/` | Self-hosted OIDC/SSO template. |

## Quick reference

```bash
# From the repository root; .env must exist.
docker compose --env-file .env -f deploy/docker-compose.yml build api web
docker build -t superbot/computer:latest \
  -f deploy/images/computer/Dockerfile deploy/images/computer
docker build -t superbot/computer-desktop:latest \
  -f deploy/images/computer/Dockerfile.desktop deploy/images/computer

sudo mkdir -p /srv/superbot/data
sudo bash deploy/harden-data-permissions.sh /srv/superbot/data

docker compose --env-file .env -f deploy/docker-compose.yml \
  up -d postgres api worker docker-proxy web caddy
```

Open `http://localhost:32608`. Optional services (`dex`, `embedding`, `stt`, `tts`) are documented in the main README and the AI deployment guide.

## Security notes

- `.env` is mode 600 and never committed.
- The worker has no Docker socket; it reaches Docker through `docker-proxy`, which allowlists sandbox lifecycle endpoints only.
- The API and worker run as uid 1000. Run `harden-data-permissions.sh` before creating workspaces.
- Production browser/desktop containers must not publish host ports and must stay on `superbot_sandbox`.
- Use HTTPS and `SUPERBOT_COOKIE_INSECURE=false` for anything exposed to the internet.
- Back up PostgreSQL and `SUPERBOT_DATA_DIR`, encrypt the backups, and test restores.

## Operations

- **Upgrade:** pull the new revision, rebuild `api web` and the computer images, run migrations on startup, then recreate `api worker web caddy`.
- **Backup:** use `backup.sh` or your own PostgreSQL dump plus a copy of `SUPERBOT_DATA_DIR`.
- **Soak test:** `python3 deploy/soak.py` with `SUPERBOT_SOAK_BASE` and `SUPERBOT_SOAK_HOURS`.
- **Integration tests:** `bash deploy/run-integration-tests.sh` on the server.

See [`docs/configuration-required.md`](../docs/configuration-required.md) for capabilities that need external credentials, network access or platform support.
