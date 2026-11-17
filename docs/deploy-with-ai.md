# Deploy superbot with an AI assistant

This guide is for two kinds of people:

- Users who want to deploy superbot on their own server without working through every command manually.
- People using Claude Code, Codex, Cursor or another AI agent who want to hand the deployment to it.

The idea is simple: give the AI the **project link**, your **server connection details** and the **prompt below**. Ask it to read the repository, run read-only preflight checks and present a plan first; it should only change the server after you approve the plan.

> **Read this first:** do not paste a root password or an LLM API key into a public AI chat window. Prefer running a local agent (Claude Code, Codex CLI, Cursor) that can SSH to the server with a dedicated, revocable deploy key. Revoke the key when the deployment is done.

> 简体中文：[deploy-with-ai.zh-CN.md](deploy-with-ai.zh-CN.md)

## 1. Before you start

Prepare:

- **A Linux server**: Ubuntu 24.04 or Debian 13; 8 vCPU / 16 GB RAM / 200 GB SSD to start. 16 vCPU / 32 GB is recommended for several browser or desktop bots.
- **Docker Engine + Compose v2**. The AI can install it, but you should confirm the source it uses.
- **An LLM provider API key** (DeepSeek, any OpenAI-compatible endpoint, Anthropic, or local Ollama).
- **SSH access**: host, port, user and key path. Prefer a dedicated `deploy` user with sudo only where needed.
- **A domain and TLS (optional)**: the default deployment is `http://server:32608`; production use and WebAuthn/Passkeys want a domain with HTTPS.
- **A server snapshot or backup** before the AI changes anything.

## 2. Fill in a connection card

Fill this in and send it with the prompt. **Do not put real passwords or API keys in the card**; use paths, password-manager references or one-time secrets.

```text
=== superbot deployment card ===
Project: https://github.com/vulgar-sir/superbot
Server: <IP or hostname>
SSH port: <usually 22>
SSH user: <e.g. deploy>
SSH auth: <key path, e.g. ~/.ssh/superbot_deploy; or "use SSH agent">
OS: <Ubuntu 24.04 / Debian 13 / other>
Resources: <CPU cores / RAM / disk>
Domain: <example.com, or "none">
TLS: <yes / no>
LLM provider: <DeepSeek / OpenAI-compatible / Anthropic / Ollama>
LLM model: <model ID>
LLM key location: <path on the server, or "I will configure it in the UI after deployment">
Admin email: <email>
Admin password: <generate one and show it once / stored in my password manager>
Data directory: /srv/superbot/data
May the AI run sudo: <yes, explain each command / no, give me the commands>
=== end ===
```

Tips:

- If the AI runs locally (Claude Code, etc.), it can read the key file you point it to. Do not paste the key into the conversation.
- If the AI runs in a web chat, do not give it an SSH password. Use a temporary SSH key and remove it from `~/.ssh/authorized_keys` after the deployment.
- Let the AI generate the admin password on the server and show it to you once; do not echo it repeatedly.

## 3. Short prompt (copy and use)

```text
You are an experienced SRE / DevOps agent. Deploy superbot on my server.

Project: https://github.com/vulgar-sir/superbot
Connection card:
<paste the card from section 2 here>

Requirements:
1. Read README.md, deploy/ and docs/configuration-required.md before doing anything.
2. Run read-only preflight checks first, then give me an environment report and a deployment plan. Wait for my approval before changing the server.
3. Follow the repository documentation; do not invent steps. If a step fails, stop and report instead of guessing.
4. Use a non-root deploy user. Explain every sudo command before running it.
5. Never echo secrets in chat, logs or command-line arguments. Set .env to mode 600.
6. Do not run destructive commands, change firewall/DNS/port mappings or open new ports without my explicit approval.
7. After deployment, verify against the acceptance checklist and give me a hand-off report.

Start now: run the read-only preflight and show me the plan.
```

## 4. Full prompt (recommended)

```text
You are an experienced SRE / DevOps agent deploying superbot on a Linux server.

[Project]
https://github.com/vulgar-sir/superbot

[Connection card]
<paste the card from section 2 here>

[How to work]
1. Read the documentation before changing anything:
   - README.md
   - deploy/docker-compose.yml
   - deploy/.env.production.example
   - deploy/Caddyfile
   - deploy/harden-data-permissions.sh
   - docs/configuration-required.md
   - docs/deploy-with-ai.md (this file; it contains the full runbook)
2. Run read-only preflight checks and report:
   - OS, CPU, RAM and disk
   - Docker / Compose versions
   - Processes listening on 80, 443 and 32608
   - Whether GitHub, Docker Hub and GHCR are reachable
   - Whether /srv/superbot already exists (inspect it; do not delete it)
3. Present a staged plan: preflight → dependencies → clone → .env → build → permissions → start → verify → optional HTTPS.
4. Wait for my approval, then execute one stage at a time and report after each stage.
5. Stop and ask me when:
   - an existing file or data set would be deleted or overwritten;
   - firewall, DNS or port mappings would change;
   - root access is needed and the reason is unclear;
   - a build, health check or login fails;
   - you need a secret from me.

[Security rules]
- Never put keys, passwords or tokens in command-line arguments, logs or chat. Use an .env file with mode 600.
- Do not run rm -rf, docker system prune -a or git reset --hard without explicit approval.
- Never give the worker the Docker socket; never use --privileged.
- Do not disable TLS verification; do not expose port 2375 to the internet.
- Do not expose the service publicly before a strong admin password is set.
- If a "fix" would weaken security, stop and explain instead of applying it.

[Deployment steps]
Follow section 5 of this document. Verify with section 6 and hand off with section 9.

Start now: run the read-only preflight and show me the plan.
```

