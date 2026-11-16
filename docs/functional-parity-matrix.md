# superbot × Grok Bot 功能对照矩阵

> 基线：本机 Grok Bot **1.0.1**（2026-11-12 由 0.79.2 强制升级）（Electron）+ superbot 代码盘点（2026-11-12）。
> 2026-11-13 收尾快照：102 行中 已验证 86、部分 0、not_in_scope 14、requires_config 0、platform_limited 2、未开始 0。
> 运行时 `/api/capabilities` 仍会因当前环境未配置而返回 `requires_config`（webauthn=HTTP IP、oauth=第三方连接器凭据；oidc 已通过自托管 Dex 变为 available，voice 已通过自托管 STT/TTS 变为 available），这是如实的能力状态，不等于功能未实现。已无未开始项；剩余均为外部凭据/网络/平台限制，见 `docs/configuration-required.md`。
> 本文件是功能完成度的唯一事实来源；状态为 `已验证` 必须有真实数据、Web/Electron 证据和对照记录。
> 证据目录：`tmp/parity/<domain>/`。
> 自动化：`web/e2e/` 对生产执行 **39 passed**：入口/账户（1）、管理页与 Canvas/文档/日历/会议/SSE/帮助弹窗（31）、功能域矩阵（7：保险库、用户记忆、例程、群成员、头像、主 Bot、通知与能力状态）；证据截图归档 `tmp/parity/e2e-2026-11-13/`（18 张）。无法自动化的桌面/硬件/外部项（WebRTC 语音、设备隧道、电脑快照、Web Push、Passkey、安装更新）均有单独实测记录。跑 E2E 必须用 `SUPERBOT_E2E_ENV_FILE=E:\project\superbot\tmp\server.env`（本地 `.env` 的 ADMIN 密码已过期，会 401）。
>
> 说明：Grok 的官方云/账号/订阅/官方市场能力不复刻，按等价自托管实现；未配置的第三方不能标记为完成。

## 运行时可靠性补齐（2026-11-16，对照 DS-Bot 的差距逐项实现）

验证分三层：本地 `go test ./...`；服务器 `deploy/run-integration-tests.sh`（一次性库 `superbot_test` + 真实 Postgres + 指令驱动的内置 mock 模型，21 个集成测试全过，不耗 token、不碰生产数据）；生产 `deploy/acceptance_runtime.py`（真实 DeepSeek，9/9 PASS，临时数据自动清理）。浏览器：新增 `web/e2e/runtime.spec.ts` 4 passed，原 39 项回归 39 passed。迁移 70–72 已上生产（迁移前备份 `backups/superbot-pre-runtime-20261116-054848.dump`）。

| 能力 | 实现 | 状态 | 证据 |
|---|---|---|---|
| 自动上下文检查点 | 历史超预算或超行数时折叠为结构化笔记（8 个固定标题 + 用户原话逐字保留），失败用机械摘录兜底；`search_chat_history` 回查原文；工具循环内按预算压缩 | 已验证 | 验收 checkpoint：170 条历史 → `source=auto`，第 3 条里的代号仍答对；集成 `TestIntegrationAutoCheckpoint` |
| 断点续跑 | 每个工具轮次写进度检查点，worker 重启后续跑不重放；不可重试错误（缺 Provider、401、模型不存在、余额）直接失败；panic 兜底；优雅退出立即交还租约 | 已验证 | `TestIntegrationResumeDoesNotReplayTools`；验收 failure：坏模型 1 次即 failed |
| 运行中插话 | 下一步模型调用前注入；赶不上的自动补一轮；UI 显示「已插话 / 已送达」 | 已验证 | 验收 interjection；两个集成测试 |
| 旁路提问 `/side` | 快照回答、不调工具、不写入对话；侧栏面板 | 已验证 | 验收 side：前后消息数不变；浏览器 side 用例 |
| 协作闭环 | `send_to_agent` 回复与子代理结果送回发起方并唤醒；接力上限 6、自动唤醒上限 8；按「Bot 写回复时读到哪里」重排历史，避免回复早于提问 | 已验证 | 验收 closure；`TestIntegrationWireReplyArrivingDuringTurnComesAfterIt`、`TestIntegrationAutoWakeLimit` |
| 审批后团队续跑 | 流水线/圆桌/群聊在审批恢复后跑完剩余阶段；审批恢复事务化，忙时延后不丢；被取代的检查点不再二次恢复 | 已验证 | 验收 pipeline：两阶段各回复 1 次 |
| 群目标 | 起草→开始→分派→汇报→凭证据验收/返工→完成；群主空闲自驱，无进展 2 次后停；规则违反返回 422 | 已验证 | 验收 goal：两阶段 36 秒完成并自停；浏览器 goal 用例 |
| 记忆分层 | 相关度阈值（e5 实测无关 ≤0.872、相关 ≥0.888，默认 0.88，并丢弃落后最佳 0.05 以上的命中）；写入去重；疑似密钥拒绝；空闲回顾提炼；每日整理可恢复；置顶/归档 | 已验证 | 验收 memory；三个记忆集成测试；浏览器 memory 用例 |
| 模型层 | 请求阶段重试（429/5xx、Retry-After）、流空闲超时、finish_reason 与截断续写、Anthropic Provider、DeepSeek 思考模式 `reasoning_content` 回传与请求形状兜底；`SUPERBOT_LLM_DEBUG_DIR` 可落盘被拒请求（默认关） | 已验证 | `TestIntegrationWire*` 5 项（本地假思考模型服务器强制同样规则） |
| 沙箱与安全 | 只挂载 Bot 所在团队的共享目录（成员变化自动重建）；审批 allow 规则不能被参数文本满足；MCP stdio 环境变量白名单；`postgres_query` 只读事务；stdio MCP 创建/批准需 owner/admin；子代理嵌套上限修正；SSE 支持 Last-Event-ID | 已验证 | 验收 sandbox：容器内 `ls /teams` 只见本团队 |

| Docker 访问收口 | worker 不再挂载 Docker socket，经 `docker-proxy`（`internal/dockerproxy`）访问：只放行沙箱生命周期所需的 10 个端点，只能操作 `superbot-bot-*` 容器；创建请求逐项校验（特权、CapAdd、host 网络、设备、root 用户、镜像白名单……），挂载源只允许 `workspaces/<uuid>` 与 `teams/<uuid>/shared` 且路径上不得有符号链接；代理只在内部网络、无端口 | 已验证 | 验收 proxy：images/build/特权创建/进入 postgres 执行均 403；22+ 项代理单测 |
| 服务降权 | API 与 worker 以 uid 1000 运行；`data/`、`workspaces/`、`teams/`、`teams/<id>/` 为 root 只读，叶子目录由代理按需创建（`deploy/harden-data-permissions.sh`） | 已验证 | 生产实测：worker 内植入/替换/删除/改名挂载源全部被拒，工作区内正常写入 |
| 停止即清理 | 命令运行在独立进程组；停止时按进程组、环境标记、以及「执行开始后被 PID 1 收养的孤儿」三路清理；容器进程数满导致清理无法启动时重启容器兜底 | 已验证 | 真实 Docker 集成测试 3 项（含 `env -i setsid` 逃逸与进程数打满） |
| 记忆一致性与预算 | 去重写入在按作用域的事务锁内；后台回顾检查账户周预算并在会话预算上预留/结算 | 已验证 | 10 路并发写同一事实只存 1 行；预算耗尽时回顾被拒且不动预算 |
| 团队周期续跑 | 被回收的团队周期按已记录进度继续（流水线阶段 / 圆桌轮次 / 群聊队列），被打断的成员从工具轮次检查点续跑 | 已验证 | `TestIntegrationReclaimedPipelineResumesAtUnfinishedStage` |
| 团队例程并入群聊 | 去掉独立的 `/routines` 管理页（只在管理页图标栏和搜索里有入口，每次运行新建「[定时]」会话，且与 Bot 例程重复）；团队例程改在群聊右侧详情面板（目标下方）管理，到点发进群聊本身的会话，群忙时按插话处理；聊天里显示为「例程 · 名称」提示；旧地址 307 跳回首页 | 已验证 | `TestIntegrationTeamRoutinePostsIntoTheGroupItself`；浏览器用例建群加例程后在面板中可见；生产已有的「每日站会」出现在「生产协作小队」面板 |
| 例程时区 | 团队例程按账户在「设置 → 通用 → 时区」里的时区计算（未设置为 Asia/Shanghai），不再依赖服务器时区 | 已验证 | 生产实测「每天 9:00」下次运行 `2026-11-17T09:00:00+08:00` |
| 其他 | 设备任务签名密钥由平台密钥派生（重启与多实例一致）；登录限流按真实客户端 IP，成功登录清零 | 已验证 | 单测；43 项浏览器回归通过 |

验证汇总（本轮结束时）：集成测试 25 项全过；生产验收 11 个场景全过（含 proxy、browser）；浏览器 43 项全过；CI：用 `act 0.2.98` 在服务器上对 git 干净克隆执行 `.github/workflows/ci.yml`，`go`（gofmt、vet、sqlc 生成代码一致性、单元测试、基于 pgvector 服务容器的集成测试）与 `web`（npm ci、typecheck、build）两个 job 全部成功；运行环境适配仅为 `node:24-bookworm` 镜像、host 网络与 goproxy.cn / npmmirror 源。仓库尚无远端，GitHub 托管运行器上未跑过。72 小时 soak 见 `deploy/soak_report.py` 输出。

## 当前进度快照（2026-11-13）

