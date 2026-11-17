# Security Policy

superbot runs real commands, connects to real services and stores credentials. We take security reports seriously and appreciate responsible disclosure.

## Supported versions

| Version | Supported |
|---|---|
| `main` (development) | ✅ |
| `0.1.x` | ✅ |
| Older tags / forks | Best effort only |

Security fixes are applied to `main` first and backported to the latest release when practical.

## Reporting a vulnerability

**Please do not open a public issue, discussion or pull request for a security problem.**

Use GitHub's private vulnerability reporting:

1. Open https://github.com/vulgar-sir/superbot/security/advisories/new
2. Describe the affected version or commit, deployment mode, reproduction steps and impact.
3. If possible, include a minimal proof of concept and a suggested fix.

If private reporting is unavailable, contact the maintainer [@vulgar-sir](https://github.com/vulgar-sir) privately through GitHub rather than posting details publicly.

We aim to acknowledge a report within **72 hours** and to provide an initial assessment or remediation plan within **7 days**. Please give us a reasonable window to fix the issue before public disclosure. We are happy to credit reporters in the advisory unless you prefer to stay anonymous.

## Scope

In scope:

- Authentication and session handling, password reset, WebAuthn/Passkey, OIDC/SSO.
- Authorization and account/tenant isolation.
- Secret handling, encryption, logging and API responses that expose plaintext secrets.
- Path traversal, symlink escapes, sandbox escape and container breakout.
- Server-side request forgery, DNS rebinding and egress-control bypasses.
- `docker-proxy` allowlist bypasses or container-creation validation flaws.
- Command injection, unsafe deserialization, XSS in rendered Markdown/HTML, CSRF.
- Supply-chain issues in code or build/release workflows.
- Unsafe defaults that turn a documented feature into remote code execution.

Out of scope:

- Bots executing shell, Python or browser actions **after the user has approved or configured them** — this is the product's purpose.
- Denial of service that requires an authenticated administrator or physical access.
- Self-XSS, missing headers with no demonstrated impact, and reports from automated scanners without a concrete exploit.
- Vulnerabilities in third-party services, model providers, MCP servers or packages that should be reported upstream.
- Social engineering.

## Deployment hardening checklist

superbot is designed for a trusted operator and a small team. Treat every bot as an untrusted code-execution workload.

- [ ] Put the instance behind HTTPS and a trusted reverse proxy. Set `SUPERBOT_COOKIE_INSECURE=false` and `SUPERBOT_ALLOW_INSECURE_COOKIE=false`.
- [ ] Generate a strong, unique `SUPERBOT_SECRET_KEY` and store it outside the repository.
- [ ] Change the first-boot admin password immediately; use a password manager.
- [ ] Run `deploy/harden-data-permissions.sh` on the host before creating workspaces.
- [ ] Keep `.env` out of git, backups and screenshots. Never commit real API keys or server addresses.
- [ ] Do not publish the Docker socket or the `docker-proxy` port. Keep the worker on the dedicated Docker network.
- [ ] Keep browser/desktop containers on `superbot_sandbox`; do not add host port mappings.
- [ ] Review every MCP server and Skill before installing it. They run with the bot's tools and are treated as untrusted input.
- [ ] Use the local device agent only with explicit per-command approval. `SUPERBOT_DEVICE_AUTO_APPROVE` is for unpackaged development builds and must not be used in production.
- [ ] Restrict outbound network access where possible. Bots browse the web by design; egress policy is your responsibility.
- [ ] Back up PostgreSQL and `SUPERBOT_DATA_DIR` regularly, encrypt the backups, and test restores.
- [ ] Keep dependencies and base images updated; CI and Dependabot help, but you own your deployment.
- [ ] For hostile multi-tenant use, run separate instances. superbot is built for a trusted team, not for arbitrary public sign-ups.

## Known design boundaries

- Bots can execute shell/Python and control a browser inside a sandbox. Sandbox isolation is a security boundary; the agent's behavior is not.
- A user who can reveal a Vault entry can read its secret. Vault protects against database dumps and silent model access, not against a compromised account.
- Browser and desktop containers have network access. Do not place them on a network with sensitive internal services.
- The desktop app is unsigned by default. Verify the source of any installer you run.

## 中文摘要

- **支持版本：** `main` 与最新 `0.1.x` 发布。
- **报告方式：** 请使用 GitHub 私密漏洞报告（Security → Report a vulnerability），不要开公开 issue；也可以私信维护者 [@vulgar-sir](https://github.com/vulgar-sir)。
- **响应目标：** 72 小时内确认，7 天内给出初步评估或修复计划。请在修复前给我们合理的披露缓冲期。
- **范围：** 认证/会话、越权、密钥泄露、路径穿越、沙箱逃逸、SSRF、docker-proxy 绕过、命令注入、XSS、供应链与不安全默认值。
- **不算漏洞：** 用户主动批准 Bot 执行命令、需要管理员权限的 DoS、无实际利用的扫描器报告、第三方服务自身问题。
- **部署底线：** 使用 HTTPS、强随机 `SUPERBOT_SECRET_KEY`、修改默认管理员密码、执行 `deploy/harden-data-permissions.sh`、不要把 Docker socket 暴露给 Worker、审阅 MCP/Skill、定期备份并测试恢复。
