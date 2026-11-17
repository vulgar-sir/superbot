# Changelog

All notable changes to superbot are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Community files: bilingual README, contributing guide, security policy, code of conduct, acknowledgments and issue/PR templates.
- Bilingual usage guide with prompt anatomy and copy-paste templates for research, coding, browser work, groups, long tasks, routines and review.
- Bilingual AI-assisted deployment guide: connection card, copy-paste prompts, an exact SRE runbook, acceptance checklist and security rules for handing a server to an AI agent.
- GitHub Actions improvements and Dependabot configuration.

### Changed

- Replaced example deployment addresses in source, tests and documentation with generic placeholders.
- `.env.example` now uses a relative data directory that works on any checkout.

## [0.1.2] - 2026-11-17

The first public release of superbot. This entry summarizes the feature set that has been implemented and verified; see [`docs/functional-parity-matrix.md`](docs/functional-parity-matrix.md) for the authoritative status and evidence.

### Added

- **Bots** with persona, per-bot model, tool policy, private workspace, long-term memory, browser and XFCE cloud computer; account-level main bot.
- **Teams** with roundtable, pipeline, router–worker and Grok-style group orchestration.
- **Rooms** for humans and bots with streaming, @mentions, stop, visible cost, tool-call timelines and folded bot-to-bot private messages.
- **Activity feed**, unread indicators, global search and `/activity`.
- **Routines** with cron, `@every`, one-shot schedules, pause, webhook and email triggers.
- **Library** for pages, documents, files and media.
- **Extensions**: built-in skills, user skills, bot-proposed skills, HTTP MCP, bot template import/export and memory import.
- **Computers**: persistent Chromium and XFCE sessions per bot, noVNC takeover, update and reset.
- **Local device agent** with per-command native approval and Ed25519-signed jobs/results.
- **Vault** with AES-256-GCM encryption, masked lists and explicit reveal.
- **Usage accounting** by bot, model and day, with RMB cost estimates; voice input/calls; dark and light themes.
- **Connectors**: GitHub device-code auth, Slack, Notion, 1Password, Email, CalDAV.
- **Multi-model support**: OpenAI-compatible providers (DeepSeek, Qwen, Moonshot, GLM, Volcengine Ark, Ollama, …) and the Anthropic Messages API, with retries, `Retry-After` handling and streaming idle timeouts.
- **Long-task reliability**: checkpoints, chat-history search, budget-aware tool-loop compression, worker restart resume and non-retryable error handling.
- **Interjections and `/side`** so users can steer or ask a side question without interrupting a task.
- **Collaboration loop**: `send_to_agent` replies and subagent results wake the initiator, with relay limits; approvals resume the remaining pipeline/roundtable/group stages.
- **Group goals** with acceptance criteria, phase planning, assignment and evidence-based verification.
- **Layered memory** with relevance thresholds, write deduplication, secret rejection, idle review, recoverable consolidation, pinning and archiving.
- **Self-hosted embedding, STT and TTS** services in the Compose stack.
- **Security boundaries**: `os.Root` file confinement, SSRF protection, non-privileged sandboxes, a Docker-socket-holding `docker-proxy` with an allowlist, uid-1000 API/worker, and audit logs for extension changes.
- **Electron desktop client** with frameless window, native menu/tray/notifications, device agent, capture overlay and NSIS auto-update plumbing.
- **Bilingual UI** (Chinese and English), light and dark themes.

### Changed

- Standardized the project's public deployment instructions on Docker Compose and a single Go binary.
- Documented the difference between implemented, verified and configuration-required capabilities.

### Security

- Secrets are encrypted at rest and never returned in plaintext by the API.
- Browser containers publish no host ports in production and join a dedicated Docker network.
- The worker never mounts the Docker socket.

### Known limitations

- Single-node deployment; no Kubernetes or distributed workers.
- No native mobile app.
- Some third-party OAuth applications are intentionally deferred.
- Desktop installers are unsigned by default.
- Some capabilities require a domain with HTTPS, outbound network access or paid third-party accounts. See [`docs/configuration-required.md`](docs/configuration-required.md).

[Unreleased]: https://github.com/vulgar-sir/superbot/compare/v0.1.2...HEAD
[0.1.2]: https://github.com/vulgar-sir/superbot/releases/tag/v0.1.2
