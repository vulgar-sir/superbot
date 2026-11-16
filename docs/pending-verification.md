# 待统一验证清单（2026-11-13）

本文件记录「已实现、等待最后统一验证」的项。实现完成后集中跑一次，逐项打勾并补证据。

## A. 内置技能英文描述（0067）✅ 已验证 2026-11-13

- 迁移：`migrations/0067_skill_description_en.sql`
- 代码：`internal/skills/builtin.go`（10 个内置技能补 `DescriptionEn`）、`queries/skills.sql`、skills DTO、扩展页 SkillCard 按语言取 `description_en`
- 验证结果：生产迁移 68；`GET /api/skills` 9/9 返回 `description_en`；英文模式扩展页显示英文描述

## B. 例程邮件触发（0068）✅ 已验证 2026-11-13

- 迁移：`migrations/0068_email_trigger_seen.sql`
- 代码：`internal/emailtrigger`（60s 轮询 IMAP、Message-ID 去重、`email:` / `email:关键词` 过滤）、worker 接入、例程 UI 预设「邮件到达」
- 验证结果（GreenMail）：配置 IMAPS 3993 + 连接器后，投递「发票 2027-01」邮件 → 60s 内 `email` 例程在**当前私聊**生成一次运行（16:15:32）；再等一个轮询周期仍只有 1 次，去重生效。
- 过程中修复：`ListDueBotRoutines` 把 `email:` 例程当定时任务反复领取；例程运行自己查找无 `direct_user_id` 的并行 DM（新增 `CreateDirectUserConversation`，优先投递到创建者 DM）；忙碌时唯一约束冲突改为跳过而不是无限重试。测试连接器/例程/邮件/容器已清理。

## C. 本轮批量收口（2026-11-13）

- ✅ 通知中心筛选（全部/未读/消息/审批/子代理）已实现并部署
- ✅ Bot 设置 Provider/模型保存链路修复（保存资料后补 `PATCH /api/bots` 全量载荷；接口实测 200 并落库）
- ✅ 桌面通知 IPC 实测返回 true；单实例/深链代码在位
- ✅ Passkey 行调整为 `requires_config`（HTTP IP 无法 WebAuthn）

## D. 仍未实现 / 按用户要求暂不做

- ~~内部结构项：Bot 安全填表、MCP Apps/Widget~~ ✅ 已实现并验证（见矩阵对应行）
- 用户模板描述多语言（属用户数据，不翻译）
- **用户 2026-11-13 明确暂不做**：Team Bot 的 Teams/Slack 安装发布、支付卡结构化/导入、1Password、Slack/Teams/Google/Outlook/会议平台、外部凭据类、生活服务/虚拟卡
- 已保留：CalDAV 自托管日历、本地会议录制/转写/摘要、Slack 协议级 webhook、设备隧道等
- ~~Bot「参数」编辑器~~ ✅ 已实现并验证（JSON 参数 PATCH 200 读回一致）

## I. 故障注入与 soak（2026-11-13）

- ✅ Worker 容器强制重启：`docker restart superbot-worker-1` → 7 秒内恢复 running，迁移 69，`worker started concurrency=4`，API 持续 200；证据 `tmp/parity/fault-injection/worker-restart.log`
- ✅ API 容器强制重启：7 秒内恢复 running，迁移 69、DeepSeek provider/builtin skills 可用、`api listening`，Web 200；证据 `tmp/parity/fault-injection/api-restart.log`
- **72h soak（发布后运维观察，不属于功能交付）**：新增 `deploy/soak.py`（每分钟：Web 200、鉴权 API、记忆创建/删除、会话 SSE 读取，JSONL 记录）；生产已启动，起始 **2026-11-13 19:01（UTC+8）**，PID 2120597，日志 `/srv/superbot/soak.jsonl`，计划结束 2026-11-16 19:01；首轮 web/api 200、memory 201/204、SSE 7349 bytes
- 说明：功能与对照验证已完成（矩阵 86 已验证 / 0 部分 / 0 requires_config / 0 未开始；Playwright 39 passed）。72h soak 是发布后的稳定性观察，不是功能完成条件；结果从 JSONL 审计，运行满 72h 后可补一份稳定性报告
- **Chrome/FCM 网络复核（2026-11-14）**：服务器直连 fcm.googleapis.com 的 4 个解析 IP（216.239.36/38.x）均 5 秒超时；临时安装 Tor 经 SOCKS5 访问同样 20 秒超时。Firefox/Mozilla 通道已验证送达，Chrome 推送需服务器具备可达 FCM 的出口

