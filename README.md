# superbot

自托管的多 Agent 协作平台：在你的服务器上创建可配置的 Bot，把它们组成团队，在同一个房间里对话、互相 @、共享工作区，并让它们使用 shell / Python / 浏览器 / MCP / Skill 完成任务。

## 功能

- **Bot**：人设、独立模型、工具策略、私有工作区、长期记忆、浏览器/云电脑；账号级「主 Bot」。
- **Team**：圆桌、流水线、Router-Worker 和 group（Grok 风格群聊：最多 6 人、@ 驱动、主 Bot fallback、无中心协调者）四种编排。
- **Room**：人类 + 多个 Bot 同屏；流式输出、@ 提及、停止、成本可见、工具调用时间线；跨 Bot 私信折叠卡片。
- **动态**：Bot 回复、例程、队友私信写入 Activity；侧栏未读点、全局搜索、`/activity` 动态页。
- **Room / Bot 私信**：`send_to_agent` 让 Bot 互相发私信，主聊天只显示折叠摘要。
- **例程与 Library**：Bot 级 cron / `@every` / 一次性例程、暂停、到点进 DM 自动开工；页面/文档/文件/媒体资料库。
- **扩展**：内置技能 + 用户 Skill + Bot 自提议；HTTP MCP；Bot 模板导出/导入；记忆导入。
- **电脑**：每个 Bot 一个持久 Chromium/XFCE 会话；noVNC 实时接管；更新（保留文件）/重置（清空工作区）。
- **本地设备**：`superbot device` 守护进程轮询，用户显式允许后 Bot 可 `local_exec` 在本机执行 shell。
- **Vault**：登录信息/支付/联系信息 AES-256-GCM 加密，列表不回显，显式 reveal。
- **用量**：按 Bot / 模型 / 日期统计 token 与人民币成本；语音输入/通话；深色/浅色主题。
- **Grok Bot 风格 UI**：240px 分组侧栏、居中身份胶囊、18px 消息气泡、320px 可拖拽详情面板、应用内设置弹窗；右键菜单、侧栏拖拽排序、@ 成员自动补全、乐观发送与快捷键。
- **连接器**：GitHub 设备码授权、Slack/Notion/1Password/Email Token、连接测试；GitHub/Slack/Notion 工具。
- **团队能力**：多账号与角色隔离、WebAuthn/Passkey 安全密钥（需 HTTPS 域名）、邮件 SMTP、日程 ICS、自托管 embedding 语义召回、自托管 STT/TTS。
- **多模型**：OpenAI 兼容（DeepSeek、Qwen、Moonshot、GLM、火山方舟、Ollama 等）与 Anthropic Messages API；请求阶段自动重试（429/5xx，遵守 Retry-After），流空闲超时检测。
- **可观测**：所有消息、工具调用、token 与成本写入事件流，可回放、可审计。
- **长任务可靠性**：对话过长时自动整理成「检查点」（结构化笔记 + 用户原话逐字保留），原文可用 `search_chat_history` 回查；工具循环内按预算压缩；worker 重启后从上一个工具轮次续跑而不是重放；不可重试的错误直接失败。
- **随时插话**：Bot 干活时发的消息会在它下一步模型调用前送达；赶不上的自动补一轮。`/side` 旁路提问不打断任务、不写入对话。
- **协作闭环**：`send_to_agent` 的回复和子代理结果会自动送回发起方并唤醒它，带接力与唤醒上限；审批暂停后流水线/圆桌/群聊会把剩余阶段跑完。
- **群目标**：群聊可设目标，群主 Bot 与你对齐验收标准、拆阶段分派、凭证据逐段验收，空闲时自驱但会自己停下。
- **记忆分层**：召回有相关度阈值，写入去重，疑似密钥拒绝入库，对话空闲后自动回顾提炼，每日整理重复且可恢复，可置顶/归档。

## 架构

```
浏览器 ── HTTPS/SSE/WebSocket ── Caddy
                                  ├── /api/* → Go API ── PostgreSQL 18 + pgvector
                                  └── /*     → Next.js
Go Worker ── PostgreSQL（SKIP LOCKED 队列 + LISTEN/NOTIFY）
   ├── Agent Loop（LLM / 工具 / 预算）
   ├── 编排（圆桌 / 流水线）
   ├── Skill / MCP / Memory
   └── Docker Sandbox（base / browser / desktop profile）
```

- Go 单二进制（`serve` / `worker` / `migrate`），Next.js 16 前端。
- Postgres 同时承担业务数据、任务队列和事件总线，首版不依赖 Redis。
- 每个 Bot 一个持久工作区容器；团队共享目录挂载到 `/teams/<team-id>/shared`。
- 浏览器容器内置 Chromium + Xvfb + x11vnc + noVNC；CDP 通过容器内 TCP 代理暴露给 Worker。

## 当前联调方式

