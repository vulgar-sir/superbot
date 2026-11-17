# 让 AI 帮你部署 superbot

这份文档写给两类人：

- 想把 superbot 部署到自己的服务器，但不想逐条查文档、敲命令的用户；
- 正在使用 Claude Code、Codex、Cursor 或其他 AI Agent 的部署者。

做法很简单：把**项目链接**、**服务器连接方式**和下面的**提示词**交给 AI；让 AI 先读仓库、做只读预检、给出计划，得到你确认后再执行。

> **先读这一段：** 不要把 root 密码、LLM API Key 直接贴在公开的 AI 聊天窗口里。优先在本地运行 AI Agent（Claude Code / Codex CLI / Cursor），通过 SSH key 连接服务器；给 AI 一个专用的、可撤销的部署账号。部署完成后撤销临时密钥。

> English version: [deploy-with-ai.md](deploy-with-ai.md)

## 1. 开始之前

请先准备：

- **一台 Linux 服务器**：Ubuntu 24.04 或 Debian 13；起步 8 vCPU / 16 GB 内存 / 200 GB SSD。要同时跑多个浏览器/桌面 Bot，建议 16 vCPU / 32 GB。
- **Docker Engine + Compose v2**。没有安装也没关系，AI 可以帮你装，但你要确认它使用的安装来源。
- **一个 LLM Provider 的 API Key**（DeepSeek、任意 OpenAI 兼容服务、Anthropic，或本机 Ollama）。
- **SSH 访问方式**：主机、端口、用户名、密钥路径。推荐创建专用用户 `deploy`，需要 sudo 时再提权。
- **域名和 TLS（可选）**：默认部署是 `http://服务器:32608`；WebAuthn/Passkey 和生产使用建议配域名 + HTTPS。
- **服务器快照或备份**：在让 AI 改任何东西之前先做。

## 2. 填一张「连接卡」

把下面这张卡填好，连同提示词一起发给 AI。**不要在卡里写真实密码或 API Key**；用路径、密码管理器引用或一次性密钥代替。

```text
=== superbot 部署连接卡 ===
项目: https://github.com/vulgar-sir/superbot
服务器: <IP 或域名>
SSH 端口: <通常是 22>
SSH 用户: <例如 deploy>
SSH 认证: <密钥路径，例如 ~/.ssh/superbot_deploy；或“使用 SSH agent”>
系统: <Ubuntu 24.04 / Debian 13 / 其他>
配置: <CPU 核数 / 内存 / 磁盘>
域名: <example.com；没有就写“无”>
TLS: <有 / 无>
LLM Provider: <DeepSeek / OpenAI 兼容 / Anthropic / Ollama>
LLM 模型: <模型 ID>
LLM Key 位置: <服务器上的文件路径，或“部署完成后我在网页里配置”>
管理员邮箱: <email>
管理员密码: <由 AI 生成并只显示一次 / 放在密码管理器>
数据目录: /srv/superbot/data
是否允许 AI 执行 sudo: <是，逐条说明原因 / 否，只给我命令>
=== 结束 ===
```

建议：

- 如果 AI 在本地运行（Claude Code 等），它可以读取你指定的密钥文件；不要在对话里粘贴密钥内容。
- 如果 AI 在网页里运行，不要给它 SSH 密码。用一次性 SSH key，部署完成后立刻从 `~/.ssh/authorized_keys` 删除。
- 管理员密码可以让 AI 在服务器上生成随机值，只显示一次给你保存；不要在聊天里反复回显。

## 3. 短版提示词（复制即用）