## N. 工具调用合并 + 权限调高 + 资料库自动收录（2026-11-14）✅ 已验证

- **工具调用合并**：同一 run 的工具时间线合并为一个受控「N 个工具调用」行（默认收起、新 run 重置、展开为两列工具卡）；工具载入后自动滚到底部，摘要可见。审批结果系统消息（`工具审批已通过/被拒绝`）不再渲染为独立聊天行。生产实测 13 个工具显示为 1 行，展开仍是 2 列网格；`browser_open`+`browser_snapshot` 显示「2 个工具调用」。Service Worker 缓存版本升到 v2，旧客户端需强刷/重开
- **权限调高**：browser/computer 工具默认不再逐次审批（仍在隔离电脑内运行）；账户 allow 规则新增 `shell`、`workspace_write`、`browser_`；shell/write/vault/credential/delete/connector 仍可单独配置 always_ask。单测更新 `TestApprovalRequired`
- **资料库自动收录**：`internal/library` 索引器在 Bot 回合结束后扫描 workspace（图片/音视频/文档/简单代码），聊天附件与新建文档也自动入库；按 file_path/source 去重，跳过 `call_*.txt` 与 artifacts 下的非媒体输出
- 生产实测：资料库自动出现截图与 py 脚本（17 项，无工具文本噪声）；文档自动收录 1 项；图片附件自动收录（标题使用原文件名）
- 资料库预览修复：代码扩展名（py/go/js/ts/…）加入文本预览白名单，前端按 `text/*` 使用 `/preview` iframe；文档自动项支持更新同步与删除同步（文档删除时清理对应资料库项，更新时刷新标题/内容）
- 清理：测试文档/附件/资料项已删除；历史 E2E 文档产生的 23 个孤儿资料项已清理；证据 `tmp/parity/library/library-doc-preview.png`、`library-code-preview.png`

## M. 自托管 OIDC/Dex（2026-11-14）✅ 已验证

- compose `dex`（GHCR 镜像，Docker Hub 在本机不可达）+ Caddy `/dex/*`；Dex 配置在 `/srv/superbot/dex/config.yaml`（模板 `deploy/dex/config.example.yaml`），`.env` 配置 issuer/client/secret/redirect
- 生产实测：登录页「使用 SSO 登录」→ Dex 登录 → 授权页 Grant Access → callback → 主界面；`/api/auth/me` 返回 `admin@superbot.local`（与既有管理员同邮箱，未创建第二个用户）；`/api/capabilities` 的 `oidc=available`
- 证据 `tmp/parity/oidc/dex-login.png`；SSO 密码在本地 `tmp/dex/credentials.json`，未写入 git

## L. 「测试使用bot」浏览器问题（2026-11-14）✅ 已修复并验证

- 用户反馈该 Bot 打不开浏览器/百度。定位到 **5 个叠加问题**：
  1. Bot `computer_profile=none`，没有 browser 工具；按要求改为 `browser`
  2. auto-sleep 只看 `bots.last_activity_at`，忽略 `workspaces.last_used_at`，活跃 computer 容器每 10 秒被 SIGKILL；修复查询并在发消息/回合开始/工具调用时 TouchBotActivity
  3. `browser.NewSession` 每个 cycle 新建标签页并 `about:blank`，审批恢复后页面/ref 全丢；改为 `context.WithoutCancel` + 按 botID 缓存 Session + 工具互斥
  4. Chromium `--disable-software-rasterizer` 导致截图纯白；browser/desktop 启动脚本移除
  5. 容器重建后 Chromium `SingletonLock` 报 profile in use；启动脚本清理锁文件
