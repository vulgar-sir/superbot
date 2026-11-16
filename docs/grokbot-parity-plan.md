# superbot × Grok Bot 1:1 复刻计划

> **2026-11-12 事实校正**：代码盘点发现本文 P0–P6 的部分“完成”项实际只有 UI/接口/空表。功能完成度以 [`functional-parity-matrix.md`](functional-parity-matrix.md) 为唯一事实来源；本文件保留为目标/架构参考，不再作为验收依据。

> 调研对象：本机安装的 Grok Bot `0.79.2`（Electron 42 / Chromium 148），2026-11-11 实测。
> 目标：功能与交互 1:1 对齐，代码、品牌素材、官方市场内容不复制；继续使用 superbot 自有实现与自托管模型。

## UI 1:1 复刻（2026-11-12）

以本机 Grok Bot 0.79.2 的实测计算样式为基准，superbot Web 端已按 1:1 视觉语言重建，除品牌图标、字体与官方头像素材外对齐：

| 项目 | 对齐结果 |
|---|---|
| 全局主题 | 默认浅色 `#fcfcfc` / 侧栏 `#f5f5f5` / 卡片 `#ffffff`；深色 `#141414` / `#181818`；支持系统主题与 5 种强调色 |
| 侧栏 | 240px；搜索 + 新建圆形按钮；主 Bot 大卡（56px 吉祥物 + 琥珀主 Bot 徽标）；分组折叠；54px Bot 行（36px 头像、14/20 标题、12/16 摘要）；未读蓝点与“更多未读消息”胶囊；底部账户菜单与“连接应用” |
| 主区 | 61px 顶栏 + 居中身份胶囊 + 分享；16px 内容边距；日期分隔；18px 圆角气泡、15/22 正文、7×12 内边距、连续消息圆角分组；用户右侧绿色气泡；滚动到底按钮居中浮动 |
| 输入框 | 44px 胶囊、0.8px 边框与柔和阴影；+ / 麦克风 / 语音 / 发送；Enter 发送、Shift+Enter 换行；群聊 `@` 成员自动补全（↑↓/Enter/Tab/Esc） |
| 右侧详情 | 320px 可拖拽调宽（280–480，记忆宽度）；64px 头像；动态 / 资料库标签；即将进行（例程开关）、建议、电脑、语音（语音/速度/语言）；资料库全部/页面/媒体分组与详情预览 |
| 弹窗 | 统一 16px 圆角、`scale(0.965)→1` 入场、半透明遮罩、Esc 关闭；新建 Bot（吉祥物预览 + 形状/颜色）、新建群聊、搜索、设置、例程、Bot 资料、连接器、电脑、语音、Bot 私信全部统一 |
| 设置 | 从侧栏账户菜单以应用内弹窗打开（1120px 宽、88vh），左侧导航 + 右侧卡片；导航按滚动高亮并同步内容标题 |
| 交互 | 右键菜单（Bot/群聊：置顶/移至子菜单/标为未读/重命名/复制对话 ID/隐藏/删除；分组：重命名/上移/下移/删除；消息：表情回应/回复/复制/复制请求 ID；资料/动态同规则）；侧栏 Bot 拖拽排序与跨分组；群聊拖拽进分组/置顶；分组拖拽排序；侧栏/详情面板拖拽调宽；统一应用内确认弹窗；乐观发送；Toast；`⌘/Ctrl+K` 搜索、`⌘/Ctrl+,` 设置、`⌘/Ctrl+\` 详情、`⌘/Ctrl+B` 侧栏、`/` 聚焦输入 |
| 响应式 | ≤1180px 详情为覆盖层，≤860px 侧栏为抽屉并默认收起，≤500px 详情全宽 |
| 持久化交互 | 表情回应、标为未读、隐藏/显示、结构化回复、附件上传/展示、Bot 模板分享链接、权限规则均落库（迁移 0017–0021） |
| 工具审批 | 权限页规则驱动 Worker：匹配工具创建待审批记录并暂停，聊天内「允许一次/始终允许/拒绝」后继续或拒绝 |
| 通用连接器 | 目录内全部 Provider 可存凭据；通用 OAuth 授权码一键连接已实现并端到端实测；Bot 通过 `connector_request` 调官方 API，Postgres 通过 `postgres_query` 只读查询 |
| Bot 连接器管理 | Bot 具备 `connector_list` / `connector_add` / `connector_test` / `connector_remove`，创建/删除走工具审批，敏感参数脱敏不回显 |
| 视觉附件 | 图片作为 `llm.PartImage` 传给支持视觉的模型；已实测 Bot 描述上传截图 |
| 登录页 | 按实测 Cursor/Grok 授权页的 500px 居中布局、账号卡、安全提示卡与双按钮重做 |
| 专项面板 | 全屏黑色电脑 VNC、顶部悬浮语音胶囊、900px 连接应用弹窗、设置六分区（通用/电脑/权限/保险库/用量/更新） |

账户菜单已对齐 Grok 的 258px 宽度/32px 行高/14px 字号与六项结构（用量、安装 Web 应用、帮助与支持 ›、设置、切换账户、退出登录）；Bot/群聊菜单 200px、分组菜单 180px、消息菜单 200px、分享菜单 227px、输入框 + 菜单 200px，均按实测复刻。逐项对照见 [`ui-parity-checklist.md`](ui-parity-checklist.md)。

客户端 UI 表面已按实测尺寸与截图全部对齐并完成生产验收；品牌素材按用户要求使用 superbot 自有 Logo、字体与吉祥物，不复制 Grok 官方素材。生产验收截图见 `tmp/sb-*.png` 与 `tmp/grok-*.png`。

## 当前进度（2026-11-12）

核心 1:1 功能面已落地并在生产验收：

| 模块 | 状态 |
|---|---|
| 修改密码 / 会话吊销 | 完成 |
| Grok 外壳、侧栏分区、置顶、搜索 | 完成 |
| 轻量 Bot、Bot 资料、主 Bot | 完成 |
| 群聊 @ 驱动、持久房间、主 Bot fallback | 完成 |
| 跨 Bot 私信、折叠卡片、动态/未读/搜索 | 完成 |
| Bot 级例程（cron/@every/once/暂停） | 完成 |
| Library（页面/文档/文件/媒体） | 完成 |
| 电脑更新/重置、noVNC | 完成 |
| Vault（登录/支付/联系信息，加密 + reveal） | 完成 |
| 本地设备配对、shell 执行、截图、Bot `local_exec`/`local_screenshot` | 完成 |
| 内置技能、Bot 模板导出/导入、记忆导入 | 完成 |
| 连接器：GitHub 设备码授权 + Slack/Notion/1Password/Email Token + 连接测试 | 完成；具体 Provider 需用户自己的 Client ID/Token |
| 用量（按 Bot/模型/日期）、语音输入、语音通话、浅色主题 | 完成 |
| 邮件 SMTP、日程 ICS、会议/购物（浏览器） | 完成 |
| 多账号/角色隔离 | 完成 |
| 硬件安全密钥（WebAuthn/Passkey） | 完成；生产 HTTP IP 不满足 WebAuthn，需 HTTPS 域名（支持 `SUPERBOT_WEBAUTHN_RPID/ORIGINS`） |
| 向量 embedding 自动召回 | 完成（本地 1024 维哈希向量 + pgvector） |
| UI 1:1 视觉与交互 | 完成（2026-11-12，浅/深色、侧栏/详情/输入框/弹窗/右键/拖拽/@ 补全） |


## 1. 调研结论

### 1.1 产品对象模型

Grok Bot 不是「一个聊天框 + 多 Agent 编排器」，而是「一支 AI 同事团队」：

- **Bot**：持久员工。只有名称/头像/描述/标题等轻量资料，创建时只要求名称和头像；人设、技能、记忆、例程都在后续对话和配置中逐步形成。
- **主 Bot（Primary Bot）**：账号级唯一的主助手。侧栏标「主 Bot」；新手引导先创建它；可「设为主 Bot / 更换主 Bot」；Team Bot 不能当主 Bot；更换后旧主 Bot 创建的 Bot 移到 Agents/未分组，不删除；主 Bot 是默认对用户说话的 Bot，并拥有创建 Bot/群聊/例程的账号级能力。
- **Sidebar Section**：账号级分组，可折叠、排序，包含 Bot 和群聊。没有「团队配置」这个概念。
- **DM**：每个 Bot 一个长期私聊；Bot 可以主动发消息、发动态、定时工作。
- **Group**：成员集合（最多 6 人，含总主管/项目主管），用户 + 多个 Bot 同屏；没有中心协调者，靠 `@` 提及和群聊轮次协议决定谁发言。
- **Activity（动态）**：Bot 的主动更新流。侧栏每行显示最近一条动态摘要和未读点，搜索可跨 Bot 检索动态。
- **Routine**：Bot 级定时任务，支持 cron、`@every`、一次性、时区、flexible ±N 分钟、暂停开关、预检无变化则不打扰。
- **Computer**：每个 Bot 一台持久云电脑；Shell / Screenshot / Computer / Read 工具，支持人工 VNC 接管、更新/重置。
- **Library**：Bot 做出的页面、文件、媒体，按「全部 / 页面 / 媒体」归档。
- **Vault**：登录信息、支付卡、联系信息；Bot 在授权下自动填表/登录。
- **Connector / Plugin / Skill**：应用连接器（GitHub、Slack、Notion、Google、1Password 等）、插件市场、内置技能与用户技能。
- **Device**：用户自己的电脑注册为设备，Bot 可按设备授权执行命令、截图、读写本地应用；可经本机转发流量。
- **Usage**：按来源/按 Bot 的用量图、套餐额度、按需上限。

### 1.2 关键机制

1. **群聊是广播 + 自选择发言**：每条消息所有 Bot 都能看到；被 `@` 的 Bot 必答，未被点名的 Bot 只在有新增结论时说话。官方提示明确说「群聊更费额度，因为每个 Bot 都要读每条消息」。没有圆桌协调者 JSON 决策。
2. **Bot 之间可私信**：主聊天里把跨 Bot 往返折叠成「与 N 个 Bot 的 M 条消息」，可点开单独交流；Bot 可以主动给队友发消息。
3. **主动工作**：Bot 不只在用户发消息时工作；例程、队友消息、事件触发都会唤醒它，并把结果写进动态/聊天。
4. **群聊防刷屏**：没被点名且没有新内容就不回复；不发「收到」「ok」；说完自己的结论就停。
5. **上下文管理**：工作笔记、子任务隔离、长输出只传路径、按需检索、稳定前缀、定时任务预检，这些是内置技能/团队规范的一部分。
6. **审批与权限**：自动审核 + 「始终询问」/「无需询问直接允许」规则；本机执行按设备单独授权；危险操作弹审批。
7. **电脑是持久工作区**：文件、安装的软件、浏览器登录态跨轮次保留；电脑休眠后仍保留；更新/重置是两种不同恢复动作。
8. **技能/模板**：Bot 模板包含 profile、memory、skills、routines、plugins、gettingStarted，可导出/导入；技能可由 Bot 自己创建/更新/提议。

### 1.3 工具面（实测枚举，按能力归类）

- 对话/消息：`SendMessage`、`SendToAgent`、`SendToUser`、`SendPoll`、`ReactToMessage`、`SetProactiveMessaging`、`ReadAgentActivity`、`WakeParent`。
- 子代理：`CheckSubagent`、`MessageSubagent`、`StopSubagent`、`CloudAgent`。
- 记忆：`RecallMemory`、`UpdateMemory`、`SortMemories`。
- 技能/例程：`Skill`、`UpdateSkill`、`ProposeSkills`、`UpdateRoutine`。
- 电脑：`Computer`、`Shell`、`Screenshot`、`Read`、`request_box_help`、`BrowserSecureFill`。
- 文件/资料库：`SaveToLibrary`、`ListBotResources`、`ShowBotResources`、`ListDocuments`、`ReadDocument`、`WriteDocument`、`EditCanvas`、`WriteCanvasAsset`。
- 安全：`Vault`、`ListCredentials`、`FillCredential`、`ListAvailableSecureValues`、`RequestOnePasswordConnect`。
- 连接器/渠道：Slack / Microsoft Teams / Email / iMessage 的收发、编辑、表情、状态；`CreateChannel`、`UpdateChannel`。
- 会议/日历：`JoinMeeting`、`LeaveMeeting`、`ListMeetings`、`GetMeetingSummary`、`ReadMeetingTranscript`、`RespondInMeeting`、`DraftCalendarInvite`。
- 任务/项目：`CreateTask`、`ReadTask`、`UpdateTask`、`JoinProject`、`ShareProject`、`ConvertToTeam`。
- 设备：`ListMachines`、`DeviceGetLocation`、Mac 邮件/笔记/Obsidian 读写、iOS 日历/提醒/健康/家庭。
- 生活服务：航班、餐厅、购物、虚拟卡、电话等（依赖官方连接器，后续可选）。

### 1.4 Settings 实测

- 通用：账户（多账号/退出）、外观（主题/强调色/语言/拼写）、系统（麦克风/硬件加速）、Bot（时区）、导入（从其他 AI 导入记忆）、安全密钥（WebAuthn）、团队 Bot（Microsoft Teams 关联）、数据共享。
- 电脑：本机执行权限、经本机转发流量、管理设备、更新/重置 Bot 的电脑。
- 权限：自动审核开关、始终询问规则、无需询问直接允许规则。
- 保险库：项目 / 登录信息 / 支付 / 联系信息。
- 用量与账单：套餐、账单、用量限额、按来源/按 Bot 图表、按需每月限额。
- 更新：自动更新、版本、更新通道。

## 2. 复刻边界

**对齐**：对象模型、页面结构、交互流程、群聊/私信/动态/例程/电脑/资料库/权限/设置的心智模型。

**不照搬**：

- 不复制 Grok Bot 的代码、UI 素材、图标、品牌名、官方技能/站点剧本内容。
- 不实现官方订阅计费；superbot 用用户自己的 LLM Provider，按实际 token 成本记账。
- 不接入官方闭源连接器/生活服务；用可插拔 Connector/MCP/Skill 框架替代，逐步补 GitHub、Slack、Notion、Google、1Password 等常用连接器。
- 不做 iOS/Android 原生端；先做响应式 Web/PWA 和桌面浏览器。
- 群聊人数、工具集等参数可按自托管资源调整，但默认对齐 6 人。

## 3. 现状差距

| 能力 | Grok Bot | superbot 现状 | 优先级 |
|---|---|---|---|
| 侧栏分组 / 置顶 / 未读 | 有 | 无，只有扁平会话列表 | P1 |
| Bot 创建 | 名称 + 头像即可 | 必须填模型/Provider/人设 | P1 |
| Bot 资料页 | 动态 / 资料库 / 例程 / 电脑 / 语音 | 无 | P1 |
| 群聊 | 持久群、@ 轮次、最多 6 人 | Team + 每任务新会话；圆桌/流水线/Router | P1/P2 |
| Bot 私信 | 每个 Bot 一个长期 DM | 可建多个 direct 会话 | P1 |
| 跨 Bot 私信 | 有，主聊天折叠展示 | 无 | P2 |
| 动态 / 未读 | 有 | 无 | P2 |
| Bot 级例程 | cron/flexible/一次性/暂停/预检 | Team 级 cron，到点新建会话 | P3 |
| 资料库 | 页面/媒体/文件 | 工具 artifact，无 UI | P3 |
| 电脑 | 每 Bot 持久 box，Shell/Screenshot/Computer | 每 Bot Docker 沙箱 + 浏览器/桌面 | P4 |
| 本地设备 | 注册设备、按设备授权、本机执行 | 无 | P4 |
| Vault | 登录/支付/联系信息 | 仅 Provider/MCP 密钥加密 | P4 |
| 技能 | 内置 + 用户 + Bot 自建 + 模板 | SKILL.md + 提议/批准 | P5 |
| 连接器/市场 | 大量 OAuth 应用 + 插件 | HTTP MCP | P5 |
| 记忆 | Bot/用户/团队 + 导入 + 语义召回 | Bot/Team 关键词检索 | P5 |
| 用量 | 套餐/按 Bot/按来源/按需 | Provider/模型成本记录 | P6 |
| 语音/会议/邮件 | 有 | 无 | P6 |

## 4. 目标架构

### 4.1 对象模型

```
User
 ├─ primary_bot_id → Bot（账号级唯一主 Bot）
 ├─ SidebarSection (name, position, collapsed)
 │   ├─ Bot (section_id, position, pinned, title, avatar, description, proactivity, kind)
 │   └─ Group (team.mode=group, section_id, position, pinned)
 ├─ DM: conversation(kind=direct, bot_id)  ← 每个 Bot 唯一
 ├─ Group: team(mode=group) + team_bots(≤6) + room_conversation_id
 ├─ AgentThread: bot ↔ bot 的私信会话（独立于用户 DM）
 ├─ Activity: bot 主动更新（摘要、未读、来源消息/例程）
 ├─ Routine: bot_id + schedule + timezone + timing + paused + precheck
 ├─ LibraryItem: bot_id + kind(page/media/file) + path/preview
 ├─ Device: 本机 daemon + per-device permission
 ├─ VaultItem: login/payment/contact + encrypted payload
 ├─ Connector: provider + OAuth/credential + tool namespace
 └─ Skill: builtin/user/bot + per-bot binding + version