```text
你是一名经验丰富的 SRE / DevOps Agent。请帮我在我的服务器上部署 superbot。

项目: https://github.com/vulgar-sir/superbot
连接卡:
<把第 2 节的连接卡贴在这里>

要求:
1. 先阅读仓库的 README.md、deploy/ 和 docs/configuration-required.md，再动手。
2. 先做只读预检，输出环境报告和部署计划；等我确认后再修改服务器。
3. 严格按仓库文档执行，不要发明步骤；任何一步失败就停下来报告，不要“猜着修”。
4. 使用非 root 部署用户；需要 sudo 时逐条说明原因。
5. 永远不要在聊天、日志或命令行参数里回显密钥；.env 权限设为 600。
6. 未经我明确同意，不要执行破坏性命令、不要改防火墙/DNS/端口映射、不要开放新端口。
7. 部署完成后按验收清单验证，并给我一份交付报告。

现在开始：先做只读预检，然后给我计划。
```

## 4. 完整版提示词（推荐）

```text
你是一名经验丰富的 SRE / DevOps Agent，负责在一台 Linux 服务器上部署 superbot。

【项目】
https://github.com/vulgar-sir/superbot

【连接卡】
<把第 2 节的连接卡贴在这里>

【你的工作方式】
1. 先读文档，再动手：
   - README.md
   - deploy/docker-compose.yml
   - deploy/.env.production.example
   - deploy/Caddyfile
   - deploy/harden-data-permissions.sh
   - docs/configuration-required.md
   - docs/deploy-with-ai.zh-CN.md（本文件，里面有完整步骤）
2. 先做只读预检，输出：
   - 系统、CPU、内存、磁盘
   - Docker / Compose 版本
   - 80 / 443 / 32608 端口占用
   - 是否能访问 GitHub、Docker Hub / GHCR
   - 现有 /srv/superbot 是否存在（存在就检查，不要删除）
3. 给出分阶段计划：预检 → 安装依赖 → 拉代码 → 配置 .env → 构建 → 权限 → 启动 → 验收 → 可选 HTTPS。
4. 等我确认计划后再执行。执行时一步一步来，每完成一个阶段汇报结果。
5. 遇到以下情况必须停下来问我：
   - 需要删除/覆盖现有文件或数据
   - 需要改防火墙、DNS、端口映射
   - 需要 root 权限且原因不明确
   - 构建失败、健康检查失败、登录失败
   - 需要我提供密钥或密码

【安全红线】
- 不要把密钥、密码、Token 写进命令行参数、日志或聊天；用 .env 文件，权限 600。
- 不要执行 rm -rf、docker system prune -a、git reset --hard，除非我明确同意。
- 不要把 Docker socket 暴露给 worker；不要使用 --privileged。
- 不要关闭 TLS 校验；不要把 2375 端口暴露到公网。
- 不要在未设置强管理员密码前把服务暴露到公网。
- 如果某个“修复”会降低安全性，先停下来解释，不要直接做。

【部署步骤】
按本文件的第 5 节执行。完成后按第 6 节验收，并按第 9 节给我交付报告。

现在开始：先做只读预检，然后给我计划。
```

## 5. AI 应该执行的部署步骤

下面这些命令是给 AI 对照用的，避免它自由发挥。默认路径 `/srv/superbot`，默认数据目录 `/srv/superbot/data`。

### 5.1 只读预检

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

如果 Docker 未安装，AI 应先说明安装来源（推荐 Docker 官方文档），得到确认后再装。

### 5.2 准备目录并拉代码

```bash
sudo mkdir -p /srv/superbot
sudo chown "$USER":"$USER" /srv/superbot
git clone https://github.com/vulgar-sir/superbot.git /srv/superbot
cd /srv/superbot
git rev-parse --short HEAD
```

如果 `/srv/superbot` 已存在，先检查内容并询问，不要覆盖或删除。

### 5.3 生成 `.env`

```bash
cd /srv/superbot
cp deploy/.env.production.example .env
chmod 600 .env
openssl rand -base64 32   # 生成 SUPERBOT_SECRET_KEY
```

需要设置：