- 验证：`browser_open` 百度 → snapshot（搜索框 [13]/按钮 [14]）→ `browser_type` 输入“今日新闻” → `browser_click` → snapshot → `browser_screenshot`；截图 **222,385 字节、1050×793、17,840 色**（此前 7 张均为 4,068 字节纯白），结果页标题「今日新闻_百度搜索」
- 证据 `tmp/parity/browser/summary.md`；`go test ./...` 通过

## K. 自托管语音 STT/TTS（2026-11-13）✅ 已验证

- 新增 compose 服务：`stt`（faster-whisper-server + `Systran/faster-whisper-small`，固定中文）与 `tts`（Piper `zh_CN-huayan-medium`）；模型/音色分别缓存在卷中
- `.env` 已配置 STT/TTS URL、模型、音色与本地占位 key；重建 api/worker 后 `/api/capabilities` 的 voice 变为 **available**
- 实测：`/api/voice/speak` 返回 141,868 字节 WAV（RIFF，22050Hz，70912 frames）；将同一 WAV 上传 `/api/voice/transcribe` 返回「一好这是超级机器人与银河乘测室。」（口述为「你好，这是超级机器人语音合成测试。」）；测试音频已删除
- 语音配置 JSON 随 TTS 镜像内置，规避镜像对小文件重定向返回 403/超时的问题

## J. 自托管语义记忆（2026-11-13）✅ 已验证

- 新增 `deploy/images/embedding`（fastembed 0.9.1 + `intfloat/multilingual-e5-large`，1024 维）与 compose `embedding` 服务；模型从 `hf-mirror.com` 下载并缓存到 `superbot_embedding_models` 卷（`HF_HUB_DISABLE_XET=1` 解决 Xet CAS 卡住）
- 生产 `.env`：`SUPERBOT_EMBEDDING_BASE_URL=http://embedding:8090`、`SUPERBOT_EMBEDDING_MODEL=intfloat/multilingual-e5-large`、`SUPERBOT_EMBEDDING_API_KEY=local-embedding`；重建 api/worker 后生效
- 验收：`/healthz` `{status:ok, dimension:1024}`；`reembed-memory` 6 条 → `remote:intfloat/multilingual-e5-large`；`/api/capabilities` `memory_semantic=available`；查询「部署包放在哪个文件夹」在 pgvector 最近邻命中「交付物保存在 /srv/releases 目录」0.8816（无关记忆更低）；测试记忆已删除

## H. Web Push 真实送达 + Passkey 协议验证（2026-11-13）✅ 已验证

- **Web Push（Firefox / Mozilla autopush）**：本机 Firefox 经 SSH 隧道访问 `http://localhost:32609`（localhost 属安全上下文）→ 订阅成功（服务端 201 落库）→ 测试推送 `sent=1` → Firefox PushService 日志出现 `receivedPushMessage` / `notifyApp` / `notificationForOriginShown`（Service Worker 已调用 showNotification）并回 ack code 100。证据 `tmp/parity/notifications/webpush-firefox.log`、脚本 `tmp/parity/notifications/push-firefox.mjs`。Chrome/FCM 仍因服务器到 fcm.googleapis.com 超时不可用，Electron 无推送服务，桌面通知走原生通道
- **Passkey（Chrome 虚拟认证器）**：`http://localhost:32609` 下真实 WebAuthn 注册 → 登出 → 安全密钥登录成功 → 删除凭据 204，证据 `tmp/parity/passkey/passkey-run.log`、`settings-passkey.png`、`login-passkey.png`
- **Passkey over real HTTPS（2026-11-14）**：用 Cloudflare Quick Tunnel 提供受信任的 HTTPS 域名，Chrome + 虚拟认证器：注册 1 个凭据 → 清 Cookie → 安全密钥登录成功 → 删除凭据 204；`webauthn=available`。证据 `tmp/parity/passkey/https-cloudflare.md`、`https-serveo-register.png`、`https-serveo-login.png`。生产 HTTP IP 仍需域名+TLS
- **顺带修复**：Caddy 的 `X-Forwarded-Host {host}` 会丢掉非默认端口，服务端算出的 origin 变成 `http://localhost`，与浏览器的 `http://localhost:32609` 不匹配，Passkey 报 `Error validating origin`。改为 `{hostport}`（默认端口会被 go-webauthn 归一化），已部署验证
- **Bot 可见性**：`PATCH /api/bots/{id}/profile` 切 owner→account 均 200 且读回一致
- 矩阵据此把仅剩外部依赖的行明确标注为 `not_in_scope`（Twilio/X/Google/Outlook/会议平台/Team Bot 外部安装），不再以 `部分` 挂起

