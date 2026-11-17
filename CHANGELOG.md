# Changelog

All notable changes to superbot are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Community files: bilingual README, contributing guide, security policy, code of conduct, acknowledgments and issue/PR templates.
- Bilingual usage guide with prompt anatomy and copy-paste templates for research, coding, browser work, groups, long tasks, routines and review.
- Bilingual AI-assisted deployment guide: connection card, copy-paste prompts, an exact SRE runbook, acceptance checklist and security rules for handing a server to an AI agent.
- GitHub Actions improvements and Dependabot configuration.
- **Primary bot as approver**: another bot's risky tool call is decided by the account's primary bot (allow, deny or hand to the user) and recorded in the conversation; the primary bot can ask the user for a batch of standing allow rules (`request_permission_rules`), which only the user can approve.
- **Group memory** panel and `/api/teams/{id}/memories`; personal facts go to each person's own memory, also in group chats; superseded memories are archived with a link to what replaced them.
- **Memory recall evaluation** (`superbot memory-eval`, `internal/memory/testdata`), automatic background re-embedding and a circuit breaker for the embedding service.
- **Computers that run installed software**: on-screen text with click coordinates for text-only models (OCR), screenshots for vision models, right/double/middle click, drag, hover, click sequences, `desktop_launch`; `userapt` and `appimage-install` for installing without root; Node.js; background jobs (`shell` with `background`, `shell_jobs`); bots can switch themselves to the desktop profile.

### Changed

- Replaced example deployment addresses in source, tests and documentation with generic placeholders.
- `.env.example` now uses a relative data directory that works on any checkout.
- Only high-risk tools ask for approval by default (`local_exec`, vault/credentials, connector add/remove, deletes); shell, Python, file writes and browser/desktop work in the bot's own computer no longer do.
- A reply is shown before the user messages that reached the bot too late for it, live and after a reload, and tool cards stay above the reply they belong to; a reply resumed after an approval keeps what was said and done before the pause.
- In a group, bots call teammates with an @ in the reply instead of messaging them privately.
- E5 embeddings use `query:`/`passage:` prefixes with a per-query calibrated threshold; recall follows the bot's memory switch.
- A bot's home directory persists in its workspace; computer ports sit behind a per-bot secret gate; an optional host firewall (`deploy/sandbox-firewall.sh`) keeps computers off private addresses and the host.

### Fixed

- "Always allow" on an approval card saved a rule named `tool` instead of the tool's name, so it never took effect.
- Runs paused for approval stayed "running" forever; stale streams no longer linger as typing bubbles.
- A memory saved while the embedding service was down was reported as failed although it was stored.
- `deploy/sandbox-firewall.sh` with iptables-nft (rule checks print the matched rule).

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