- `SUPERBOT_SECRET_KEY`：32 字节 base64，丢失后已加密的 Provider/MCP 密钥不可恢复。
- `POSTGRES_PASSWORD`，并同步 `SUPERBOT_DATABASE_URL` 里的密码。
- `SUPERBOT_ADMIN_EMAIL`、`SUPERBOT_ADMIN_PASSWORD`。
- `SUPERBOT_DEEPSEEK_API_KEY`（可选；也可以部署完成后在网页里配置 Provider）。
- `SUPERBOT_DATA_DIR=/srv/superbot/data`。
- 纯 HTTP 部署：`SUPERBOT_COOKIE_INSECURE=true`、`SUPERBOT_ALLOW_INSECURE_COOKIE=true`。
- HTTPS 部署：以上两项改为 `false`。

写 `.env` 时不要让密钥出现在命令历史或 AI 输出里。优先让 AI 用编辑器写文件，或由你在服务器上手动填入。

### 5.4 构建镜像

```bash
cd /srv/superbot
docker compose --env-file .env -f deploy/docker-compose.yml build api web
docker build -t superbot/computer:latest \
  -f deploy/images/computer/Dockerfile deploy/images/computer
docker build -t superbot/computer-desktop:latest \
  -f deploy/images/computer/Dockerfile.desktop deploy/images/computer
```

构建可能较慢。若 Docker Hub 不可达，先配置镜像加速或使用可用的镜像源；不要让 AI 盲目重试几十次。

### 5.5 数据目录权限

```bash
cd /srv/superbot
sudo mkdir -p /srv/superbot/data
sudo bash deploy/harden-data-permissions.sh /srv/superbot/data
```

这个脚本是幂等的；它会拒绝在沙箱挂载源发现符号链接，并把父目录设为 root 所有、叶子目录交给 uid 1000。

### 5.6 启动核心服务

```bash
cd /srv/superbot
docker compose --env-file .env -f deploy/docker-compose.yml \
  up -d postgres api worker docker-proxy web caddy
```

可选服务（先不要启动，确认核心可用后再按需开启）：

- `dex`：自托管 OIDC/SSO，需要先准备 `dex/config.yaml`。
- `embedding`：自托管 embedding，首次启动下载模型。
- `stt` / `tts`：自托管语音服务。

### 5.7 健康检查与日志

```bash
docker compose --env-file .env -f deploy/docker-compose.yml ps
curl -fsS http://127.0.0.1:32608/healthz
curl -fsS -o /dev/null -w '%{http_code}\n' http://127.0.0.1:32608/
docker compose --env-file .env -f deploy/docker-compose.yml logs --tail=100 api worker caddy
```

期望：`/healthz` 返回 200，首页返回 200，`postgres/api/worker/docker-proxy/web/caddy` 都在运行。

### 5.8 登录与真实模型验证

用浏览器打开 `http://<服务器>:32608`，用管理员账号登录。然后：

1. 设置 → Provider → 新增/确认 Provider → 测试。
2. 创建一个 Bot，发一句：「请只回复 OK」。
3. 确认 Bot 正常回复、右侧面板有 token/成本记录。
4. 如需电脑：确认 `superbot/computer:latest` 镜像存在，启动一个 Bot 的浏览器并打开页面。

如果 AI 有浏览器能力，可以自己做；否则把 URL 和账号交给用户，让用户完成这一步，AI 等待结果。

### 5.9 可选：域名 + HTTPS

默认 Compose 把 Caddy 的 80 映射到宿主机 `32608`，所以默认是 HTTP。要上域名 + HTTPS：

1. 域名 A/AAAA 指向服务器；放通 80/443。
2. 决定方案：改 Compose 端口映射 + Caddyfile 站点地址，或在前面加一层反向代理。
3. 将 `SUPERBOT_COOKIE_INSECURE` 和 `SUPERBOT_ALLOW_INSECURE_COOKIE` 设为 `false`。
4. 配置 `SUPERBOT_WEBAUTHN_RPID` 和 `SUPERBOT_WEBAUTHN_ORIGINS`。
5. 重建 caddy/api/worker，验证 HTTPS、登录和 Passkey。

这是**需要用户明确同意的改动**。AI 不应在未确认的情况下改端口映射、防火墙或 DNS。

### 5.10 交付报告

AI 完成后，按第 9 节的模板给用户一份报告。

## 6. 验收清单