```

### 4.2 Agent Runtime

- **Direct**：保持不变（单 Bot 工具循环）。
- **Group**：用户消息广播到群；提取 `@BotName`：
  - 有提及：按顺序运行被提及 Bot；每个 Bot 完整读取群消息。
  - 无提及：运行群主（`teams.config.primary_bot_id`，否则第一位成员）。
  - 某个 Bot 的最终文本出现 `@队友`：把该队友加入下一轮队列，最多 N 轮/预算上限。
  - 不引入中心协调者；用系统提示 + 确定性 @ 规则防止刷屏。
  - 所有 Bot 的 run 仍写 events/runs/usage，UI 按 Bot 展示。
- **Cross-bot**：新增 `send_to_agent` 工具，写入 AgentThread 并唤醒目标 Bot；目标 Bot 的回复写回 AgentThread，并向发起者/用户动态发摘要。
- **Proactivity**：例程/队友消息/事件创建 Bot 的 queued run；结果写 Activity 和 DM/群消息。
- **Subagent**：后续增加 `spawn_subagent`/`check_subagent`，在隔离上下文执行长任务，只回传摘要。

### 4.3 UI Shell

- 左侧单栏（替代当前 66px Rail + 会话列表）：
  - 账户头像/名称（菜单：设置、切换账户、退出）
  - 搜索、+ 新建
  - 分区列表：Bot/群聊，最近动态摘要、未读点、折叠
  - 连接应用
  - 底部账户菜单
- 主区：DM/群聊；顶部 Bot/群名、分享、详情开关；消息流；输入框。
- 右侧详情面板：动态（资料、例程、建议、电脑、语音）/ 资料库（全部/页面/媒体）。
- 设置：六分区（通用/电脑/权限/保险库/用量/更新），先做通用/权限/用量，其他随能力上线。
- 新建 Bot：名称 + 形状/颜色；模型自动选默认；创建后打开 DM 并让 Bot 自我介绍。

### 4.4 主 Bot 设计

#### 数据模型

- `users.primary_bot_id uuid REFERENCES bots(id) ON DELETE SET NULL`
- `bots.kind text NOT NULL DEFAULT 'personal' CHECK (kind IN ('personal','team'))`
- `bots.created_by_bot_id uuid REFERENCES bots(id) ON DELETE SET NULL`（记录由哪个 Bot 创建，用于更换主 Bot 时迁移子 Bot）
- `bots.section_id/position` 已有；旧主 Bot 的子 Bot 在更换时迁到「Agents / 未分组」，不删除。

#### 规则

1. 每个用户最多一个主 Bot；数据库用 `users.primary_bot_id` 保证唯一。
2. 用户还没有主 Bot 时，创建的第一个 Bot 自动成为主 Bot；如果没有 Bot，引导页显示「Create primary Bot」。
3. 侧栏、Bot 资料页、聊天页对主 Bot 显示「主 Bot」标签。
4. 非主 Bot 的资料页提供「设为主 Bot」；主 Bot 的资料页提供「更换主 Bot」。
5. `bots.kind='team'` 的 Team Bot 不能设为主 Bot，接口与 UI 都要拒绝并提示「Team Bots can't be a primary Bot」。
6. 更换主 Bot：
   - 新主 Bot 必须是当前用户的 personal Bot；
   - 旧主 Bot 保留为普通 Bot，不删除；
   - 旧主 Bot 创建的 Bot（`created_by_bot_id=旧主 Bot`）迁到未分组/Agents 分区；
   - 写入 `audit_logs`，记录旧/新主 Bot。
7. 主 Bot 拥有账号级工具：`CreateAgent/UpdateAgent`（创建/配置其他 Bot）、`CreateGroup`、`CreateRoutine`、`SetPrimaryBot`（仅主 Bot 或用户可调用）；普通 Bot 调用这些工具返回权限错误。
8. 群聊默认把主 Bot 作为成员（最多 6 人时优先保留主 Bot）；群聊无人 @ 时的 fallback 顺序：群 `primary_bot_id` → 主 Bot（若在群里）→ 第一位成员。
9. 首次打开应用、没有指定会话时，默认打开主 Bot 的 DM；主 Bot 的欢迎消息走「Introducing your primary Bot」流程。
10. 主 Bot 可以被删除，但删除前必须先指定新的主 Bot；否则返回冲突错误。

#### API

- `GET /api/auth/me` 返回 `user.primary_bot_id`
- `POST /api/account/primary-bot` `{bot_id}`：设置/更换主 Bot
- `POST /api/bots`：若用户没有主 Bot，创建后自动设为主 Bot
- `DELETE /api/bots/{id}`：若目标是主 Bot 且没有替代，返回 409 `primary_bot_required`
- `GET /api/sidebar`：Bot 项包含 `is_primary`

#### UI

- 侧栏主 Bot 行显示「主 Bot」徽标，默认置顶到账号区。
- Bot 资料页：主 Bot 显示「主 Bot」状态与「更换主 Bot」；普通 Bot 显示「设为主 Bot」。
- 更换主 Bot 弹窗：列出 personal Bot，Team Bot 置灰并提示不能选。
- 设置/通用增加「主 Bot」行，与资料页入口一致。

#### 验收

- 空账号创建第一个 Bot 后自动成为主 Bot，侧栏有「主 Bot」。
- 创建第二个 Bot，点「设为主 Bot」后标签切换；`/api/auth/me` 的 `primary_bot_id` 更新。
- Team Bot 设置主 Bot 返回明确错误。
- 更换主 Bot 后旧主 Bot 保留，旧主 Bot 创建的子 Bot 出现在未分组。
- 删除主 Bot 且无替代返回 409；指定新主 Bot 后可删除。
- 主 Bot 能创建群聊/其他 Bot；普通 Bot 调用账号级工具被拒绝。

## 5. 里程碑

### P0 已完成（2026-11-11）
- 修改密码接口 + 设置页账户安全卡片；改密后吊销其他会话；生产验证通过。

### P1 已完成（2026-11-11）
- DB：`sidebar_sections`；`bots.section_id/position/title/avatar_shape/avatar_color/pinned/last_activity_at/kind/created_by_bot_id`；`users.primary_bot_id`；`teams.section_id/position/pinned`；`teams.mode` 增加 `group`；`teams.room_conversation_id`。
- API：`/api/sidebar`、`/api/sections` CRUD、Bot 轻量创建（模型可省略，自动选默认）、Bot 资料更新、DM 复用、`/api/account/primary-bot`。
- UI：Grok 风格左侧栏、新建 Bot 对话框、Bot 详情面板骨架（动态/资料库）、账户菜单、主 Bot 标签/设置/更换。
- 验收：新建 Bot 只需名称/头像；能分组/折叠/置顶；每个 Bot 只有一个 DM；刷新后侧栏顺序保留；首个 Bot 自动成为主 Bot，可切换主 Bot，Team Bot 不可当主 Bot。

### P2 群聊与跨 Bot 私信（已完成 2026-11-12）
- 群创建/持久 room、最多 6 人、@ 驱动、主 Bot fallback、链式 @；生产 PASS。
- AgentThread 跨 Bot 私信、`send_to_agent`、主聊天折叠卡片、Activity/未读/全局搜索、`/activity` 动态页；生产 PASS。

### P3 例程、主动工作与资料库（已完成 2026-11-12）
- Bot 级 routine（cron/@every/一次性/flexible/timezone/paused/precheck）；例程到点写入 DM 或群；完成 inbox。
- 主动消息偏好、动态生成、WakeParent。
- Library：页面/文件/媒体、保存/预览/删除。
- 验收：Bot 例程按 cron 自动在 DM 产出并生成动态；暂停生效；无变化不打扰；生成的页面出现在资料库。

### P4 电脑、本地设备与 Vault（已完成 2026-11-12）
- Box 生命周期：启动/休眠/更新/重置；状态卡；Shell/Screenshot/Computer/Read 工具；computerUse 子代理。
- 本地设备 daemon：配对、设备列表、按设备执行权限、本机 shell/截图、流量转发。
- Vault：登录/支付/联系信息加密；Bot 授权填表；1Password 连接器位。
- 验收：Bot 在 box 内执行命令/截图；用户在 VNC 接管；本机命令弹审批；Vault 凭据可被浏览器工具安全填写。

### P5 技能、模板、连接器与记忆（已完成 2026-11-12）
- 内置技能（群聊轮次、例程、技能编写、源码管理、电脑、连接器、Vault、用量复盘等）；Bot 自建/提议技能；Bot 模板导入导出。
- Connector 框架 + GitHub/Slack/Notion/Google/1Password 首批；OAuth/凭据加密；MCP 市场。
- 记忆：Bot/用户/团队三层、导入其他 AI 记忆、pgvector 语义召回。
- 验收：Bot 自己创建技能并生效；导入 Bot 模板；连接 GitHub 读仓库；跨会话记忆召回。

### P6 用量、语音与打磨（已完成 2026-11-12；硬件密钥需 HTTPS 域名）
- 按 Bot/来源用量图、预算/按需上限；语音输入/输出/通话；通知；WebAuthn；响应式/PWA。
- 验收：用量图表与账单一致；语音可对话；手机宽度可用。

## 6. 本次开始的实施顺序

1. 迁移 0004：侧栏分区、Bot 资料字段、Group 模式/room、置顶。
2. sqlc 查询 + `/api/sidebar`、`/api/sections`、Bot 轻量创建/资料更新。
3. 前端新侧栏 + 新建 Bot + Bot 详情面板。
4. 群聊 runtime 与 API（P2 前半），再做跨 Bot 私信/动态。

## 7. 风险

- **群聊成本**：每个 Bot 读每条消息，6 人群一轮可能 6 倍上下文；必须做轮次/预算/并发上限，并在 UI 明示。
- **上下文膨胀**：Bot 长期 DM + 动态 + 记忆会让上下文失控；沿用工作笔记/摘要/工具输出截断，并给群聊单独预算。
- **数据迁移**：现有 Team/Conversation 数据要兼容；group 模式是新增枚举，不改旧数据语义。
- **本地设备安全**：设备 daemon 必须有配对、签名、按设备权限、命令审计；默认拒绝。
- **连接器合规**：OAuth token 加密、最小权限、可撤销；不复制官方市场内容。
- **1:1 预期管理**：功能面对齐是渐进的；先保证「Bot 团队 + 群聊 + 动态 + 例程 + 电脑」核心闭环，再补连接器/生活服务。

## 8. 验收总原则

- 每个里程碑必须有可在生产 URL 上操作的端到端验收，不只单测。
- 所有新增写接口带鉴权、输入校验、审计；密钥只加密存储、不回显。
- 群聊/例程/设备执行必须有硬上限与停止按钮。
- 不以「看起来像」为完成标准，以「用户能完成同样任务」为完成标准。