当前以云服务器为唯一运行/测试环境，本地只做代码编辑和静态检查（`go test` / `npm run build`），不再启动本地 dev server。改完代码后：

1. 打包源码并上传到 `/srv/superbot`；
2. 在服务器执行 `bash deploy/deploy.sh`（或只构建变更的镜像）；
3. 直接在 `http://202.189.8.109:32608/` 和 `/api/*` 上验收。

## 本地开发（仅供参考）

前置：Go 1.27+、Node 24+、Docker Desktop。

```bash
cp .env.example .env            # 填入 SECRET_KEY、ADMIN_PASSWORD、DEEPSEEK_API_KEY
task dev-infra                 # 启动开发 Postgres（端口 55432）
task migrate                   # 数据库迁移
# 三个终端：
task api
task worker
task web                       # http://127.0.0.1:3000
```

首次启动会自动创建管理员，并在没有 Provider 时用 `SUPERBOT_DEEPSEEK_API_KEY` 种子化 DeepSeek。

## 生产部署（单机 Docker Compose）

服务器建议 8C16G / 200GB SSD，Ubuntu 24.04 或 Debian 13；同时跑多个浏览器/桌面 Bot 建议 16C32G。

```bash
# 源码放到 /srv/superbot，.env 放在同一目录
cd /srv/superbot
docker compose --env-file .env -f deploy/docker-compose.yml build
docker build -t superbot/computer:latest -f deploy/images/computer/Dockerfile deploy/images/computer
docker build -t superbot/computer-desktop:latest -f deploy/images/computer/Dockerfile.desktop deploy/images/computer
docker compose --env-file .env -f deploy/docker-compose.yml up -d
```

> 注意：`POSTGRES_PASSWORD` 等变量由 `--env-file .env` 提供；不要省略这个参数。

服务：

| 组件 | 作用 |
|---|---|
| `caddy` | 对外入口（默认 `32608:80`），`/api/*` 反代 Go API，SSE/WebSocket 已配置 |
| `web` | Next.js 前端 |
| `api` | REST + SSE |
| `worker` | Agent 运行时、编排、Docker 沙箱、定时任务 |
| `postgres` | 业务数据 + pgvector |

国内网络注意：

- Docker 镜像加速写在 `/etc/docker/daemon.json`，本仓库部署脚本使用 `docker.1ms.run` 等可用源。
- 基础镜像预拉后打本地标签，避免构建时依赖 Docker Hub 直连。
- `SUPERBOT_DEEPSEEK_API_KEY` 只放在 `.env`，不要提交。

`deploy/deploy.sh` 和 `deploy/backup.sh` 提供了构建/升级与备份的简写。

`deploy/run-integration-tests.sh` 在服务器上用一次性数据库 `superbot_test` 跑单元测试和引擎/记忆集成测试（真实 Postgres + 指令驱动的内置 mock 模型，不消耗 token，不碰生产数据）。

- **自托管 embedding**：compose 的 `embedding` 服务首次启动会从 `hf-mirror.com` 下载 `intfloat/multilingual-e5-large`（1024 维）并缓存在 `superbot_embedding_models` 卷；`.env` 配 `SUPERBOT_EMBEDDING_BASE_URL=http://embedding:8090`、`SUPERBOT_EMBEDDING_MODEL`、任意非空 `SUPERBOT_EMBEDDING_API_KEY`，重启 api/worker 后执行 `/app/superbot reembed-memory`。
- **自托管语音**：compose 的 `stt`（faster-whisper-small，中文）与 `tts`（Piper `zh_CN-huayan-medium`）分别缓存模型/音色；`.env` 配 `SUPERBOT_STT_URL=http://stt:8000/v1/audio/transcriptions`、`SUPERBOT_TTS_URL=http://tts:8091/v1/audio/speech` 及对应 model/voice/key，`/api/capabilities` 的 voice 变为 available。
- **自托管 OIDC**：compose `dex` 服务（GHCR 镜像）通过 Caddy `/dex` 提供 SSO；按 `deploy/dex/README.md` 生成 `/srv/superbot/dex/config.yaml` 并配置 `SUPERBOT_OIDC_*` 后，登录页出现「使用 SSO 登录」。
- **可靠性 soak**：`python3 deploy/soak.py` 每分钟检查 Web/API、记忆增删与会话 SSE，结果写 `/srv/superbot/soak.jsonl`；`SUPERBOT_SOAK_HOURS=72` 可跑完整 72h。

## 关键环境变量

| 变量 | 说明 |
|---|---|
| `SUPERBOT_SECRET_KEY` | 32 字节 base64，加密 Provider/MCP 密钥；丢失后密文不可恢复 |
| `SUPERBOT_DATABASE_URL` | Postgres DSN |
| `SUPERBOT_ADMIN_EMAIL` / `SUPERBOT_ADMIN_PASSWORD` | 首次启动的管理员账号 |
| `SUPERBOT_DATA_DIR` | 工作区、技能、附件目录（容器内与宿主机同路径） |
| `SUPERBOT_SANDBOX_NETWORK` | 生产环境设为 `superbot_sandbox`；本地留空使用回环端口 |
| `SUPERBOT_SANDBOX_IMAGE` | 默认 `superbot/computer:latest` |
| `SUPERBOT_ALLOW_INSECURE_COOKIE` | 纯 HTTP 部署时必须显式设为 `true` |