- [ ] `docker compose ps`：postgres、api、worker、docker-proxy、web、caddy 均为 running/healthy。
- [ ] `curl -fsS http://127.0.0.1:32608/healthz` 返回 200。
- [ ] 首页返回 200。
- [ ] 管理员登录成功。
- [ ] Provider 测试通过。
- [ ] Bot 能正常回复；右侧面板有 token/成本。
- [ ] 如需电脑：`superbot/computer:latest` 存在，Bot 浏览器能打开页面。
- [ ] `.env` 权限为 600，且没有被 git 跟踪。
- [ ] `docker ps` 没有意外发布的端口；Docker socket 没有暴露给 worker。
- [ ] `docker compose logs` 没有明文密钥。
- [ ] 部署前已做服务器快照/备份。
- [ ] 用户知道如何备份、升级和查看日志。

## 7. 安全红线（给 AI）

- 不要执行 `rm -rf`、`docker system prune -a`、`git reset --hard`，除非用户明确同意。
- 不要修改防火墙、DNS、端口映射，除非用户明确同意。
- 不要关闭 TLS 校验，不要使用 `--privileged`，不要把 Docker socket 挂进 Worker。
- 不要把 2375 端口暴露到公网。
- 不要把密钥写进命令行参数（会进入 shell history）。
- 不要在未设置强管理员密码前把服务暴露到公网。
- 不要为了“让服务跑起来”而降低安全配置；先停下来解释。
- 如果用户给的是 root 密码，建议改用专用用户 + sudo + SSH key，并提醒用户轮换。

## 8. 常见问题

| 现象 | 先检查什么 |
|---|---|
| `permission denied` 连 Docker | 用户是否在 `docker` 组；重新登录；不要直接把 socket 权限改成 777 |
| 32608 端口被占用 | `ss -ltnp` 看是谁；换端口或停掉冲突服务；先问用户 |
| 构建卡住 / 拉镜像失败 | Docker Hub / GHCR 网络；配置镜像加速；不要盲目重试 |
| Caddy 502 | api/web 是否健康；看 `docker compose logs caddy api web` |
| `/healthz` 不通 | api 是否启动；数据库是否健康；看 api 日志 |
| 登录失败 | 管理员密码；数据库卷是否被旧数据占用；首次启动日志 |
| Bot 不回复 / 401 | Provider Key、Base URL、模型 ID；设置页测试 |
| 电脑启动失败 | `superbot/computer:latest` 是否存在；`docker-proxy` 是否正常；网络 |
| 磁盘满了 | `df -h`；先问用户再清理；不要直接 `prune -a` |
| GitHub 访问不了 | 使用代理或镜像；不要把凭据写进公开配置 |

## 9. 给 AI 的交付报告模板

```text
superbot 部署报告

1. 访问地址: <URL>
2. 代码版本: <commit>
3. 服务器: <OS / CPU / RAM / 磁盘>
4. 运行中的服务: <docker compose ps 摘要>
5. 验收结果:
   - /healthz: <状态码>
   - 首页: <状态码>
   - 管理员登录: <通过/未完成>
   - Provider 测试: <通过/未完成>
   - Bot 回复: <通过/未完成>
   - 电脑镜像: <存在/未构建>
6. 手动步骤: <需要用户自己完成的事>
7. 密钥位置: <只写路径，不写值；例如 /srv/superbot/.env>
8. 已知风险/限制: <HTTP、未配 HTTPS、未配 Passkey 等>
9. 下一步建议: <备份、域名、监控、可选服务>
10. 回滚方式: <快照/备份位置>
```

## 10. 一句话版本

把下面这句话和连接卡发给 AI，它会读这份文档并开始：

```text
请阅读 https://github.com/vulgar-sir/superbot/blob/main/docs/deploy-with-ai.zh-CN.md
并按其中的完整提示词帮我部署。我的连接卡如下：<粘贴连接卡>
```

如果 AI 说它访问不了链接，让它先 `git clone` 项目，再读本地的 `docs/deploy-with-ai.zh-CN.md`。
