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
| `images/computer/` | Bot computer images: browser (`Dockerfile`) and XFCE desktop (`Dockerfile.desktop`, built on the browser image), plus the gate, the environment and the user-space installers (`userapt`, `appimage-install`). See [Bot computers](#bot-computers). |
| `sandbox-firewall.sh`, `systemd/` | Host firewall that keeps bot computers off private addresses and the host; see [Bot computers](#bot-computers). |
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

Open `http://<server>:32608` (or `http://localhost:32608` on the host itself). Optional services (`dex`, `embedding`, `stt`, `tts`) are documented in the main README and the AI deployment guide.

## Security notes

- `.env` is mode 600 and never committed.
- The worker has no Docker socket; it reaches Docker through `docker-proxy`, which allowlists sandbox lifecycle endpoints only.
- The API and worker run as uid 1000. Run `harden-data-permissions.sh` before creating workspaces.
- Production browser/desktop containers must not publish host ports and must stay on `superbot_sandbox`.
- Each computer's browser control (CDP) and screen (noVNC) are reachable only through its gate with a per-computer secret; the worker and the API derive it from `SUPERBOT_SECRET_KEY`. Apply `sandbox-firewall.sh` so computers cannot reach private addresses or the host.
- Use HTTPS and `SUPERBOT_COOKIE_INSECURE=false` for anything exposed to the internet.
- Back up PostgreSQL and `SUPERBOT_DATA_DIR`, encrypt the backups, and test restores.

## Operations

- **Upgrade:** pull the new revision, rebuild `api web` and the computer images, run migrations on startup, then recreate `api worker web caddy`.
- **Backup:** use `backup.sh` or your own PostgreSQL dump plus a copy of `SUPERBOT_DATA_DIR`.
- **Soak test:** `python3 deploy/soak.py` with `SUPERBOT_SOAK_BASE` and `SUPERBOT_SOAK_HOURS`.
- **Integration tests:** `bash deploy/run-integration-tests.sh` on the server.

See [`docs/configuration-required.md`](../docs/configuration-required.md) for capabilities that need external credentials, network access or platform support.

## Bot computers

Each bot can have a computer: a container from `superbot/computer:latest` (browser) or `superbot/computer-desktop:latest` (XFCE desktop plus browser). The bot has no root on it.

- **Home and installs persist.** `HOME` is `/workspace/.home` in the bot's workspace. Settings, `pip install` (falls back to `--user`), `npm install -g`, AppImages (`appimage-install`) and Debian packages (`userapt install PKG`, unpacked under `~/.local/apt`, programs with built-in data paths run through `proot`) survive sleep, restarts and rebuilds. Caches stay on the container's tmpfs.
- **Seeing the screen.** The desktop tools return the windows and every piece of text on screen with click coordinates (tesseract OCR, Chinese and English), so text-only models can operate the desktop; models marked as vision-capable also get the screenshot.
- **Background jobs.** `shell` with `background=true` starts a job (`shell_jobs` shows and stops it). A computer running a job is not put to sleep or evicted; a job runs at most 12 hours.
- **Gate.** Chromium's DevTools and noVNC listen on loopback inside the computer; `gate.py` (ports 9223 and 6080) forwards a connection only when its first request carries the computer's secret, and relays one request per plain HTTP connection.
- **Windows programs** are not supported in the computer (Wine would add 1–1.7 GB to the image and over 1 GB per bot); bots use the user's paired PC (`local_exec`) instead.

### Upgrading to these images

Build the browser image first; the desktop image is built on it:

```bash
docker build -t superbot/computer:latest -f deploy/images/computer/Dockerfile deploy/images/computer
docker build -t superbot/computer-desktop:latest -f deploy/images/computer/Dockerfile.desktop deploy/images/computer
```

The worker labels computers with a configuration version. Each existing computer is recreated from the new image the next time it starts or is used (the workspace, browser profile and files stay); running computers keep running until then. Rebuild the computer images *before* recreating the worker, or a computer recreated in between gets the old image.

### Host firewall

Computers share `superbot_sandbox` with the API and the worker. Without host rules a computer can open connections to the API's port, to other computers, to the host (SSH) and to private or cloud-metadata addresses. As root on the Docker host:

```bash
# 1. Same-network traffic is only filtered with bridge netfilter on.
#    (Check: sysctl net.bridge.bridge-nf-call-iptables; Docker loads it only
#    for some network settings.)
modprobe br_netfilter
sysctl -w net.bridge.bridge-nf-call-iptables=1 net.bridge.bridge-nf-call-ip6tables=1
echo br_netfilter > /etc/modules-load.d/superbot.conf
printf 'net.bridge.bridge-nf-call-iptables = 1\nnet.bridge.bridge-nf-call-ip6tables = 1\n' > /etc/sysctl.d/90-superbot-bridge.conf

# 2. Apply the rules (idempotent) and keep them across reboots.
bash deploy/sandbox-firewall.sh apply
cp deploy/systemd/superbot-sandbox-firewall.service /etc/systemd/system/
systemctl daemon-reload && systemctl enable superbot-sandbox-firewall

# Inspect or undo:
bash deploy/sandbox-firewall.sh status
bash deploy/sandbox-firewall.sh remove
```

Once applied, `deploy.sh` refreshes the rules on every deploy. Verify afterwards that a bot's browser tools and the live view still work (the API and worker reach computers on the gate ports, which stay open).
