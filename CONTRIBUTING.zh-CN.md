# 参与 superbot 开发

感谢你愿意改进 superbot。欢迎各种规模的贡献：Bug 报告、文档、翻译、UI 打磨、测试和功能。

[English](CONTRIBUTING.md) · **简体中文**

参与本项目即表示你同意遵守 [行为准则](CODE_OF_CONDUCT.md)。安全问题必须按照 [SECURITY.md](SECURITY.md) 走私密渠道报告，不要开公开 issue。

## 可以怎么贡献

- **报告 Bug**：附上最小复现、日志和环境信息。
- **提出功能建议**：描述你遇到的真实问题和需要的工作流，而不只是实现方案。
- **改进文档**：尤其是中文深入文档的英文翻译。
- **补充测试**：给目前没有覆盖的区域写测试。
- **提交 PR**：修一个小问题或实现一个边界清晰的功能。

## 动手之前

1. 先搜索已有的 issue 和 PR，避免重复劳动。
2. 大改动（新子系统、数据库迁移、安全相关代码、新依赖、破坏性 API 变更）请先开 issue，确认范围和设计后再投入时间。
3. PR 尽量小且聚焦。一组可审查的提交比一次大重写更容易合并。
4. 如果使用 AI 辅助编程工具，你仍然需要理解、审查并测试自己提交的每一行代码。

## 开发环境

### 前置条件

- Go 1.27+
- Node.js 24+
- Docker Desktop（Linux 上可用 Docker Engine）
- [Task](https://taskfile.dev/) 可选，但很方便

### 后端与前端

```bash
cp .env.example .env
# 填入 SUPERBOT_SECRET_KEY；如需真实模型，再填 LLM API Key。

task dev-infra   # PostgreSQL，127.0.0.1:55432
task migrate     # 执行 goose 迁移

task api         # Go API，:8080
task worker      # Go Worker
task web         # Next.js，:3000
```

不使用 Task 时：

```bash
docker compose -f deploy/docker-compose.dev.yml up -d postgres
go run ./cmd/superbot migrate
go run ./cmd/superbot serve
go run ./cmd/superbot worker
cd web && npm run dev
```

### 桌面端

```bash
cd desktop
npm ci
npm run typecheck
SUPERBOT_WEB_URL=http://127.0.0.1:3000 npm run dev
```

### 测试

```bash
# 后端（单元测试不需要外部服务）
go test ./...
go vet ./...

# 集成测试：使用一次性 PostgreSQL 数据库
SUPERBOT_TEST_DATABASE_URL="postgres://superbot:superbot@127.0.0.1:55432/superbot_test?sslmode=disable" \
  go test -tags integration -run Integration ./internal/agent/...

# 前端
cd web
npm run typecheck
npm run build
npm run test:e2e        # 需要运行中的服务；设置 SUPERBOT_E2E_BASE_URL

# 桌面端
cd desktop
npm run typecheck
```

CI 会运行 gofmt、`go vet`、sqlc 生成检查、Go 测试、基于真实 Postgres 的集成测试，以及 Next.js 与 Electron 的 typecheck/build。

## 项目结构

| 路径 | 内容 |
|---|---|
| `cmd/superbot` | 入口（`serve`、`worker`、`migrate`、`device` 等） |
| `internal/agent` | Agent 循环、编排、上下文管理 |
| `internal/llm` | Provider 归一化（OpenAI 兼容 + Anthropic） |
| `internal/tools` | 文件、shell、Python、Web、记忆与技能工具 |
| `internal/sandbox` | Docker 沙箱与电脑生命周期 |
| `internal/httpapi` | REST、SSE、WebSocket 与 noVNC 代理 |
| `internal/memory` | 召回、去重、回顾与整理 |
| `internal/store` | sqlc 生成的数据库访问代码 |
| `migrations/` | goose 迁移 |
| `queries/` | sqlc 查询 |
| `web/` | Next.js 16 前端 |
| `desktop/` | Electron 客户端与本地设备代理 |
| `deploy/` | Compose 栈、Caddy、镜像与脚本 |
| `docs/` | 工程笔记与验收记录 |

## 代码规范

### Go

- 运行 `gofmt`，代码必须格式化。
- 运行 `go vet ./...`。
- 错误用 `%w` 包装并补充上下文，不要静默吞掉错误。
- 请求与工具链路要传递 `context.Context`。
- 不要记录密钥、Token、Cookie 或完整的 Provider 请求体。
- 文件工具必须留在自己的 `os.Root` 内，不要加绕过根目录的路径处理。
- MCP Server、Skill、模型输出和抓取到的网页内容一律按不可信输入处理。
- 数据库变更必须新建编号 goose 迁移。运行 `sqlc generate` 并提交重新生成的 `internal/store` 代码，不要手改生成文件。
- 测试行为而不是实现细节；优先使用表驱动测试。
- 没有充分理由不要引入新依赖；新依赖必须维护良好且许可证兼容。

### TypeScript / Next.js

- 保持 TypeScript strict；运行 `npm run typecheck` 和 `npm run build`。
- 不要硬编码用户可见文案：中英文词典要同时加 key。
- 保持键盘导航、焦点管理和可访问标签。
- UI 改动请在 PR 中附截图或简短录屏。
- 截图、fixture 和测试里不要出现真实凭据、服务器地址或个人数据。

### 桌面端

- 提交 PR 前运行 `npm run typecheck`。
- 设备代理的改动必须保留签名校验、防重放和原生审批弹窗。
- 自动更新改动要用未签名的开发构建验证；不要提交签名证书或凭据。

### 文档

- 只有在有真实验收证据时才更新 [`docs/functional-parity-matrix.md`](docs/functional-parity-matrix.md) 的状态。
- 修改配置时，同时更新 `.env.example` 和 `deploy/.env.production.example`。
- 鼓励双语文档。如果只能写一种语言，先用一种语言提 PR 并注明需要翻译。

## Pull Request

一个好的 PR：

- 说明**改了什么**以及**为什么**；
- 关联相关 issue；
- 为新行为补测试，并说明你如何验证；
- UI 改动附截图；
- 用户可见的改动更新 changelog；
- 不把无关重构混进同一个改动；
- 通过 CI。

请确认你有权提交这些代码，并且不包含复制的专有代码、凭据或个人信息。

## 提交信息

使用祈使句、简洁地描述行为变化：

```
Add per-bot memory import
Fix SSE reconnect after worker restart
Document the OIDC callback URL
```

`feat:`、`fix:`、`docs:` 等前缀可选，不强制。

## 许可证

superbot 以 [GNU Affero General Public License v3.0](LICENSE) 开源。提交贡献即表示你同意你的贡献以相同条款授权，并且你有权授予该许可。

如果引入第三方代码，必须与 AGPL 兼容，并保留其版权与许可证声明。请在 [`ACKNOWLEDGMENTS.md`](ACKNOWLEDGMENTS.md) 或相关源文件中补充署名。

## 有问题？

如果本指南有不清楚的地方，欢迎开 discussion 或带 `question` 标签的 issue。我们宁愿回答问题，也不想让你卡住。
