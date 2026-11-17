# Acknowledgments / 致谢

**English** · [中文](#中文)

superbot would not exist without the open-source community. This page thanks the projects, tools and people that made it possible. If you contributed and are not listed here, please open a pull request — we want to fix that.

## Open-source foundations

| Project | How superbot uses it |
|---|---|
| [Go](https://go.dev/) | The API, worker, scheduler and sandbox orchestration. |
| [Next.js](https://nextjs.org/) and [React](https://react.dev/) | The web application. |
| [Tailwind CSS](https://tailwindcss.com/) | The UI styling system. |
| [PostgreSQL](https://www.postgresql.org/) and [pgvector](https://github.com/pgvector/pgvector) | Business data, the `SKIP LOCKED` job queue, the event bus and vector memory. |
| [Caddy](https://caddyserver.com/) | The reverse proxy and static entry point. |
| [Docker](https://www.docker.com/) | Sandbox and computer containers. |
| [Chromium](https://www.chromium.org/) | The per-bot browser. |
| [Xvfb](https://www.x.org/), [x11vnc](https://github.com/LibVNC/x11vnc) and [noVNC](https://github.com/novnc/noVNC) | The virtual display, VNC server and browser-based live takeover. |
| [faster-whisper](https://github.com/SYSTRAN/faster-whisper) and [fedirz/faster-whisper-server](https://github.com/fedirz/faster-whisper-server) | Self-hosted speech-to-text. |
| [Piper](https://github.com/rhasspy/piper) | Self-hosted text-to-speech. |
| [Dex](https://github.com/dexidp/dex) | Self-hosted OIDC/SSO in the Compose stack. |
| [Yjs](https://yjs.dev/) | Real-time collaborative documents. |
| [react-markdown](https://github.com/remarkjs/react-markdown), [remark-gfm](https://github.com/remarkjs/remark-gfm) and [rehype-sanitize](https://github.com/rehypejs/rehype-sanitize) | Safe Markdown rendering. |
| [Lucide](https://lucide.dev/) | Icons. |
| [Playwright](https://playwright.dev/) | End-to-end and UI verification. |
| [sqlc](https://sqlc.dev/) and [goose](https://github.com/pressly/goose) | Type-safe SQL and database migrations. |
| [Electron](https://www.electronjs.org/), [electron-vite](https://electron-vite.org/) and [electron-builder](https://www.electron.build/) | The desktop client and installers. |
| [TypeScript](https://www.typescriptlang.org/), [Node.js](https://nodejs.org/) and [Python](https://www.python.org/) | Build tooling and the self-hosted AI services. |
| [fastembed](https://github.com/qdrant/fastembed) and [multilingual-e5-large](https://huggingface.co/intfloat/multilingual-e5-large) | Self-hosted multilingual embeddings. |

## Verification and interoperability

We are also grateful for the projects used to test real-world behavior, including [Keycloak](https://www.keycloak.org/), [Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/), [Mozilla autopush](https://github.com/mozilla-services/autopush), [Caddy](https://caddyserver.com/), [Docker Compose](https://docs.docker.com/compose/) and the various LLM providers whose OpenAI-compatible and Anthropic APIs superbot speaks.

## People

- **qihai (vulgar-sir)** — creator and maintainer.
- Everyone who reported bugs, tested edge cases, reviewed pull requests, translated the interface, or simply ran superbot on their own server and told us what broke.
- The maintainers of every dependency above, who did the hard work that let us build on top.

Thank you. / 谢谢你们。

## Inspiration and trademarks

superbot's room layout, sidebar and composer were informed by modern chat and agent products, including **Grok** by xAI. All code in this repository is an independent implementation. superbot is **not affiliated with, sponsored by, or endorsed by xAI** or any other product mentioned in the documentation.

Product names, logos and trademarks — including Grok, xAI, GitHub, Slack, Notion, 1Password, DeepSeek, Qwen, Moonshot, GLM, Volcengine Ark, Ollama, Anthropic, OpenAI, Docker, PostgreSQL and Chromium — belong to their respective owners. They are used only to describe compatibility or inspiration.

## AI-assisted development

Parts of this codebase were reviewed, tested and hardened with the help of AI coding and review tools. Every change remains the responsibility of its human author and is verified against tests and real deployments.

---

## 中文

superbot 离不开开源社区。这一页感谢让本项目成为可能的项目、工具和人。如果你贡献过但不在名单里，欢迎提 PR，我们很乐意补上。

### 开源基石

| 项目 | superbot 用它做什么 |
|---|---|
| [Go](https://go.dev/) | API、Worker、调度器与沙箱编排。 |
| [Next.js](https://nextjs.org/) 与 [React](https://react.dev/) | Web 应用。 |
| [Tailwind CSS](https://tailwindcss.com/) | UI 样式系统。 |
| [PostgreSQL](https://www.postgresql.org/) 与 [pgvector](https://github.com/pgvector/pgvector) | 业务数据、`SKIP LOCKED` 任务队列、事件总线与向量记忆。 |
| [Caddy](https://caddyserver.com/) | 反向代理与静态入口。 |
| [Docker](https://www.docker.com/) | 沙箱与电脑容器。 |
| [Chromium](https://www.chromium.org/) | 每个 Bot 的浏览器。 |
| [Xvfb](https://www.x.org/)、[x11vnc](https://github.com/LibVNC/x11vnc) 与 [noVNC](https://github.com/novnc/noVNC) | 虚拟显示、VNC 服务与浏览器实时接管。 |
| [faster-whisper](https://github.com/SYSTRAN/faster-whisper) 与 [fedirz/faster-whisper-server](https://github.com/fedirz/faster-whisper-server) | 自托管语音识别。 |
| [Piper](https://github.com/rhasspy/piper) | 自托管语音合成。 |
| [Dex](https://github.com/dexidp/dex) | Compose 栈中的自托管 OIDC/SSO。 |
| [Yjs](https://yjs.dev/) | 实时协作文档。 |
| [react-markdown](https://github.com/remarkjs/react-markdown)、[remark-gfm](https://github.com/remarkjs/remark-gfm) 与 [rehype-sanitize](https://github.com/rehypejs/rehype-sanitize) | 安全的 Markdown 渲染。 |
| [Lucide](https://lucide.dev/) | 图标。 |
| [Playwright](https://playwright.dev/) | 端到端与 UI 验收。 |
| [sqlc](https://sqlc.dev/) 与 [goose](https://github.com/pressly/goose) | 类型安全 SQL 与数据库迁移。 |
| [Electron](https://www.electronjs.org/)、[electron-vite](https://electron-vite.org/) 与 [electron-builder](https://www.electron.build/) | 桌面客户端与安装包。 |
| [TypeScript](https://www.typescriptlang.org/)、[Node.js](https://nodejs.org/) 与 [Python](https://www.python.org/) | 构建工具与自托管 AI 服务。 |
| [fastembed](https://github.com/qdrant/fastembed) 与 [multilingual-e5-large](https://huggingface.co/intfloat/multilingual-e5-large) | 自托管多语言 Embedding。 |

### 验收与互操作

也感谢用于真实验收的工具，包括 [Keycloak](https://www.keycloak.org/)、[Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/)、[Mozilla autopush](https://github.com/mozilla-services/autopush)、[Caddy](https://caddyserver.com/)、[Docker Compose](https://docs.docker.com/compose/)，以及所有提供 OpenAI 兼容或 Anthropic API 的模型服务。

### 人

- **qihai (vulgar-sir)** — 作者与维护者。
- 所有报告 Bug、测试边界、审查 PR、翻译界面，或者只是在自己的服务器上跑了一遍并告诉我们哪里坏了的朋友。
- 上面每一个依赖的维护者——是你们完成了困难的部分，让我们可以站在上面继续搭。

谢谢你们。

### 灵感与商标

superbot 的房间布局、侧栏与输入框参考了包括 **Grok**（xAI）在内的现代聊天与 Agent 产品。本仓库所有代码均为独立实现。superbot 与 xAI 或文档中提到的其他产品**没有隶属、赞助或背书关系**。

Grok、xAI、GitHub、Slack、Notion、1Password、DeepSeek、Qwen、Moonshot、GLM、火山方舟、Ollama、Anthropic、OpenAI、Docker、PostgreSQL、Chromium 等名称、Logo 与商标归各自所有者所有，仅用于说明兼容性或灵感来源。

### AI 辅助开发

本项目的部分代码在 AI 编程与审查工具的帮助下完成评审、测试和加固。每一处改动仍由其人类作者负责，并通过测试和真实部署验证。