## G. 菜单子菜单与帮助弹窗（2026-11-13）✅ 已验证

- 缺陷：`Menu` 的子菜单此前通过独立 portal 渲染，点父项后子菜单在父菜单的 mousedown 外部点击处理中被卸载，导致「支持 → 快捷键」等子菜单点不到（Playwright 复现：点击后对话框不出现）
- 修复：子菜单改为 `nested` 渲染在父菜单 DOM 内；重新打开菜单时清理上次残留的子菜单；不再在 highlight 变化时误清子菜单
- 后续回归发现并修复：为稳定子菜单位置新增的 `useMemo` 被放在 `if (!open) return null` 之后，违反 Hooks 顺序；菜单打开时 React 报错并导致页面加载失败。已把 `useMemo` 移到条件返回之前
- 帮助弹窗：标题/快捷键/安装/自托管全部接入 zh/en 词典；版本改为读取构建元数据（`web/package.json`），不再写死 `v0.1.0`
- 验证：无头 Chrome 实测英文模式打开「Account menu → Support → Keyboard shortcuts & install」，对话框标题 `Help & support`、9 条快捷键、`v0.1.2 · self-hosted`，无中文；证据 `web/test-results/parity/help-en.png`。新增 Playwright 用例并排到文件末尾；用例内拦截 `PUT /api/settings/language`，避免把账户语言写脏，`afterAll` 作为兜底恢复中文（此前语言测试会污染后续用例，且 `finally` 里再发请求会在 90s 超时边界上抖动）

## F. 桌面自动更新链路（2026-11-13）✅ 已验证

- 版本统一为 **0.1.2**：`web/package.json`、`desktop/package.json` 与两个 lockfile；帮助弹窗版本改为构建元数据（不再写死 `v0.1.0`）并接入 zh/en 词典
- 修复 feed 发布到错误目录：Caddy 实际挂载的是 `/srv/superbot/deploy/desktop-release`（compose 相对路径），此前上传到 `/srv/superbot/desktop-release` 不会被服务
- 修复更新通道 404：electron-updater 在 Windows 按 `<channel>.yml` 取元数据；设置里的 stable/beta 会请求 `stable.yml`/`beta.yml`，此前只有 `latest.yml`。现三份文件同源发布（当前仅一个构建）
- 实测：0.1.1 静默安装到临时目录 → 启动自动检查 feed → `pending/superbot Setup 0.1.2.exe`（sha512 与 latest.yml 一致）→ 关闭应用后静默安装 → `app.asar` 内 package.json 为 0.1.2
- 实测：0.1.2 打包态 `getAppInfo()` = `{version:0.1.2, packaged:true, channel:stable}`；`checkForUpdates()` = `{status:"latest", latest:"0.1.2"}`（证明 stable.yml 通道可用）
- 临时安装已卸载、测试目录已清理

## E. 收尾回归（2026-11-13）

- ✅ `go test ./...` 通过
- ✅ `npx tsc --noEmit` 通过
- ✅ Playwright **39 passed**（管理页逐页 + 控制台错误检查 + 英文帮助弹窗/子菜单 + 功能域矩阵 7 项；最终全量 2026-11-13 19:09）
- ✅ Electron `electron-vite build` 通过
- ✅ NSIS 安装包构建成功：`desktop/release/superbot Setup 0.1.0.exe`（107MB，未签名，按用户要求跳过证书）
- ✅ 生产 Web 200；迁移 69
- 能力配置清单：`docs/configuration-required.md`