## 5. Deployment steps the AI should follow

These commands are the reference runbook, so the AI does not have to improvise. Default path: `/srv/superbot`; default data directory: `/srv/superbot/data`.

### 5.1 Read-only preflight

```bash
uname -a
cat /etc/os-release
nproc
free -h
df -h /
docker --version || true
docker compose version || true
ss -ltnp | grep -E ':(80|443|32608)\b' || true
curl -fsS -m 10 https://api.github.com/ >/dev/null && echo "github ok" || echo "github blocked"
```

If Docker is missing, the AI should explain the installation source (the official Docker documentation is preferred) and wait for approval.

### 5.2 Prepare the directory and clone

```bash
sudo mkdir -p /srv/superbot
sudo chown "$USER":"$USER" /srv/superbot
git clone https://github.com/vulgar-sir/superbot.git /srv/superbot
cd /srv/superbot
git rev-parse --short HEAD
```

If `/srv/superbot` already exists, inspect it and ask before overwriting or deleting anything.

### 5.3 Create `.env`

```bash
cd /srv/superbot
cp deploy/.env.production.example .env
chmod 600 .env
openssl rand -base64 32   # generates SUPERBOT_SECRET_KEY
```

Set:

- `SUPERBOT_SECRET_KEY`: 32 random bytes, base64. Losing it makes existing encrypted provider/MCP secrets unrecoverable.
- `POSTGRES_PASSWORD`, and keep the password in `SUPERBOT_DATABASE_URL` in sync.
- `SUPERBOT_ADMIN_EMAIL`, `SUPERBOT_ADMIN_PASSWORD`.
- `SUPERBOT_DEEPSEEK_API_KEY` (optional; you can also configure a provider in the UI after deployment).
- `SUPERBOT_DATA_DIR=/srv/superbot/data`.
- Plain HTTP: `SUPERBOT_COOKIE_INSECURE=true`, `SUPERBOT_ALLOW_INSECURE_COOKIE=true`.
- HTTPS: set both to `false`.

Do not let secrets appear in shell history or AI output. Prefer an editor on the server, or have the user fill in the file.

### 5.4 Build images

```bash
cd /srv/superbot
docker compose --env-file .env -f deploy/docker-compose.yml build api web
docker build -t superbot/computer:latest \
  -f deploy/images/computer/Dockerfile deploy/images/computer
docker build -t superbot/computer-desktop:latest \
  -f deploy/images/computer/Dockerfile.desktop deploy/images/computer
```

Builds can take a while. If Docker Hub is unreachable, configure a mirror or use a reachable registry; do not retry blindly for a long time.

### 5.5 Data directory permissions

```bash
cd /srv/superbot
sudo mkdir -p /srv/superbot/data
sudo bash deploy/harden-data-permissions.sh /srv/superbot/data
```

The script is idempotent. It refuses symlinks where sandbox mounts come from, makes the parent directories root-owned, and gives leaf directories to uid 1000.

### 5.6 Start the core services

```bash
cd /srv/superbot
docker compose --env-file .env -f deploy/docker-compose.yml \
  up -d postgres api worker docker-proxy web caddy
```

Optional services (leave them off until the core works):

- `dex`: self-hosted OIDC/SSO; requires `dex/config.yaml` first.
- `embedding`: self-hosted embeddings; the first start downloads a model.
- `stt` / `tts`: self-hosted voice services.

### 5.7 Health checks and logs

```bash
docker compose --env-file .env -f deploy/docker-compose.yml ps
curl -fsS http://127.0.0.1:32608/healthz
curl -fsS -o /dev/null -w '%{http_code}\n' http://127.0.0.1:32608/
docker compose --env-file .env -f deploy/docker-compose.yml logs --tail=100 api worker caddy
```

Expected: `/healthz` returns 200, the front page returns 200, and `postgres/api/worker/docker-proxy/web/caddy` are running.

### 5.8 Log in and verify a real model

Open `http://<server>:32608` in a browser and log in with the admin account. Then:

1. Settings → Providers → add or confirm a provider → Test.
2. Create a bot and send: "Reply with OK only."
3. Confirm the bot replies and the right panel records tokens/cost.
4. If computers are needed: confirm `superbot/computer:latest` exists, start a bot's browser and open a page.

If the AI has browser access it can do this; otherwise hand the URL and credentials to the user and wait for the result.

### 5.9 Optional: domain and HTTPS

The default Compose file maps Caddy's port 80 to host port `32608`, so the default is HTTP. To add a domain and HTTPS:

1. Point the domain's A/AAAA record at the server; open 80/443.
2. Choose an approach: change the Compose port mapping and the Caddyfile site address, or put an external reverse proxy in front.
3. Set `SUPERBOT_COOKIE_INSECURE` and `SUPERBOT_ALLOW_INSECURE_COOKIE` to `false`.
4. Set `SUPERBOT_WEBAUTHN_RPID` and `SUPERBOT_WEBAUTHN_ORIGINS`.
5. Recreate caddy/api/worker and verify HTTPS, login and Passkeys.

This change **requires explicit user approval**. The AI must not change port mappings, firewall rules or DNS on its own.

### 5.10 Hand-off report

Use the template in section 9.

## 6. Acceptance checklist

- [ ] `docker compose ps`: postgres, api, worker, docker-proxy, web and caddy are running/healthy.
- [ ] `curl -fsS http://127.0.0.1:32608/healthz` returns 200.
- [ ] The front page returns 200.
- [ ] Admin login succeeds.
- [ ] Provider test passes.
- [ ] A bot replies and the right panel records tokens/cost.
- [ ] If computers are needed: `superbot/computer:latest` exists and a bot browser opens a page.
- [ ] `.env` is mode 600 and is not tracked by git.
- [ ] `docker ps` shows no unexpected published ports; the Docker socket is not exposed to the worker.
- [ ] `docker compose logs` contains no plaintext secrets.
- [ ] A server snapshot/backup was taken before deployment.
- [ ] The user knows how to back up, upgrade and read logs.

## 7. Security rules for the AI

- Do not run `rm -rf`, `docker system prune -a` or `git reset --hard` without explicit approval.
- Do not change firewall rules, DNS or port mappings without explicit approval.
- Do not disable TLS verification, use `--privileged`, or mount the Docker socket into the worker.
- Do not expose port 2375 to the internet.
- Do not put secrets in command-line arguments (they end up in shell history).
- Do not expose the service publicly before a strong admin password is set.
- Do not weaken security to "make it work"; stop and explain first.
- If the user gives a root password, suggest a dedicated user + sudo + SSH key, and remind them to rotate the password.

## 8. Troubleshooting

| Symptom | Check first |
|---|---|
| `permission denied` talking to Docker | Is the user in the `docker` group? Re-login. Do not chmod the socket to 777. |
| Port 32608 is in use | `ss -ltnp` to see what owns it; change the mapping or stop the conflicting service; ask the user first. |
| Build hangs / image pull fails | Docker Hub / GHCR connectivity; configure a mirror; do not retry endlessly. |
| Caddy returns 502 | Are api/web healthy? Read `docker compose logs caddy api web`. |
| `/healthz` fails | Is api running? Is the database healthy? Read the api logs. |
| Login fails | Admin password; an old database volume; first-boot logs. |
| Bot does not reply / 401 | Provider key, base URL, model ID; use the provider test in Settings. |
| Computer fails to start | Does `superbot/computer:latest` exist? Is `docker-proxy` healthy? Network? |
| Disk is full | `df -h`; ask the user before cleaning; do not run `prune -a` blindly. |
| GitHub is unreachable | Use a proxy or mirror; never put credentials in public configuration. |

## 9. Hand-off report template

```text
superbot deployment report

1. URL: <URL>
2. Revision: <commit>
3. Server: <OS / CPU / RAM / disk>
4. Running services: <summary of docker compose ps>
5. Verification:
   - /healthz: <status>
   - front page: <status>
   - admin login: <pass / not done>
   - provider test: <pass / not done>
   - bot reply: <pass / not done>
   - computer image: <exists / not built>
6. Manual steps: <what the user still needs to do>
7. Secret location: <path only, never the value; e.g. /srv/superbot/.env>
8. Known risks/limits: <HTTP, no HTTPS, no Passkey, etc.>
9. Next steps: <backup, domain, monitoring, optional services>
10. Rollback: <snapshot/backup location>
```

## 10. One-line version

Send this and the connection card to the AI; it will read this document and start:

```text
Read https://github.com/vulgar-sir/superbot/blob/main/docs/deploy-with-ai.md
and deploy superbot using the full prompt in it. My connection card is: <paste card>
```

If the AI cannot reach the link, ask it to `git clone` the repository first and read the local `docs/deploy-with-ai.md`.