- **Team Bot 服务账号（2026-11-13）**：新增可撤销的服务令牌（sha256 存储、明文只回显一次）与公开消息入口；生产实测建 Team Bot → 建令牌 → 无 Cookie 投递 → Bot 回复 ok → 撤销后 401。测试 Bot/令牌/消息已清理。
- **记忆 CRUD/排序（2026-11-13）**：新增记忆更新/删除接口与 UI，编辑会重新生成向量；生产界面实测编辑→API 一致、删除→API 消失，排序切换可用。
- **更新/版本（2026-11-13）**：桌面 IPC 暴露真实版本与检查更新；设置页显示 `v0.1.0 · 开发模式（未打包）`，检查按钮返回「开发模式无法检查更新」，打包态切换通道会重设 autoUpdater 并检查。
- **主动消息（2026-11-13）**：补 proactivity HTTP 接口与 Bot 设置开关；关闭态工具被拒、开启态真实投递，并把投递目标从「自己查 DM」改为当前会话，修复错投并行会话的问题。
- **范围调整（2026-11-13）**：用户明确当前**单用户自用、暂不需要多租户/多使用者**；后续优先单用户功能与体验，多账户/邀请/跨账号 ACL/Team Bot 外部安装等不再投入（已完成部分保留）。
- **团队记忆验证（2026-11-13）**：团队会话内 memory_save(scope=team) → memory_search 命中，团队作用域闭环。
- **MCP Apps/Widget（2026-11-13）**：MCP 工具 `_meta.ui.resourceUri` → 读取资源 HTML → 事件携带 `widget_html` → 工具卡 iframe（`sandbox="allow-scripts"`，注入 `window.__MCP_TOOL_RESULT__`）渲染；同时补齐聊天内工具时间线（ToolCard 此前未挂载）。生产用 mcp-echo 的 echo_widget 实测 iframe srcdoc 含应用 HTML 与结果注入。至此矩阵**未开始 = 0**。
- **范围再次收敛（2026-11-13）**：用户明确「外部额外拓展的，Team Bot Teams/Slack 安装、支付卡结构化、1Password、Slack/Teams/Google/Outlook/会议平台、外部凭据类都先不做」。矩阵对应行改为 `not_in_scope` 或在备注标注暂缓；同时验证子代理完成路径（succeeded/SUBAGENT-OK）。
- **i18n 全页收尾（2026-11-13）**：补齐 bots/teams/activity/calendar/email/audit/扩展页文案；英文模式逐页扫描，UI 文案已全部覆盖，剩余中文为数据、语言自称与 OS 设备名。
- **上下文预算动态化（2026-11-13）**：按模型 context_window 与 max_output 计算预算，保留稳定前缀与最近 6 条，单测覆盖裁剪边界。
- **记忆导入完善（2026-11-13）**：支持 AI 导出 JSON（content/text/memory/rule 字段）与文本行导入，用户记忆加文件导入 UI；生产实测 JSON 3 条 + 文本 2 条。
- **批量收口（2026-11-13）**：通知中心加筛选（全部/未读/消息/审批/子代理）；修复 Bot 设置里 Provider/模型选择未持久化（保存资料后补 `PATCH /api/bots`）；桌面通知 IPC 实测返回 true；Passkey 行改为 `requires_config`（HTTP IP 无法 WebAuthn）。
- **内置技能英文描述 + 例程邮件触发（2026-11-13）**：技能表加 `description_en` 并回填 9 个内置技能，英文扩展页显示英文；新增 `email:` 例程触发（GreenMail 实测一次触发、去重），并修复例程运行投递到并行 DM 的旧问题。清单见 `docs/pending-verification.md`（A/B 已勾选）。
- **设置页 i18n 收尾（2026-11-13）**：补齐时区/麦克风/硬件加速/版本/通道/登录方式/密码说明/Push 提示/主题选项等，英文模式设置页只剩「中文」语言自称与 OS 设备名（属数据）。
- **设置页 i18n 进展 + 词典修复（2026-11-13）**：设置页头部/返回、导航、主题/强调色、密码区、成员/Provider/设备/保险库等主要按钮与说明接入词典；英文模式设置页从整页中文降为 23 行中文（多为描述与选项）。过程中一次去重脚本误删了 en 块，已从生产构建 chunk 恢复全部 504 个键（zh/en 对齐、tsc 通过），并重新部署验证。
- **用户级记忆（2026-11-13）**：新增用户作用域 CRUD API 与「我的记忆」UI，并在 direct 会话召回中并入；实测 Bot 只凭用户记忆回答「安装包」。
- **会话内查找（2026-11-13）**：Ctrl/Cmd+F 查找条（计数/上下跳转/Esc），系统消息与 agent-thread 卡片补 `data-message-id`；实测命中 10 条系统消息并滚动定位。
- **PPTX 预览（2026-11-13）**：解析 ppt/slides/slideN.xml，按页码输出标题与要点；单测覆盖转义与多页，生产上传 pptx → iframe 渲染两页内容；测试资料已删除（文件同步清理）。
- **扩展页 i18n（2026-11-13）**：补齐 Skill/MCP/模板/团队审查/连接器目录等 UI 文案（并修复一处「Connect器」拼接错误）；英文模式 /extensions 只剩 Bot 名与服务端内置技能描述。
- **Web Push（2026-11-13）**：完整实现服务端 VAPID/订阅/下发 + Service Worker 通知 + 设置页开关；HTTP 下用 Google Chrome 实测订阅与落库成功，但服务器到 FCM 超时，测试接口如实返回 502。详见「通知中心」行。
- **标题栏点击修复（2026-11-13）**：用户反馈窗口按钮点不动。定位到 Chromium 的非客户区命中测试：完全透明的 `no-drag` 区域会被忽略，底下页面头部的 `drag` 区域把点击当成拖窗口；且标题栏在 DOM 中先于页面渲染。修复：`DesktopTitleBar` 用 `createPortal` 挂到 `body` 末尾（最后绘制）、层级提到 2147483000、caption 与按钮加 `no-drag !important`、caption 使用与页面一致的**不透明**背景（#fcfcfc/深色变量）。实测真实鼠标（DPI 感知 + ClientToScreen + SendInput）：还原状态点最大化 → 窗口变为 -9,-9 1938x1038；再点还原 → 回到 336,60 1320x940。
- **文档/Canvas i18n（2026-11-13）**：文档页与 Canvas 编辑器（工具栏、属性面板、图层、导出、prompt）全量接入词典；英文模式 /documents DOM 中文 0 项。证据 `tmp/parity/ui-main/documents-en2.png`。
- **任务/Store 页 i18n + 标题栏遮挡修复（2026-11-13）**：任务页（状态列/视图切换/项目弹窗/角色）与 Bot Store 全量接入词典，英文模式实测 /tasks 仅剩 Bot 名、/store 中文 0 项。另修复桌面端管理页右上角控件被窗口按钮遮挡的问题：13 个管理页 header 加 `sb-page-header`，桌面模式右侧预留 150px，实测最右控件 right=874、caption 从 888 开始，不再重叠。证据 `tmp/parity/ui-main/tasks-en.png`、`store-en.png`、`tasks-fixed.png`。
- **Slack Webhook 协议验收（2026-11-13）**：用自建 connector + signing_secret 对生产验证 challenge/签名/时间戳/消息事件 → 通知；通知标题随账户语言（Slack 消息 / Slack message）。测试连接器与通知已清理，语言恢复中文。真实 Slack 工作区凭据仍未提供。
- **Bot 模板 memory/MCP（2026-11-13）**：模板结构新增 memories 与 mcp_servers（密钥不导出）；导入重建记忆并按名称复用/创建 MCP 后绑定。生产往返验证 1 条记忆 + 1 个 MCP，导入 Bot 后两者都在；测试数据已清理。
- **服务端消息本地化（2026-11-13）**：新增 `internal/i18n` 与 `/api/settings/language`（按账户持久化）；前端切换语言即同步服务端，新设备在无本地值时读取账户语言。审批结果、子代理完成/失败/取消通知与消息按账户语言输出；生产实测英文环境生成「Tool approval granted: shell」，随后恢复中文并清理测试消息。
- **Bot 跨私信对照（2026-11-13）**：实测 Grok 1.0.1 的跨私信弹窗是只读（无任何输入框），superbot 行为一致；生产用 `send_to_agent` 生成折叠卡并打开面板核对双方消息，测试线程/消息已清理。此前矩阵把它记成「缺用户回复」，属于对标错误，已更正。
- **i18n 主壳层（2026-11-13）**：补充侧栏/详情栏约 120 个词条，`t()` 支持 `{n}` 占位符；相对时间与动态类型随 `html lang` 切换。英文模式实测：侧栏与 Composer 中文 0 项、详情栏仅剩 Bot 名等内容数据；`Activity/Library/Files`、`Recent activity`、`Reply · 7 min ago` 正常。管理页长尾文案仍待接入。
- **群聊成员增删（2026-11-13）**：`/teams` 编辑弹窗选择成员并保存（`PUT /api/teams/{id}/bots`，≤6）。生产桌面实测 2 人 → 加成 3 人 → 移除回 2 人，API 每次核对一致；测试群已删除。
- **主 Bot 账户级工具（2026-11-13）**：新增 `list_agents`；`create_group`/`create_routine` 支持 Bot 名称解析（原来只收 UUID，实际调用会失败）。生产用主 Bot 真实跑通四步：列 Bot → 建 Bot → 按名称建群 → 按名称建例程，并核对 bots/teams/bot-routines 落库；测试数据全部清理。
- **记忆向量空间（2026-11-13）**：新增可配置 embedding Provider（OpenAI 兼容 `/v1/embeddings`、1024 维、归一化、超时/错误处理）；迁移 0064 加 `memories.embedding_model`，保存与检索都带模型名并只比较同一空间；新增 `superbot reembed-memory` 迁移历史向量（无 key 明确报错）。`/api/capabilities` 新增 `memory_semantic`，未配置时为 `requires_config` 并说明仍用本地哈希。单测 4 项通过；生产实测 Bot 通过 `memory_save`/`memory_search` 保存并命中「蓝鲸-MEMTEST」，DB 标记 `local-hash-1024`，测试数据已清理。
- **会话摘要复用（2026-11-13）**：`buildMessages` 读取最新 `conversation_summaries`，作为系统消息注入并跳过 `ordinal <= up_to_seq` 的旧消息，避免长会话重复塞满上下文；worker 会记录 `conversation summary in prompt`（含 up_to_seq/chars）。生产实测：真实生成 794 字摘要后触发回合，日志确认摘要进入 prompt，测试数据已清理。
- **资料库 Office 预览（2026-11-13）**：新增 `/api/library/{id}/preview`，用 Go 标准库解析 DOCX（段落/表格）与 XLSX（sharedStrings/多 sheet/单元格坐标），文本类直接转义输出；响应带 CSP、全部文本 HTML 转义。前端预览弹窗识别 docx/xlsx/txt/csv/md 并用同源 iframe 展示。生产实测两份真实文件渲染正确；删除资料时同步删除磁盘文件，服务器 library 目录无孤儿。
- **例程 Webhook 与 flexible（2026-11-13）**：迁移 0063 加 `bot_routines.trigger_token`（唯一索引、历史行回填）；新增公开 `POST /api/hooks/routines/{token}`（无 Cookie，令牌即凭证）→ 202 并真实生成一次运行；停用/暂停 409、错 token 404。例程编辑弹窗新增 Webhook 地址+复制。flexible 由「只存配置」改为真实语义：超过 15 分钟才被调度器发现则跳过本次（一次性例程始终执行），并补 `TestShouldSkipFlexible` 单测。生产实测：例程 DM 出现任务与 `webhook-ok` 回复，测试例程/消息已清理。
- **Bot 头像（2026-11-13）**：新增 `GET/POST/DELETE /api/bots/{id}/avatar` 与 `POST /api/bots/{id}/avatar/generate`（multipart 白名单 PNG/JPEG/WebP/GIF、≤4MB、按内容嗅探；文件存 `DataDir/avatars/{botID}`，DB 里存带版本号的 URL；未配置生图返回 `requires_config`）。前端 `BotAvatar` 支持图片并在侧栏/头部/详情/消息/@ 列表等 20+ 处接入；头像面板「上传」接真实上传/移除，「生成」接真实图像模型（不再用哈希伪造）。生产实测：上传 1×1 PNG → 三处 `<img>` naturalWidth 1024 正常 → 移除回退形状；真实生图 1024×1024 PNG 161KB；txt 上传 400。
- **资料/主 Bot 入口（2026-11-13）**：详情栏名称 hover 铅笔 →「Bot 设置」弹窗可达（名称/标题/描述/Provider/模型/可见性/分组/置顶/导出模板/主 Bot），点名称内联重命名与 Grok 一致；`POST /api/account/primary-bot` 切换并还原主 Bot 实测通过；删除主 Bot 返回 409 + 更换提示（守卫而非死锁）。
- **保险库编辑（2026-11-13）**：设置→保险库→条目编辑→改名→保存，API 读回新名称；密码留空不覆盖，查看走 GET reveal；测试条目已清理。
- **UI 交互**：Grok 1.0.1 的「+」直接打开新建 Bot（superbot 已改为一致，其他新建动作移到侧栏空白右键）；新建弹窗宽度 552、头像 140、形状按钮 48，证据 `tmp/parity/ui-main/superbot-newbot.png`；弹窗尺寸 550×676/y119 与 Grok 552×668/y120 基本对齐；Bot 右键菜单 240/项高 40，移除 Grok 没有的编辑资料项；账户菜单顶部改为真实周预算百分比，安装项改为“获取移动版 superbot”；“通知”移入“支持”子菜单；侧栏/标题/Composer 字号 15px 对齐。2026-11-13 修复子菜单点击：子菜单改为嵌套渲染，避免父菜单外部点击处理在 click 前卸载它；帮助弹窗同步接入 i18n 与真实版本。
- **UI 布局（2026-11-13 实测校正）**：Grok 1.0.1 在 1042×764 CSS 视口下侧栏 **240**、聊天列 **482**、信息栏 **320**、头部 **61**；superbot 在 1046 宽桌面窗口下现为 **240 / 483 / 323 / 61**，一致。修正了此前把物理像素当 CSS 像素的问题（旧值 310/353/70）。信息栏在 ≤1180 收窄为 320 并保持停靠，仅 ≤980 变为浮层，避免覆盖聊天内容；欢迎页三段建议改为按 Bot 名称/描述生成（不再全站写死）；Bot 详情栏「最近动态」上方留白由 104px 收到 24px。证据 `tmp/parity/ui-main/after-fix2.png`、`electron-window-final.png`。
- **Electron 自绘标题栏（2026-11-13 按 Grok 实测重构）**：Grok 1.0.1 的窗口是**无边框 + 原生 caption overlay**：没有独立标题条，右上角浮着 − □ ✕ 三个 46×32 按钮（与页面同底色，Segoe 图标 10px），内容从窗口顶端开始。superbot 改为同规格的自绘版本（`DesktopTitleBar` 只渲染右上角 138×32 控制区，46×32/按钮、10px 图标、hover 7% 混合、关闭键 #c42b1c），取消 36px 条与内容高度偏移；顶部区域（侧栏搜索行、聊天头部、信息栏头部）设为 `-webkit-app-region: drag`，交互子元素 no-drag，另加 8px 兜底拖拽条覆盖无顶栏路由。信息栏关闭时聊天头部右侧预留 138px（与 Grok 的分享按钮位置一致）。实测：真实鼠标拖动窗口 (336,38)→(476,150) 再拖回，按钮 DOM 几何与 Grok 一致。证据 `tmp/parity/ui-main/grok-window-full.png`、`grok-controls-zoom.png`、`superbot-window-full.png`、`superbot-topright-noinfo.png`。
- **Composer 几何（2026-11-13）**：按 Grok 实测（frame 高 44、圆角 22、按钮 28×28、右侧内边距 8、麦克风与语音/发送间距 8）修正：移除历史遗留的 66px 右侧内边距（曾导致语音/发送键离右边框约 70px），按钮 36→28、输入框最小高 36→28、底部留白 24→16。证据 `tmp/parity/ui-main/superbot-composer-zoom.png`。
- **生产**：`http://202.189.8.109:32608/`，迁移版本 **69**，页面 200；Electron NSIS 0.1.2 安装包与 `/desktop/` 更新 feed（latest/stable/beta）已验证。
- **Go Postgres 集成测试**：`internal/store/integration_test.go`（build tag integration）在 API 容器内对生产 Postgres 实测：创建文档 → revision 1→2 → 写 document_event → latest/after 查询 → 删除；PASS，测试数据已清理。另有 SSE 协作与 Docker 沙箱集成测试 PASS。
- **自动化**：Playwright **39 passed**（登录/账户、Bot/项目/Skill/模板 ACL、任务、搜索、文档、Canvas、日历、会议、SSE 协作、管理页、英文帮助弹窗；功能域矩阵 7 项）；`go test ./...`、`tsc`、`next build`、Electron build 通过；CalDAV/STT/TTS 有 mock 单测。
- **i18n 基座**：补齐缺失的 `web/components/LanguageProvider.tsx`（zh/en 词典、localStorage 持久化），修复 bots/teams/routines 三页构建；主界面其余页面仍未接入，故语言项保持部分。
- **任务看板**：`/tasks` 新增列表/看板切换与五状态列，卡片可拖拽到目标列改状态（乐观更新、失败回滚，视图写入 localStorage）；生产实拖「看板拖拽测试」→ 已完成，API 返回 done，测试任务已删除。证据 `tmp/parity/ui-main/tasks-board.png`。
- **消息语义/投票（2026-11-13）**：消息右键新增编辑/置顶/删除；编辑仅本人消息并写 `edited_at`，置顶写 `pinned_at`，删除软删并从列表隐藏；投票消息渲染单选/多选卡片、票数与百分比。生产端到端实测：编辑→`消息编辑测试-updated`+`edited_at`；置顶→`pinned_at`；删除→API/DOM 同时消失；投票→1 票 100%。迁移 0051/0052 已在生产。
- **通知偏好（2026-11-13）**：设置→通用→通知新增桌面通知、声音（默认/清脆/柔和/无声）、音量、消息/审批/子代理分类开关；服务端按账户持久化（`/api/settings/notifications`），通知中心按偏好播放 WebAudio 声音并决定是否弹 Electron 通知。生产实测 GET/PUT/GET 持久化，界面截图 `tmp/parity/ui-main/settings-notifications.png`。
- **捕获 overlay（2026-11-13）**：Composer「教它做一项任务」打开真实录屏 overlay，`getDisplayMedia` 录制、1.5s 关键帧（≤8）、10 分钟上限；停止后调用视觉模型生成 Markdown Skill 草稿，可插入输入框或保存为 Skill。生产桌面实测 3 帧 → 生成完整草稿，证据 `tmp/parity/ui-main/capture-recording.png`、`capture-draft.png`。Electron 增加 display-media handler 与 HTTP origin 安全上下文开关。
- **语音消息（2026-11-13）**：Composer→录制语音消息，getUserMedia + MediaRecorder 录音（最长 5 分钟），上传为 audio 附件并随消息发送；消息内渲染 audio 播放器。生产桌面实测录制 3 秒→发送→播放器出现，测试消息/审批已清理，证据 `tmp/parity/ui-main/voice-message.png`。
- **语言切换（2026-11-13）**：设置→通用→语言新增中文/English，切换后 html lang、设置导航与 Rail 导航实时更新并持久化；已覆盖登录页全文、设置标题、聊天 Composer 菜单/占位符/投票/消息菜单/欢迎建议、bots/teams/routines。生产实测英文登录页、设置与 Composer 占位符 `Message 测试`，证据 `tmp/parity/ui-main/language-en.png`、`settings-en.png`、`chat-en.png`。侧栏分组与设置描述仍未全量翻译，保持部分。
- **数据共享（2026-11-13）**：设置→通用→数据共享新增默认关闭的「匿名错误报告」；开启后前端 `error`/`unhandledrejection` 写入本机 `diagnostic_events`（无消息内容、不上传第三方），关闭时接口直接 204 不落库。生产实测：关闭时两条上报无记录，开启后真实错误与手动上报均入库；测试数据已清理。迁移 0056。
- **安全密钥管理（2026-11-13）**：迁移 0060 给 webauthn_credentials 增加 name；新增凭据列表字段、重命名与删除 API；设置页列出密钥名称/时间，支持内联重命名与确认删除。生产插入测试密钥后 API 列表/重命名/删除与 UI 均验证通过，测试密钥已删除。
- **CRDT 多实例（2026-11-13）**：CRDT 更新除本地 hub 广播外，同时写入 `document_events`；其他 API 实例的 SSE 轮询器会按事件 ID 投递，避免多实例下丢更新。Yjs 更新幂等，重复投递安全；双客户端收敛测试与 31 项 E2E 仍通过。
- **操作级 CRDT（2026-11-13）**：文档文本接入 Yjs：服务器新增 `document_crdt_updates`（迁移 0059）与 `/crdt`、`/crdt/seed` 接口；首客户端用原子种子建立唯一历史，后续编辑发送 Yjs 状态更新，同时写入 `document_events` 供多 API 实例的 SSE 轮询器投递。两个独立浏览器并发编辑 `base` → `base-AAA`/`base-BBB`，最终两端一致收敛（`base-BBB-AAA` 或 `base-AAA-BBB`）。原有 31 项 Playwright 全部通过。
- **SOCKS5 隧道（2026-11-13）**：在设备隧道上新增带用户名/密码认证的 SOCKS5 监听（8082，username=device id，password=tunnel token）。容器实测 `--socks5-hostname` 访问 Windows 本地服务返回 `socks-ok-from-windows`，访问 https://www.baidu.com 返回 200。测试设备/token/服务已清理，生产 env 还原。
- **设备流量隧道（2026-11-13）**：新增设备 WebSocket 隧道（`/api/device/tunnel`）与独立 egress HTTP 代理（8081，Proxy-Authorization 绑定 device id + tunnel token）。容器内 curl 经代理访问 Windows 本机 HTTP 服务返回 `tunnel-ok-from-windows`，HTTPS（baidu）经 CONNECT 返回 200。每条隧道连接在 Electron 本地弹窗审批；测试设备/token/服务已全部清理，生产 env 已还原。SOCKS5 未做。
- **WebRTC 实时语音（2026-11-13）**：新增 Pion WebRTC 服务（单 UDP 端口 40000 + NAT1To1 公网 IP、SDP offer/answer、DataChannel 音频）。客户端优先用 RTCPeerConnection 发送 16k PCM，服务端按 ~1s 缓冲调用 faster-whisper，转写文本经 DataChannel 回传并自动发送给 Bot。生产实测 DataChannel 打开、连续转写、会话消息与 Bot 回复均到达；测试后已还原环境。说话人分离未做。
- **语音通话真实回合（2026-11-13）**：临时 STT/TTS + Chromium 假麦克风（SAPI WAV）实测：MediaRecorder 分片→生产 `/api/voice/transcribe`→文本发送给 Bot→Bot 回复→`/api/voice/speak` 播放；对话出现 2 轮识别与回复，performance 记录 3 次 transcribe、2 次 speak（620–723KB）。随后还原 env、删除服务与测试消息。WebRTC 实时传输、说话人分离仍未做，但语音通话闭环已验证。
- **语音通话服务端模式（2026-11-13）**：VoiceCallPanel 读取 `/api/voice/status`；配置 STT 时用 MediaRecorder 4 秒分片上传转写并发送，配置 TTS 时播放 `/api/voice/speak` 音频，未配置回退 Web Speech/speechSynthesis。STT/TTS Provider 已各自真实验证；WebRTC 传输、说话人分离仍未做。
- **系统设置（2026-11-13）**：设置→通用补齐麦克风、时区、硬件加速、更新通道。麦克风枚举并用于语音消息；时区通过 `/api/settings/timezone` 按账户持久化并写入例程默认；硬件加速读写 desktop-settings.json，启动时 `disableHardwareAcceleration()`；更新通道写入并供 autoUpdater 使用。生产桌面实测四项均正常，测试值已还原。
- **麦克风设备（2026-11-13）**：设置→通用新增麦克风选择，枚举音频输入并持久化；语音消息录音按所选 deviceId 打开。生产桌面实测列出 Senary/ToDesk 等设备；时区与硬件加速仍未做。
- **桌面升级/卸载与签名（2026-11-13）**：NSIS 0.1.0 静默安装到临时目录 → 0.1.1 覆盖升级（ProductVersion 0.1.0.0→0.1.1.0）→ 静默卸载（退出码 0，目录删除）全部实测通过。用自签名证书 + signtool 对 0.1.1 安装包签名成功，签名块存在；因无法把自签名根加入受信任存储，验证结果仅为“根不受信”，正式 CA 代码签名证书仍待提供。证书与临时安装目录已清理。
- **图像生成（2026-11-13）**：接入 gpt-image 异步 API（POST /v1/images/generations?async=true + GET /v1/tasks/{id}），新增 `generate_image` 工具（prompt/size/quality/background/参考图），图片保存到工作区并写入资料库。生产用真实 API 生成 1024×1024 PNG（87KB），Bot 在会话中返回“生成完成”和资料库条目；库文件下载 200。生图模型默认 gpt-image-2.5-flare。
- **Bot Store（2026-11-13）**：允许发布 `visibility=public` 模板；新增 `/api/store/templates` 列表与 `/install`（安装计数），`/store` 页面一键创建 Bot；扩展页发布新增「公开到 Store」。生产实测发布→列表→安装→导入新 Bot→计数 +1，测试模板/Bot 已清理。付费交易明确 not_in_scope。
- **套餐/额度（2026-11-13）**：迁移 0062 增加 accounts.plan/credits_micros 与 credit_ledger；新增 `/api/billing`（套餐、credits、周预算/已用、账本）与 `/api/billing/credits`（owner/admin 手动调整，事务+审计）。设置→用量新增套餐与额度卡。生产实测 +¥10 后余额与账本正确，测试数据已清理。真实支付/发票明确标记 not_in_scope。
- **Bot 文件（2026-11-13）**：新增 `/api/bots/{id}/files` 与 `/files/content`（os.Root 防穿越、25MB 上限），Bot 详情新增「文件」Tab，支持目录导航与文件打开。生产实测根目录 `.superbot`/`artifacts`、artifacts 57 项、内层文件下载正常。顺带修复 API 未挂载 `/srv/superbot/data` 的问题：API 与 worker 现在共享工作区/附件/资料库数据。
- **审计日志（2026-11-13）**：迁移 0061 给 audit_logs 加 account_id 与索引；新增 CreateAuditLog/ListAuditLogs 与 `/api/audit-logs`（owner/admin）；ACL set/delete、工具审批 allow/deny、Bot 删除、Vault reveal 均写审计。新增 `/audit` 页面。生产创建并删除测试 Bot 后生成 bot.delete 记录，API 与页面均显示，测试日志已清理。
- **磁盘迁移（2026-11-13）**：发现系统盘 29G 使用 92%、150G 数据盘未挂载；已挂载 `/dev/vdb1` 到 `/data` 并写入 fstab，清理 17GB build cache/2.8GB 镜像，随后把 `/var/lib/docker` 与 `/var/lib/containerd` 迁移到 `/data` 并 bind mount。系统盘降至 12%（3.3G），数据盘 4%（5G），7 个容器与 Web 200 正常。
- **TTS 真实验证（2026-11-13）**：用 espeak-ng 搭了一个 OpenAI 兼容 `/v1/audio/speech` 服务（真实语音合成），临时配置生产 TTS 后 `/api/voice/speak` 返回 `200 audio/wav`，73KB，RIFF 头有效；同时修正 API 把 Provider 的 Content-Type 原样转发。随后还原 .env、停止服务、删除文件；TTS 回到 requires_config，WebRTC 实时通话仍未做。
- **STT 真实验证（2026-11-13）**：临时 faster-whisper OpenAI 兼容服务（HF_ENDPOINT 走镜像）成功转写 SAPI 生成的 WAV；生产 API 临时配置 STT 后 `/api/voice/transcribe` 返回正确文本，`/api/voice/status` 显示 configured。随后还原 .env、重启 API、删除容器与音频；STT 回到 requires_config，TTS 仍缺。
- **OIDC 真实登录（2026-11-13）**：临时启动 Keycloak 26（realm/client/user），临时配置生产 API 的 OIDC 环境变量，完整跑通 start→Keycloak 登录→callback→session→`/api/auth/me`，返回 `oidc@example.com / OIDC User`，证明 PKCE、state/nonce、JWKS/RS256 与 JIT 建号链路真实可用。随后还原 .env、删除 Keycloak、清理测试用户与账号；OIDC 状态回到 requires_config。
- **邮件客户端（2026-11-13）**：新增 `/email` 页面（收件箱/草稿/写邮件）与 `/api/email/{status,inbox,send,drafts}`；mailbox 支持自签名 TLS、MIME 正文/附件、线程归并；SMTP 发送重构为 mailbox.Send。GreenMail 端到端实测：收件 1 封→发信后 2 封、草稿创建/列表/删除、正文与附件解析、回复链 1 线程；UI 截图 `tmp/parity/ui-main/email-page.png`。测试连接/草稿/容器已清理。
- **IMAP 正文/附件（2026-11-13）**：mailbox 解析完整 MIME：text/plain 正文与附件元数据；GreenMail 投递 multipart（正文 + file.pdf）后，IMAPS 探针读回 `hello body text` 与 `{file.pdf application/pdf 7 bytes}`。此前回复链 3 封归并 1 线程也已验证。草稿仍未做，矩阵保持部分。
- **CalDAV 真实端到端（2026-11-13）**：连接器测试新增 PROPFIND；临时 Radicale + Linux 探针在 API 容器内实测 `Create` 成功、`List` 读回 2 个事件。真实测试还发现并修复了 Radicale 返回 `/xxx.ics/` 尾斜杠导致 List 过滤失败的 bug，新增回归测试。测试连接/容器/探针已清理；Google/Outlook OAuth 仍待验收。
- **会议实时字幕（2026-11-13）**：会议页新增「麦克风实时字幕」，用浏览器/Electron Web Speech 持续识别并追加 transcript，显示「正在听…」与 interim 文本；与 STT 分片转写并存，二者都按真实配置状态工作。生产桌面实测启动/停止与实时字幕条通过，证据 `tmp/parity/ui-main/meeting-live-captions.png`。
- **任务自动执行（2026-11-13）**：任务创建时若指定 `assignee_bot_id`，后台自动在 Bot 私聊派发执行指令并把任务置为 in_progress；Bot 通过现有 update_task 工具回写状态。生产实测「自动任务测试 2+2」从 todo 自动变为 done，Bot 回复 `2+2=4，任务已完成并把状态标记为 done。`；测试任务与消息已清理。
- **本机流量（2026-11-13）**：新增 `http_request` 设备任务与 `local_http_request` Bot 工具，通过已配对设备访问本机内网/本地服务；本地逐请求审批、任务签名复用、30s 超时与 256KB 响应上限。生产实测经本机代理访问生产 healthz 与百度均返回 200；测试设备/令牌已删除。完整 SOCKS 隧道未做，矩阵保持部分。
- **会议本地录制（2026-11-13）**：会议页「录制会议音频」用 `getDisplayMedia` 共享音频（Electron `audio: "loopback"`）；STT 已配置时每 20 秒自动分片转写并串行拼接实时字幕，未配置时单段录制后在停止时尝试转写；无音频轨明确提示。生产桌面实测开始/计时/停止正常，STT 未配置不伪造。证据 `tmp/parity/ui-main/meeting-recording.png`。
- **连接器令牌（2026-11-13）**：OAuth 回调保存 `refresh_token` 与 `expires_in`；`AccessToken` 在过期前 1 分钟自动刷新并回写加密令牌；删除连接器时对 GitHub/Slack 调用远端 revoke。`sqlc` 重新生成并补回被手改丢失的查询（消息过滤、会话删除、公共模板），Go 全量测试通过；真实 OAuth 凭据待验收。
- **Library 媒体（2026-11-13）**：资料库支持上传文件（≤25MB，存 `DataDir/library`）、鉴权下载 `/api/library/{id}/file`，以及图片/音频/视频/PDF 预览与普通文件下载；生产实测图片上传→列表→模态预览通过。证据 `tmp/parity/ui-main/library-preview.png`。
- **OIDC/SSO（2026-11-13）**：新增标准 OIDC 登录（discovery、JWKS、RS256 验签、issuer/audience/exp、PKCE、state+nonce 签名 Cookie、首次登录 JIT 建号 + 个人账户）；登录页按 `/api/auth/oidc/status` 显示 SSO 入口。未配置时生产返回 `requires_config`（已验证），OIDC 服务单测（真实 RSA/JWKS 验签 + PKCE URL）通过。
- **团队审查（2026-11-13）**：新增 Team Bot `submit_for_review` 工具与 `/extensions`「团队审查」队列，支持批准/驳回；Skill 审查接入批准/禁用与 Auto-Publish 规则。生产实测插入待审查记忆→页面显示→批准→列表消失且状态 approved，测试数据已删除。证据 `tmp/parity/ui-main/team-review.png`。
- **X 草稿（2026-11-13）**：`draft_x_post` 工具生成 X 草稿消息卡片（字数/280、发布按钮），`/api/x/publish` 通过 X API v2 发布；未连接 X 时生产返回 `requires_config`，不伪造发布成功。证据 `tmp/parity/ui-main/x-draft.png`。
- **电话/SMS（2026-11-13）**：Twilio REST 客户端（短信/语音 TwiML）、E.164 校验、`/api/phone/status|sms|call` 与 `send_sms`/`make_call` 工具；未配置时生产返回 `requires_config`，不伪造成功。真实号码验收需凭据。
- **PR 审查（2026-11-13）**：`review_pull_request` 工具读取 PR 元数据/文件/patch，配合 `github_api` 可提交 review；工具已注册，真实 GitHub 连接待验收。
- **恢复的运行时模块（2026-11-13）**：从生产源码回收本地缺失的 `message_crud.go`、`captures.go`、`summaries.go`、`team_reviews.go`、`tools/{admin,poll,proactive,github_review}.go` 与迁移 0051/0052；补齐 `pollOptionIDs`、消息/摘要/捕获/团队审查路由、主 Bot/投票/主动消息/GitHub review 工具注册；`queries/messages.sql` 与生成代码对齐。Go 全量测试通过。
- **身份/账户**：个人账户自动创建、跨用户邀请/接受、账户切换、DM 隔离（403）、Bot/Project/Skill/Template ACL 与 UI、分享链接过期/次数/撤销、join_project/share_project、Team Bot kind 均验证。
- **Agent Runtime**：异步审批 pause/resume（直接、群聊、子代理、多工具、5 分钟超时）；子代理嵌套 depth≤2、任务树、停止清理、运行中/审批期消息注入、孤儿恢复、完成通知。
- **Electron 本地代理**：shell、screenshot、clipboard、fs、app_list/app_open、location、audio_list；Ed25519 job 签名 + 设备验签、nonce 防重放、结果/artifact 签名、坏签名 401；NSIS/更新 feed/打包版启动。
- **文档/Canvas**：文档 CRUD、版本历史/恢复、SSE 协作与远端光标、在线人数；Canvas 画笔/矩形/椭圆/箭头/三角形/多边形（顶点拖动）/文本/图片、选择/拖动/删除、图层、属性/样式、8 向缩放、旋转手柄、撤销重做、PNG/SVG 导出。
- **日历/会议/语音**：本地日历 CRUD、CalDAV 客户端（mock 单测，真实外部未验收）；会议 transcript + 真实 DeepSeek 摘要/行动项、音频上传→STT mock UI；STT/TTS API 未配置 `requires_config` 与 mock 测试。
- **渠道/Team Bot**：Slack 发消息 + Events webhook（HMAC/challenge/message→通知）；Email SMTP 发送 + IMAP 收件代码（真实邮箱未验收）；Team Bot 创建/账户可见/不能设主 Bot。
- **功能与对照验证：已完成**（102 行中 86 已验证、0 部分、0 requires_config、0 未开始；Playwright 39 passed；自动更新、多实例协作、语言全量接入、Web Push（Firefox）、Passkey（localhost 与临时 HTTPS）、语义记忆、OIDC（Dex）、浏览器持久会话、工具调用合并、资料库自动收录、证据归档均已完成）。
- **发布后运维观察 / 外部条件（不列入功能未完成）**：72h soak 运行中（发布后稳定性观察，非功能交付）；真实 Slack/Teams/Email/Twilio/X 凭据与会议平台按用户要求暂不做；Chrome/FCM 出口受网络限制（Firefox 推送已验证）；生产域名+HTTPS（Passkey 协议已验证，80/443 入站被云防火墙拦截）；CA 签名按用户要求不做。
- 未验证/未配置项保持 `部分/未开始/requires_config`，不标记通过。