## 使用流程

1. 设置 → 新增 LLM Provider（DeepSeek/Qwen/…），模型目录里维护价格与能力。
2. 首页侧栏创建 Bot（只需名称和头像，自动选默认模型）；点「详情」编辑标题/描述、移动分组或置顶。
3. 侧栏「新建群聊」选择最多 6 个 Bot；在输入框用 `@BotName` 点名，被点名的 Bot 才发言，Bot 也可以 @ 队友形成链式协作，或 `send_to_agent` 私信队友。
4. 右侧「详情」查看 Bot 资料、资料库、记忆；右侧面板管理 Bot 例程；电脑面板支持更新/重置。
5. 团队页面仍可组建圆桌/流水线/Router 团队：成员顺序、模式、协调者模型、最大轮次。
6. 会话页发任务；右侧面板看运行、工具、成本；顶栏查看用量与动态。
7. 扩展页创建 Skill、导入/导出 Bot 模板、接入 HTTP MCP；内置技能自动可用。
8. 设置 → 电脑添加本地设备，按需允许 Bot 使用；设置 → 保险库保存登录信息；设置 → 账户安全修改密码。
9. 定时任务页用 cron / `@every 1h` 让团队自动开工；Bot 例程在 Bot 详情/右侧面板管理。
10. 设置 → 外观切换深色/浅色；输入框支持浏览器语音输入。

## 安全边界

- Provider/MCP 密钥 AES-256-GCM 加密；API 永不回显明文。
- 文件工具使用 `os.Root` 限定根目录；`fetch_url` 拦截私网/回环/云元数据。
- 沙箱：非特权、cap-drop ALL、资源限额、独立工作区；生产浏览器容器只加入专用 Docker 网络，不发布宿主机端口。
- MCP/Skill 内容按不可信输入处理；扩展变更写审计日志。
- 建议生产环境使用域名 + HTTPS，并把 `SUPERBOT_COOKIE_INSECURE` 关闭。

## 当前边界 / Roadmap

- 已实现：单管理员/多账号、主 Bot、侧栏分区、轻量 Bot、Bot 资料/记忆/资料库、DM 复用、群聊 @ 驱动、跨 Bot 私信、动态/未读/搜索、圆桌/流水线/Router-Worker、Bot 级例程（含 webhook / 邮件触发）、Library、浏览器与 XFCE 云电脑（快照/恢复/自动休眠）、本地设备 shell/截图/文件/应用、Vault 与 Bot 安全填表、内置技能、Bot 模板、记忆导入与用户/团队记忆、用量统计、语音输入/通话/语音消息（自托管 STT/TTS）、邮件/日程、连接器、WebAuthn 安全密钥、embedding 自动召回、MCP Apps/Widget、工具调用合并折叠、资料库自动收录（图片/媒体/文档/简单代码、聊天附件与新建文档）、中英文 i18n、深色/浅色主题、修改密码。
- **Electron 桌面端**：无边框窗口与 46×32 自绘窗口控制、托盘/原生菜单、桌面通知、本地设备代理、捕获 overlay、NSIS 安装包与 `/desktop/` 自动更新 feed（当前 0.1.2）。
- 完成度以 [`docs/functional-parity-matrix.md`](docs/functional-parity-matrix.md) 为唯一事实来源；需要外部凭据/网络/平台条件的项见 [`docs/configuration-required.md`](docs/configuration-required.md)，未满足时显示 `requires_config` 或平台限制，不伪造成功。
- 按需接入：GitHub 设备码/Token、Slack webhook、IMAP/SMTP、CalDAV、MCP stdio/HTTP（均需用户自己的凭据；外部 OAuth 应用凭据类按用户要求暂缓）。
- 首版不支持：移动端原生 App、Kubernetes、跨机分布式 Worker；需要第三方企业租户/付费账号的能力（Teams/Google/Outlook/会议平台/Twilio/X 等）按用户要求暂缓。

## 目录

```
cmd/superbot        入口（serve/worker/migrate）
internal/agent      Agent Loop 与团队编排
internal/llm        多 Provider 归一化
internal/tools      文件/shell/python/web/memory/skill 工具
internal/sandbox    Docker 沙箱与电脑容器
internal/mcpservice HTTP MCP 客户端
internal/routines   定时任务调度
internal/goals      群目标状态机
internal/memory     记忆：召回、去重、回顾、整理
internal/httpapi    REST + SSE + noVNC 代理
web                 Next.js 前端
deploy              Compose / Caddy / 镜像
migrations          goose 迁移
```
