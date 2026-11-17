# Documentation / 文档

**English** · [中文](#中文)

The [project README](../README.md) is the best entry point. The documents below are deeper engineering notes, verification records and configuration guides.

## Guides

| Document | Language | Contents |
|---|---|---|
| [`deploy-with-ai.md`](deploy-with-ai.md) | English | A connection card, prompt and exact runbook for letting an AI agent deploy superbot on a server. |
| [`deploy-with-ai.zh-CN.md`](deploy-with-ai.zh-CN.md) | 中文 | 把项目链接和服务器连接方式交给 AI，让 AI 代为部署的完整提示词与执行步骤。 |
| [`usage.md`](usage.md) | English | First-run walkthrough, prompt anatomy, copy-paste templates for research, coding, browser work, groups, long tasks, routines and review. |
| [`usage.zh-CN.md`](usage.zh-CN.md) | 中文 | 第一次使用、提示词写法、可复制模板（研究 / 编码 / 浏览器 / 群聊 / 长任务 / 例程 / 审查）。 |
| [`functional-parity-matrix.md`](functional-parity-matrix.md) | 中文 | The single source of truth for feature completeness and verification evidence. |
| [`configuration-required.md`](configuration-required.md) | 中文 | Features that need external credentials, network access or platform support. |
| [`pending-verification.md`](pending-verification.md) | 中文 | Items still awaiting real-world verification. |
| [`ui-parity-checklist.md`](ui-parity-checklist.md) | 中文 | UI alignment checklist and measured geometry. |
| [`grokbot-parity-plan.md`](grokbot-parity-plan.md) | 中文 | Historical plan for the Grok-style bot experience. |
| [`../deploy/`](../deploy/) | English | Compose stack, Caddy, sandbox images, backups, Dex and soak tests. |
| [`../desktop/README.md`](../desktop/README.md) | English | Electron client, local device agent and auto-update feed. |

## A note on evidence

superbot's engineering notes distinguish between:

- **implemented** — the code exists;
- **verified** — tested against real infrastructure with evidence;
- **requires_config** — the code exists but needs external credentials or platform support;
- **platform_limited** — cannot work on the current platform;
- **not_in_scope** — intentionally out of scope.

We would rather show a `requires_config` badge than pretend a feature works. When contributing a status change, include the evidence.

## 中文

[项目 README](../README.zh-CN.md) 是最好的入口。下面这些文档是更深入的工程笔记、验收记录和配置指南。

### 指南

| 文档 | 语言 | 内容 |
|---|---|---|
| [`deploy-with-ai.zh-CN.md`](deploy-with-ai.zh-CN.md) | 中文 | 把项目链接和服务器连接方式交给 AI，让 AI 代为部署的完整提示词与执行步骤。 |
| [`deploy-with-ai.md`](deploy-with-ai.md) | English | Connection card, prompt and exact runbook for AI-assisted deployment. |
| [`usage.zh-CN.md`](usage.zh-CN.md) | 中文 | 第一次使用、提示词写法、可复制模板（研究 / 编码 / 浏览器 / 群聊 / 长任务 / 例程 / 审查）。 |
| [`usage.md`](usage.md) | English | First-run walkthrough, prompt anatomy and copy-paste templates. |
| [`functional-parity-matrix.md`](functional-parity-matrix.md) | 中文 | 功能完成度与验收证据的唯一事实来源。 |
| [`configuration-required.md`](configuration-required.md) | 中文 | 需要外部凭据、网络或平台支持的能力。 |
| [`pending-verification.md`](pending-verification.md) | 中文 | 仍待真实验收的条目。 |
| [`ui-parity-checklist.md`](ui-parity-checklist.md) | 中文 | UI 对齐清单与实测尺寸。 |
| [`grokbot-parity-plan.md`](grokbot-parity-plan.md) | 中文 | Grok 风格 Bot 体验的历史计划。 |
| [`../deploy/`](../deploy/) | English | Compose 栈、Caddy、沙箱镜像、备份、Dex 与 soak 测试。 |
| [`../desktop/README.md`](../desktop/README.md) | English | Electron 客户端、本地设备代理与自动更新 feed。 |

### 关于验收证据

superbot 的工程笔记严格区分：

- **已实现** — 代码存在；
- **已验证** — 在真实基础设施上测试过，并有证据；
- **requires_config** — 代码存在，但需要外部凭据或平台支持；
- **platform_limited** — 当前平台无法工作；
- **not_in_scope** — 主动不做。

我们宁愿显示 `requires_config`，也不愿假装功能可用。提交状态变更时，请附上证据。

## Translating these documents

Most deep-dive notes are currently written in Chinese. If you would like to translate one into English, open an issue or a pull request. Keep the original file available and add a language switcher at the top.

## 翻译这些文档

目前多数深入文档以中文撰写。如果你愿意把其中一篇翻译成英文，欢迎开 issue 或 PR。请保留原文件，并在顶部加语言切换链接。