## 状态枚举

- `未开始`：没有对象模型/执行器。
- `部分`：有 DB/API/UI 之一，但任务闭环不完整。
- `已实现未验证`：代码完成，缺真实数据或对照证据。
- `已验证`：真实数据 + Web/Electron（如适用）+ 对照证据齐备。
- `requires_config`：实现存在，但需要用户提供真实凭据/域名。
- `platform_limited`：受 OS/平台限制，已明确降级。
- `not_in_scope`：明确不复刻的官方品牌/云能力。

## 账户与身份

| 功能 | Grok 入口 | superbot 入口 | 状态 | 目标阶段 | 证据/备注 |
|---|---|---|---|---|---|
| 邮箱密码登录 | Cursor 登录 | `/login` | 已验证 | 已有 | 生产实测 |
| Passkey 登录/注册 | 设置→安全密钥 | 设置→登录方式 | 已验证(HTTPS/协议级) | P7 | 2026-11-13 用 localhost + Chrome CDP 虚拟认证器完成真实 WebAuthn；2026-11-14 再用 **Cloudflare Quick Tunnel 的真实 HTTPS 域名**复测：注册 1 个凭据 → 清 Cookie → 安全密钥登录成功 → 删除 204，`webauthn=available`。过程中修复 **Caddy `{host}` 丢端口**（改 `{hostport}`）与 Chrome 虚拟认证器等待。生产 HTTP IP 仍非安全上下文，正式上线仍需自己的域名+TLS；证据 `tmp/parity/passkey/https-cloudflare.md`、`https-serveo-register.png`、`https-serveo-login.png` |
| 多账户保存/切换 | 设置→账户/切换账户 | 账户菜单 | 已验证(Web) | P2 | 创建/切换账户已实测：新账户 bots/sections/primary 为空；切回原账户数据恢复；资源读路径已切 account |
| OIDC/SSO 登录 | Cursor OAuth | /api/auth/oidc/* + 登录页 SSO | 已验证(自托管 Keycloak/Dex) | P2 | 真实 Keycloak 端到端通过（JIT 建号）；**生产自托管 Dex 也已端到端验证（2026-11-14）**：登录页 SSO → Dex 登录/授权 → callback → `/api/auth/me` 返回 admin@superbot.local（与既有管理员同邮箱，未产生重复账号）。Discovery/JWKS/RS256、PKCE、state+nonce 签名 Cookie 全部生效；证据 `tmp/parity/oidc/dex-login.png` |
| 账户成员/邀请/角色 | 设置→账户/人员 | 账户菜单 | 已验证(Web) | P2 | 创建/切换账户、成员列表、邀请链接、接受邀请实测；DM direct_user_id 已隔离；资源级 ACL 未做 |
| 资源 ACL/共享 | Bot/项目/Skill/模板分享 | Bot 访问 / 项目管理 / 扩展页 Skill 访问 / 分享链接 / Bot 模板 | 已验证(Web) | P2 | Bot/项目/Skill/模板 ACL、分享链接、join/share project 均已实测；跨账户/模板市场仍未验收 |
| 审计日志 | 企业管理 | `/audit` | 已验证(Web) | P2 | 迁移 0061 加 account_id；ACL 变更/工具审批/Bot 删除/保险库查看写入 audit_logs；`/api/audit-logs` 仅 owner/admin 可读，`/audit` 页面展示时间/动作/对象/操作者。生产实测 Bot 删除产生记录并展示 |
| 首次运行 onboarding | 全屏引导 | 空账户主界面 | 已验证(Web) | P2 | 新账户显示「连接一个模型」→ 配置 Provider；配置后创建主 Bot |
| 个人资料/主 Bot | 账户/主 Bot | 信息栏 Bot 设置（悬浮铅笔）+ 名称内联重命名 | 已验证(Web) | P0B/P2 | 入口实测：详情栏名称 hover 铅笔打开「Bot 设置」，含名称/标题/描述/Provider/模型/可见性/分组/置顶/导出模板/主 Bot；点名称内联重命名（Grok 同款交互） |

## Bot 与群聊

| 功能 | Grok 入口 | superbot 入口 | 状态 | 目标阶段 | 证据/备注 |
|---|---|---|---|---|---|
| 轻量创建 Bot | +→New Bot | 侧栏+/NewBotDialog | 已验证 | 已有 | 名称/形状/颜色 |
| 头像形状/颜色 | New Bot/头像编辑 | BotAvatarPicker | 已验证 | 已有 | 15 形状/11 色 |
| 头像上传/生成 | 上传/生成 | 头像面板「上传 / 生成」 | 已验证(Web) | P0B/P7 | 新增 `POST/GET/DELETE /api/bots/{id}/avatar` 与 `POST /avatar/generate`：真实 PNG 上传→侧栏/头部/详情三处 `<img>` 渲染（naturalWidth 1024）→移除回退形状头像；真实生图 1024×1024 PNG 161KB 生成并展示；非图片 400、未配置生图返回 requires_config。文件存 `DataDir/avatars/{botID}`，版本号防缓存 |
| Bot 资料/标题/描述 | Bot 设置 | BotInfoDialog | 已验证(Web) | P0B | 侧栏右键/名称铅笔可打开；含 Provider/模型 |
| Bot 模型/Provider/参数 | Bot 设置 | Bot 设置弹窗 Provider/模型/参数 | 已验证(Web/API) | P0B | 弹窗可选 Provider/模型，参数以 JSON 编辑器编辑；保存资料后补调 `PATCH /api/bots` 全量载荷（修复模型选择丢失）。生产实测参数 `{temperature:0.3, top_p:0.9}` PATCH 200 并读回一致；当前只配置了一个模型，无法做换模型对照（接口路径已验证） |
| 主 Bot 设置/更换 | 账户/Bot | Bot 设置弹窗「设为主 Bot / 当前主 Bot」 | 已验证(Web) | P0B/P2 | 生产实测 `POST /api/account/primary-bot` 切换主 Bot 后 `/api/auth/me` 立即变更，再切回原 Bot；删除主 Bot 返回 409 并提示先更换（明确守卫，不是死锁） |
| Bot 可见性 OWNER/TEAM | Bot 访问 | BotInfoDialog 可见性 | 已验证(Web/API) | P2 | Bot 设置弹窗提供 owner/account 选择，保存走 `PATCH /api/bots/{id}/profile`；生产实测切到 owner 再切回 account，响应与读回均为对应值。资源级 viewer/editor/runner 授权由下一行覆盖；单用户范围下不展开跨账户可见性 |
| Bot 访问人员/角色 | 人员/访问 | BotInfoDialog 访问页 | 已验证(Web) | P2 | viewer/editor/runner 授权/撤销已实测 |
| 群聊创建/成员 | +→New group chat | NewGroupDialog | 已验证 | 已有 | 最多 6 |
| 群聊 @ 驱动/链式 | 群聊 | engine group | 已验证 | 已有 | 修复前缀边界 |
| 群聊成员增删 | 群成员 | `/teams` 编辑弹窗 | 已验证(Web) | P0B | 复用 room、≤6 限制；生产桌面实测：创建 2 人群 → 编辑加「生产桌面助手」→ 保存后 API 返回 3 人 → 再编辑移除 → 保存后 2 人；测试群已删除（`PUT /api/teams/{id}/bots` 为保存路径） |
| Bot 跨私信 | 主聊天折叠卡 | 主聊天折叠卡 → AgentThreadPanel | 已验证(对照) | P3 | 与 Grok 1.0.1 对照：Grok 的「打开与 X 的交流」弹窗 DOM 内 textarea/input 为 0（只读），superbot 同为只读；生产实测 Bot 调用 send_to_agent → 主聊天出现「已发消息给 测试」折叠卡 → 打开面板显示双方消息、时间与「Bot 之间的私信 · 只读」；证据 `tmp/parity/ui-main/superbot-agent-thread.png`。测试线程/消息已清理 |
| 子代理 | Subagent 卡/工具 | spawn/check/stop + 详情列表 | 已验证(Web/API) | P3 | 生产实测：主 Bot 调 spawn_subagent 派发「只回复 SUBAGENT-OK」，子任务状态 succeeded、result=SUBAGENT-OK，主会话收到完成汇报；测试数据已清理 |
| 主 Bot 创建 Bot/群/例程 | 主 Bot 工具 | create_agent/list_agents/create_group/create_routine/set_primary_bot | 已验证(API) | P3 | 生产让主 Bot 真实调用：`list_agents` 返回账户 4 个 Bot；`create_agent` 建成「验证助手-ADMIN2」；`create_group` 用**名称**建群成功（team 12952752）；`create_routine` 用名称解析出 bot_id 并创建每天 09:00 例程（next_run 2026-11-14T09:00+08:00）。顺带修复：`create_group`/`create_routine` 现在接受 Bot 名称（此前只收 UUID，模型拿不到 ID 会失败），新增 `list_agents` 供主 Bot 枚举。测试 Bot/群/例程/消息已全部清理 |
| Team Bot 内部/发布 | Team Bot 设置 | kind=team 创建 + 服务账号令牌 | 已验证(Web/API) | P6 | 内部 Team Bot、服务账号令牌（建令牌→无 Cookie 投递→Bot 回复→撤销后 401）已验证；MS Teams/Slack 安装与发布按用户要求暂不做（not_in_scope） |

## 消息与主动工作

| 功能 | Grok 入口 | superbot 入口 | 状态 | 目标阶段 | 证据/备注 |
|---|---|---|---|---|---|
| 流式回复/工具调用 | 聊天 | SSE/engine | 已验证(Web) | 已有 | 工具时间线按运行合并为一个可折叠「N 个工具调用」组（单个工具仍直接显示卡片）；审批结果系统消息不再单独占用聊天行。生产实测 browser_open+browser_snapshot 显示「2 个工具调用」，展开可见每个工具的参数/结果/widget |
| 回复/复制/请求 ID | 消息右键 | ChatMessage | 已验证 | 已有 | 224px 菜单 |
| 表情回应 | 消息右键 | reactions | 已验证 | 已有 | 服务端持久化 |
| 消息编辑/删除/置顶 | 消息菜单 | 消息右键菜单 | 已验证(Web) | P3 | 编辑（仅本人消息）/置顶/删除已实测：编辑写入 edited_at、置顶写 pinned_at、删除软删并从列表隐藏；生产端到端通过 |
| 消息/会话全文搜索 | Cmd+K/会话查找 | `/api/search` + 会话内查找 | 已验证(Web) | P0B/P3 | 生产 API 返回 messages，搜索 UI 深链到 `?conv=&message=` 并在打开会话后滚动+高亮目标消息；新增**会话内查找**：Ctrl/Cmd+F 打开查找条，实时计数 1/N，Enter/Shift+Enter 或 ↑/↓ 跳转，Esc 关闭。系统消息与跨私信卡片补上 `data-message-id`，因此旧审批/系统消息也能被定位。实测查询「工具审批」命中 10 条，Enter 后 transcript scrollTop 0→175 |
| Poll 投票 | SendPoll | 消息内投票卡 | 已验证(Web) | P3 | send_poll 工具 + 单选/多选投票卡 + 计票/百分比；生产实测投「面条」后 API 与 UI 同步 1 票 100% |
| 主动消息/偏好 | SetProactiveMessaging | Bot 设置→主动消息 + send_to_user/set_proactive_messaging | 已验证(Web/API) | P3 | 新增 `PATCH /api/bots/{id}/proactivity` 与 Bot 设置里的开关。生产实测：界面关闭 → `{'enabled': false}` 落库 → Bot 调 `send_to_user` 返回「主动消息已在 Bot 设置中关闭」；开启 → 工具投递成功。顺带修复**错投**：send_to_user 原来自己按 created_by+bot 查找私聊，可能落到另一条并行 DM；现在优先投递到当前会话（direct 且属于该 Bot），实测 proactive 消息出现在当前私聊且 metadata `kind=proactive`。测试消息与错投会话已清理 |
| 动态未读/深链 | 侧栏圆点/动态 | `/activity` | 已验证(Web) | P0B | conversation/message 深链 + 聚焦动画；More unread 菜单已实现 |
| 新消息胶囊 | 聊天 | 已实现 | 已验证(Web) | P0B | 生产桌面实测：聊天滚到顶部后发送消息，SSE 到达时出现「2 条新消息」胶囊；点击后平滑回到底部且胶囊消失（scrollTop 2289.6/2314）；测试消息已软删。证据 `tmp/parity/ui-main/new-message-pill.png` |
| 例程 cron/@every | 动态→即将进行 | BotRoutineList | 已验证 | 已有 |  |
| 例程 once/flexible | 例程详情 | BotRoutineList | 已验证(Web) | P0B/P3 | once 执行后自动停用（已有）；flexible 落为真实语义：准点后可执行，超过 15 分钟才被发现则跳过本次、避免过期工作，单元测试 `TestShouldSkipFlexible` 覆盖；UI 文案同步。注：Grok 无独立 precheck 字符串，其等价能力是 test-run + Auto-review（已具备），不虚构 precheck |
| 例程编辑/测试运行 | 例程菜单 | BotRoutineList | 已验证(Web) | P0B | 编辑/时区/once/flexible；测试运行 API/按钮已接 |
| 例程事件触发 | email/Slack/Teams/webhook | 例程编辑器 Webhook + `email:` 调度 | 已验证(Web/API) | P3/P5 | webhook 已验证（公开 token 入口）。**email 触发已验证**：迁移 0068 `email_trigger_seen` 去重，worker 每 60s 轮询 IMAP，`email:`/`email:关键词` 匹配 from+subject；GreenMail 实测投递「发票 2027-01」→ 60s 内在当前私聊生成一次运行，再等一个周期仍仅 1 次；同时修复 email 例程被定时轮询反复领取、例程运行投递到无 direct_user_id 的并行 DM、忙碌唯一约束无限重试。Slack/Teams 事件映射需真实企业租户 |
| 通知中心/声音/推送 | 设置/OS | 页头通知中心 + 设置→通知 | 已验证(Web/真实推送) | P5 | 页头通知、Electron 原生通知、桌面开关、声音/音量/分类开关已上线。**Web Push 已实现**：迁移 0066 `push_subscriptions` + `notifications.pushed_at`，VAPID 密钥自动生成并持久化到 `DataDir/push-vapid.json`，`/api/push/{status,subscribe,unsubscribe,test}`、worker 每 15s 投递未推送通知、Service Worker `push/notificationclick`、设置页开关与测试按钮。按用户要求走 HTTP：Google Chrome 加 `--unsafely-treat-insecure-origin-as-secure` 后实测订阅成功（FCM endpoint，服务端 201 存储）；但生产服务器**访问不了 fcm.googleapis.com（i/o timeout）**，测试推送返回 502 并带真实原因，不伪造成功。**已用 Firefox/Mozilla autopush 完成真实送达验证（2026-11-13）**：本机 Firefox 经 localhost 安全上下文订阅 → 服务端 201 落库 → 测试推送 `sent=1` → Firefox 日志 `PushService: receivedPushMessage` / `notifyApp` / `notificationForOriginShown`（SW 已调用 showNotification）→ 浏览器 ack code 100；证据 `tmp/parity/notifications/webpush-firefox.log`。Chrome 仍因服务器到 FCM 网络超时不可达，Electron 内 Chromium 无推送服务，桌面通知走原生通道 |

## 记忆与上下文

| 功能 | Grok 入口 | superbot 入口 | 状态 | 目标阶段 | 备注 |
|---|---|---|---|---|---|
| Bot/Team 记忆 | Knowledge | Bot 设置→记忆 + 团队记忆工具 | 已验证(Web/API) | 已有/P3 | bot/team 两 scope 均可用：生产在团队会话让 Bot 调 `memory_save(scope=team)` 保存「团队记忆-验证123」，随后 `memory_search` 命中 `[team] 团队记忆-验证123`；测试消息与记忆已清理 |
| 用户级记忆 | 账户记忆 | 设置→Bot 记忆→「我的记忆」 | 已验证(Web/API) | P3 | 新增 `/api/memories`（GET/POST/PUT/DELETE，scope=user、按账户+用户隔离）；BotMemory 增加「Bot / 我的记忆」切换与新增/编辑/删除；召回链路在 direct 会话中把用户级记忆并入（`conv.DirectUserID`）。生产实测：API 增删改查、UI 列表显示既有记忆；向 Bot 问「蓝鲸交付物」→ 仅凭用户记忆回答「安装包」 |
| 记忆 CRUD/排序 | Knowledge | Bot 设置→记忆 | 已验证(Web) | P3 | 新增 `PUT/DELETE /api/bots/{id}/memories/{memoryID}`：编辑时重算向量并按当前 embedding 模型写回；UI 每条记忆支持编辑（内联 textarea）、删除（确认框）与「最新在前/最早在前」排序。生产实测：界面把「记忆-已编辑内容」改为「记忆-界面编辑完成」→ API 读回一致；删除后 API 列表消失；编辑后 DB `embedding_model=local-hash-1024` 已刷新 |
| 真实语义召回 | recall_memory | 记忆服务 + 自托管 embedding | 已验证(API/向量检索) | P3 | 新增自托管 `embedding` 服务（`deploy/images/embedding`，fastembed + `intfloat/multilingual-e5-large` 1024 维，模型从 hf-mirror 缓存到卷）。生产实测：服务 healthy dim=1024；`reembed-memory` 将 6 条记忆迁到 `remote:intfloat/multilingual-e5-large`；查询「部署包放在哪个文件夹」在 pgvector 最近邻中命中「交付物保存在 /srv/releases 目录」0.8816，高于无关记忆；`/api/capabilities` 的 `memory_semantic` 为 available。未配置 key/服务时仍显式 `requires_config` 并用本地哈希 |
| 记忆导入其他 AI | 设置→导入 | Bot/我的记忆导入（粘贴或文件） | 已验证(Web/API) | P3/P7 | 新增 JSON 导出解析：数组字符串、对象 content/text/memory/rule(s) 递归提取，另保留按行导入；用户记忆新增「从文件导入」(.json/.txt/.md)。生产实测：ChatGPT 风格 JSON（memories/rules）导入 3 条、文本行导入 2 条，均落 user scope，测试数据已删除 |
| 会话摘要/工作笔记 | 上下文管理 | `GET/POST /api/conversations/{id}/summary` | 已验证(API) | P3 | 生产真实摘要：POST → 201、794 字、`up_to_seq=298`；随后发消息触发 Bot 回合，worker 日志出现 `conversation summary in prompt up_to_seq=298 chars=794`，且上下文按 `ordinal <= up_to_seq` 过滤旧消息；测试消息与摘要行已清理。工作笔记是内置技能里的长任务规范（非独立表）。证据：worker 日志 + `tmp/summary-test.json` |
| 稳定前缀/上下文预算 | 内置 | 动态预算裁剪 | 已验证(单测/代码) | P3 | 稳定 system 前缀（Bot 提示+技能+记忆+摘要）保持不变；预算改为 `context_window - max_output - 2000`，下限为窗口一半/至少 4000，始终保留 system 与最近 6 条；超预算从最旧消息裁剪。新增 `TestTrimMessagesToBudget` 覆盖大预算不裁剪、超预算保留 system+最近 N 条 |

## Vault、电脑与本地

| 功能 | Grok 入口 | superbot 入口 | 状态 | 目标阶段 | 备注 |
|---|---|---|---|---|---|
| Vault 登录/联系信息 | 保险库 | 设置→保险库 | 已验证 | 已有 | 加密/不回显 |
| 支付卡结构化/导入 | 支付/导入 | 通用字段 | not_in_scope | P4 | 用户 2026-11-13 明确：支付卡结构化/导入暂不做 |
| Vault 编辑 | 条目菜单 | 设置→保险库 | 已验证(Web) | P0B | 生产端到端：设置→保险库→条目「编辑」→改名→保存，API 读回 `vault-ui-…-edited`；密码不回填（留空不覆盖），`查看` 走 GET reveal 返回明文，删除 204，测试条目已清理。支付结构化字段仍待 P4 |
| Bot 安全填表 | Vault + 浏览器 | `vault_fill_credentials` | 已验证(Web/API) | P4 | 新增工具：按条目名称取凭据 → CDP 打开保存的登录页 → 原生 setter 填入用户名/密码（可选提交）。凭据仅在服务端→浏览器间传递，工具结果只返回 `{username_filled,password_filled,submitted}`，不进模型上下文；强制审批；目标站点必须与条目保存的 Host 一致，防止外传。生产实测：容器内登录页 + Vault 条目，工具返回两项 true，页面脚本回报 `{"u":"fill-user","p":"fill-pass"}`；修复 CDP Host/WS IP 连接 |
| 1Password Connect | 连接应用 | token 占位 | not_in_scope | P4 | 用户 2026-11-13 明确：1Password Connect 暂不做 |
| Bot 云电脑/VNC | 动态→电脑 | ComputerPanel | 已验证 | 已有 | Docker 持久容器。**browser 电脑完整链路（2026-11-14）**：`browser_open` → `browser_snapshot`（拿到搜索框 [13]/按钮 [14]）→ `browser_type` 输入“今日新闻” → `browser_click` → `browser_snapshot` → `browser_screenshot`；截图 222,385 字节、1050×793、17,840 种颜色（非白图），结果页标题「今日新闻_百度搜索」。期间修复：Bot `computer_profile=none` 导致无 browser 工具、auto-sleep 忽略 `workspaces.last_used_at` 每 10 秒杀掉活跃容器、浏览器会话每轮重建并跳 about:blank、`--disable-software-rasterizer` 导致纯白截图、容器重建后 Chromium `SingletonLock` 阻止启动。证据 `tmp/parity/browser/` |
| 通用 Computer 工具 | Computer | 桌面工具集 | 已验证(API/容器) | P4 | 已有截图/点击/输入/按键；新增 `desktop_scroll`（滚轮）、`desktop_list_windows`（可见窗口标题）、`desktop_window_action`（focus/minimize/maximize/close，按标题子串）。工具注册单测通过；在 `superbot/computer-desktop` 容器内用同一 xdotool 命令实测能列出「Desktop」窗口（exit 0）。完整 GUI 自动化路径未逐应用验收 |
| 电脑更新/重置/快照 | 设置→电脑 | 更新/重置/快照/自动休眠 | 已验证(桌面/API) | P4 | 更新/重置走 workspace_actions（停容器→重建/清空）。**快照已验证**：迁移 0069 `computer_snapshots` + 动作 `snapshot/restore`，工作区 tar.gz 归档、恢复时停容器→清空→解包→重建；临时 Bot 实测快照 126B、改文件后恢复回 `original-content`。**自动休眠已验证**：30 分钟无活动自动 stopped（2h 旧活动 8s 内停止）。UI 电脑面板新增创建快照/快照列表/恢复/删除 |
| 本地设备配对/执行 | 本地 daemon | `superbot device` + Electron 内置代理 | 已验证(桌面) | P1/P4 | Ed25519 job 签名、设备验签、nonce 防重放、结果/artifact 签名、坏签名 401；NSIS/打包版启动 |
| 本地文件/App/剪贴板/定位 | 本地工具 | Electron 设备代理 | 已验证(桌面) | P4 | shell/screenshot/clipboard_read/write/fs_read/write/app_list/app_open/location/audio_list |
| 本机流量转发 | egress tunnel | 设备 WebSocket 隧道 + HTTP CONNECT 代理 | 已验证(HTTP/HTTPS 隧道) | P4 | 容器经 `api:8081` HTTP 代理或 `api:8082` SOCKS5 → 设备 WebSocket 隧道 → 设备本机网络：实测 http 访问 Windows 本地服务返回 `tunnel-ok-from-windows`，https 访问 baidu 返回 200，SOCKS5 同样返回 `socks-ok-from-windows` 与 200；每条隧道连接在设备逐条审批；另有 local_http_request 工具 |
| Mac Mail/Notes/Obsidian/iOS | 本地工具 | 无 | platform_limited | P4 | 仅 macOS/伴侣 App |

## 渠道、邮件、日历、会议

| 功能 | Grok 入口 | superbot 入口 | 状态 | 目标阶段 | 备注 |
|---|---|---|---|---|---|
| GitHub/Slack/Notion 基础 | 连接应用 | 连接器/工具 | 已验证 | 已有 | token/OAuth |
| 连接器 refresh/revoke/webhook | 连接应用 | OAuth 回调 + /api/connectors | 已验证(协议级) | P5 | Slack Events 生产实测（自建 connector + signing_secret）：`url_verification` 原样回 challenge；错误签名 401；时间戳超 5 分钟 401；合法 `event_callback/message` → 200 且生成通知，标题随账户语言输出（zh「Slack 消息」/ en「Slack message」）。删除连接器 204 并走远端 revoke（无真实 Slack 凭据时远端失败被忽略，不伪造成功）。OAuth refresh_token/expires_in 持久化与提前刷新仍在代码路径，真实 OAuth 刷新需凭据验收 |
| Slack 收发/线程/Canvas | Slack | 仅 post | not_in_scope | P5 | 用户 2026-11-13 明确：Slack 真实工作区暂不做（已有发消息/webhook 协议级实现保留） |
| Teams 账户/频道/会议 | Teams | token 占位 | not_in_scope | P5 | 用户 2026-11-13 明确：Teams/Azure 暂不做 |
| Email SMTP 发信 | 邮件 | send_email | 已验证 | 已有 |  |
| Email IMAP 收件/线程/草稿 | Bot 邮箱 | `/email` 页面 + read_email/send_email | 已验证 | P5 | 自托管 GreenMail 真实 IMAPS/SMTP：收件、正文、附件元数据、3 封回复链归并线程、草稿 CRUD、发信后收件箱出现新邮件；`/email` 收件箱/草稿/写邮件 UI 实测 |
| 电话/SMS | 手机号 | /api/phone/* + send_sms/make_call | not_in_scope | P5 | Twilio REST 实现保留（短信/语音、TwiML 转义、E.164 校验、未配置返回 requires_config）；用户 2026-11-13 明确外部凭据类暂不做，故不纳入本轮验收 |
| X 草稿/连接 | X | draft_x_post + 草稿卡 + /api/x/publish | not_in_scope | P5 | 草稿卡片与 X API v2 发布实现保留；用户 2026-11-13 明确外部凭据类暂不做，未配置时仍如实返回 requires_config；X Premium 不伪造 |
| iMessage | macOS | 无 | platform_limited | P5 | 仅 macOS |
| 日历同步 Google/Outlook | 日历 | CalDAV 客户端 + 连接测试 | not_in_scope | P5 | CalDAV 自托管已端到端验证（临时 Radicale 真实 PROPFIND/Create/List，修复尾斜杠 bug）；Google/Outlook OAuth 按用户要求暂不做 |
| 会议加入/转写/摘要 | 会议 | `/meetings` | not_in_scope | P5 | 本地系统音频录制、实时字幕、真实 STT/摘要已验证；平台自动加入/发言按用户要求暂不做，能力状态 meetings=degraded |
| 会议聊天/发言 | 会议 | 无 | not_in_scope | P5 | 用户 2026-11-13 明确会议平台暂不做 |

## Library、文档、任务、模板

| 功能 | Grok 入口 | superbot 入口 | 状态 | 目标阶段 | 备注 |
|---|---|---|---|---|---|
| Library 页面/文件/媒体 | 资料库 | BotDetailPanel 资料库 Tab | 已验证(Web/API) | P0B/P6 | 文件上传（≤25MB，DataDir/library）、鉴权下载、图片/音频/视频/PDF 预览与文件下载；**自动收录（2026-11-14）**：Bot 回合结束后扫描 workspace 的图片/音频/视频、文档（md/txt/csv/pdf/office）与简单代码（py/go/js/ts…），聊天附件与新建文档也自动入库；去重、单次最多 50 个、跳过 `call_*.txt` 工具原始输出。生产实测「测试使用bot」资料库自动出现 10+ 张截图与 6 个 py 脚本，且无工具文本噪声；文档与图片附件自动收录验证通过。预览：代码扩展名加入文本白名单并用 `/preview` iframe，文档自动项更新/删除会同步资料库；历史 E2E 孤儿资料项已清理 |
| 文档/PDF/Word/Excel 预览 | 资料库 | 资料库预览 | 已验证(Web) | P6 | `/api/library/{id}/preview` 支持 DOCX（段落/表格）、XLSX（sharedStrings/多 sheet/单元格坐标）、**PPTX**（按 slide 顺序输出标题/要点，文本全转义）、文本/CSV/JSON；CSP 禁脚本。单测 DOCX/XLSX/PPTX；生产上传真实 docx/xlsx/pptx 均正确渲染，PPTX 界面 iframe 显示「第 1 页 季度总结 / 第 2 页 下一步」。删除资料会同步清理磁盘文件，无孤儿 |
| Canvas/文档编辑 | 页面 | `/documents` | 已验证(Web) | P6 | 画笔/矩形/椭圆/箭头/三角形/多边形/文本/图片、图层、8 向缩放、旋转、撤销重做、PNG/SVG 导出 |
| Bot 文件/Store | Bot files | Bot 详情→文件 Tab + `/store` | 已验证(Web) | P6 | 工作区文件浏览/目录导航/下载已验证；Bot Store 公开模板列表/一键安装/安装计数已验证；付费交易为 not_in_scope |
| Skill 创建/批准/绑定 | 技能 | `/extensions` | 已验证 | 已有 | 无 update/版本 |
| MCP HTTP/stdio | 连接应用 | `/extensions` | 已验证 | 已有 | 无 OAuth/市场 |
| MCP Apps/Widget | MCP App | 工具卡内嵌 iframe | 已验证(Web/API) | P6 | MCP 工具结果 `_meta.ui.resourceUri` → `resources/read` 取 HTML → 事件携带 `widget_html` → 聊天工具卡用 `sandbox="allow-scripts"` 的 iframe 渲染，并注入 `window.__MCP_TOOL_RESULT__`；凭据/源码不经过模型。同时补齐**聊天工具时间线**（此前 ToolCard 未渲染）。生产用 `mcp-echo` 的 `echo_widget` 实测：工具卡出现、iframe srcdoc 含 `Echo MCP App` 与 `app-widget-4`、sandbox 属性正确；历史运行也能从 run 事件恢复 widget。测试 Bot/MCP/会话已清理 |
| Bot 模板导入/导出 | 分享 | 分享链接 / Bot 设置→导出模板 / 导入模板 | 已验证(API) | P0B/P6 | 模板新增 `memories` 与 `mcp_servers`（此前只有 profile/skills/routines）：导出 bot 记忆（≤200 条）与已绑定 MCP（名称/传输/URL/命令/参数，**不含 env/headers 密钥**）；导入时重建记忆、按名称复用或创建 MCP 并绑定。生产实测：给 Bot 加 1 条记忆 + 绑定 1 个 MCP → 导出模板含 `memories:1 / mcp_servers:1` → 导入新 Bot → 新 Bot 记忆与 MCP 绑定均存在；测试 Bot/记忆/MCP 已清理 |
| 模板市场/发布/团队审查 | Marketplace | 模板注册表 + 团队审查队列 | 已验证(Web) | P6 | 模板发布/导入已上线；Team Bot `submit_for_review` 工具 + `/extensions` 审查队列（批准/驳回、Skill 批准/禁用与 Auto-Publish）已实测 |
| Tasks 页面/任务板 | Tasks | `/tasks` | 已验证(Web) | P6 | 列表/看板切换、五状态列、拖拽改状态（乐观更新、失败回滚、视图持久化）、项目/成员 ACL |
| 任务/项目工具 | CreateTask/JoinProject/ShareProject | 任务页 + 自动派发 | 已验证(Web) | P6 | 项目创建/删除/成员授权/任务选择项目/join_project/share_project 已实测；新任务分配 Bot 后自动在私聊派发执行、Bot 用 update_task 回写 done；生产实测 2+2 任务自动完成并汇报 |
| 云代理/PR 审查 | Cursor agent | review_pull_request + github_api | not_in_scope | P6 | 用户 2026-11-13 明确：GitHub 凭据类暂不做（工具保留） |

## 计费、设置、桌面、语音

| 功能 | Grok 入口 | superbot 入口 | 状态 | 目标阶段 | 备注 |
|---|---|---|---|---|---|
| 用量统计 | 用量与账单 | 设置→用量 | 已验证 | 已有 | 真实 token 成本 |
| 套餐/配额/账单/credits | 用量与账单 | 设置→用量→套餐与额度 | 已验证(Web) | P7 | 自托管套餐、credits 余额、credit_ledger 手动调整、周预算/已用展示与 Agent 强制限额；生产实测充值 ¥10 与账本展示。真实支付/发票为 not_in_scope（自托管不接 Stripe） |
| 主题/强调色 | 外观 | 设置→外观 | 已验证 | 已有 | 下拉菜单 |
| 语言/i18n | 通用→语言 | 设置→通用→语言 | 已验证(Web) | P7 | zh/en 词典 500+ 键、localStorage + 账户设置双持久化、html lang 切换；主壳层/任务/Bot Store/文档/Canvas/扩展/设置/日历/邮件/审计/Bot/群聊/动态页均已接入。英文模式逐页扫描：/documents /store /calendar /audit 中文 0 项，/bots /teams /extensions 仅剩 Bot 名/描述等内容数据，/email 0，/settings 仅剩语言自称「中文」与 OS 麦克风设备名；内置技能描述已英文；帮助弹窗（快捷键/安装/自托管）已接入词典且版本改为构建元数据。用户生成内容（Bot 名/消息/模板描述）不翻译 |
| 麦克风/时区/硬件加速 | 通用/系统 | 设置→通用 | 已验证(Web/桌面) | P1/P7 | 麦克风设备枚举/持久化并用于语音消息录音；时区选择服务端按账户持久化并作为例程默认时区；硬件加速开关写入 desktop-settings.json、启动时禁用；更新通道 stable/beta 持久化 |
| 拼写检查 | 通用 | 设置→外观 | 已验证 | 已有 |  |
| 通知声音/偏好 | 通用/通知 | 设置→通用→通知 | 已验证(Web/桌面) | P5/P7 | 服务端按账户持久化；桌面通知/声音/音量/分类开关实测读写；Electron 桥接沿用 |
| 数据共享 | 通用→数据共享 | 设置→通用→数据共享 | 已验证(Web) | P7 | 默认关闭的匿名前端错误报告：开启后仅写入本机 diagnostic_events（无消息内容、不上传第三方），关闭时不落库；生产实测开关与上报行为通过，测试数据已清理 |
| 硬件安全密钥管理 | 安全密钥 | 设置→安全密钥 | 已验证(Web) | P7 | 注册/列表/计数/重命名/删除；迁移 0060 增加名称；生产实测列表、重命名、删除 API 与设置 UI |
| 更新/通道/自动更新 | 更新 | 设置→更新 | 已验证(桌面) | P1/P7 | 版本来自桌面包 `package.json`（实测 v0.1.0，不再写死）；新增 IPC `getAppInfo/checkForUpdates`，设置页显示「当前版本 + 已安装/开发模式」和真实「检查更新」按钮（开发模式如实返回「开发模式无法检查更新」）；更新通道持久化，打包态切换通道会立即设置 `autoUpdater.channel` 并触发检查。NSIS 0.1.0→0.1.1 升级与 feed 此前已实测 |
| Electron 窗口/托盘/菜单 | 桌面 | desktop/ | 已验证(桌面) | P1 | 无边框窗口 + 右上角 46×32 自绘 caption（− □ ✕，与 Grok 同规格）、托盘菜单、原生菜单、窗口状态；真实鼠标拖拽/最大化/还原实测通过 |
| 桌面通知/深链/单实例 | 桌面 | Electron 通知 + `superbot://` | 已验证(桌面) | P1 | 主进程注册 `setAsDefaultProtocolClient("superbot")`、`requestSingleInstanceLock` + `second-instance` 聚焦窗口；IPC `showNotification` 实测返回 true 并弹出 Windows 通知；打包/升级/卸载此前已实测 |
| 捕获 overlay/教任务 | Composer+ | Composer「教它做一项任务」 | 已验证(桌面) | P1 | 真实 getDisplayMedia 录屏、1.5s 关键帧（≤8）、10 分钟上限、视觉模型生成 Markdown Skill 草稿、插入输入框/保存为 Skill；生产桌面实测生成成功 |
| 原生自动更新/签名 | 桌面 | electron-updater + `/desktop/` feed | 已验证(桌面) | P1/P8 | 版本统一 0.1.2 并发布到 `/desktop/`；补 `stable.yml`/`beta.yml`（Windows 按 `<channel>.yml` 取元数据）；实测 0.1.1 静默安装 → 启动自动下载 0.1.2（sha512 匹配）→ 退出静默安装 → 落盘 0.1.2，0.1.2 打包态 `checkForUpdates` 返回 latest；临时安装已卸载。CA 签名按用户要求不做 |
| 语音输入/通话/转写 | 动态→语音 | WebRTC 数据通道 + 服务端 STT/TTS | 已验证(Web/桌面) | P7 | 真实 WebRTC 链路通过：单端口 UDP 40000 ICE、SDP offer/answer、DataChannel 打开；假麦克风 PCM→DataChannel→服务端 faster-whisper 转写→转写文本回传→发送给 Bot 并收到回复。**STT/TTS 已自托管验证（2026-11-13）**：faster-whisper-server + Piper `zh_CN-huayan-medium`；`/api/voice/speak` 返回 141KB WAV（22050Hz），同一 WAV 经 `/api/voice/transcribe` 转写为「一好这是超级机器人与银河乘测室。」（口述「你好，这是超级机器人语音合成测试。」），`/api/capabilities` voice=available。说话人分离未做（Grok 无对应可见功能） |
| 语音消息 | 语音 | Composer「录制语音消息」 | 已验证(桌面) | P7 | getUserMedia + MediaRecorder 录音（最长 5 分钟），上传为 audio 附件并随消息发送；消息内 audio 播放器；生产桌面实测录制/发送/播放器渲染通过 |
| 视觉生成/试穿 | 购物 | generate_image 工具 | 已验证(Web) | P7 | 文本生图 + 参考图（图生图），异步任务轮询、图片落库到 Bot 资料库；生产实测生成 1024×1024 PNG 87KB 并可下载。虚拟试穿为图生图用法，无专用预设 |

## 外部/生活服务

| 功能 | Grok 入口 | superbot 入口 | 状态 | 目标阶段 | 备注 |
|---|---|---|---|---|---|
| 航班/餐厅/购物 | 官方连接器 | 无 | not_in_scope | P7 | 用户 2026-11-13 明确外部生活服务暂不做 |
| 虚拟卡/Stripe Link | 官方商务 | 无 | not_in_scope | P7 | 用户 2026-11-13 明确外部支付类暂不做 |
| 官方市场内容 | Marketplace | 不做 | not_in_scope | — | 只做自建模板/Skill 注册表 |
| X Premium/SuperGrok/Cursor 订阅 | 官方 | 不做 | not_in_scope | — | 等价自托管 entitlement |
