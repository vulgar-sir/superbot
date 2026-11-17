# Contributing to superbot

Thanks for taking the time to improve superbot. Contributions of every size are welcome — bug reports, documentation, translations, UI polish, tests, and features.

**English** · [简体中文](CONTRIBUTING.zh-CN.md)

By participating, you agree to follow our [Code of Conduct](CODE_OF_CONDUCT.md). Security issues must be reported privately according to [SECURITY.md](SECURITY.md), never in a public issue.

## Ways to contribute

- **Report a bug** with a minimal reproduction, logs and environment details.
- **Request a feature** by describing the problem and the workflow you need, not only the implementation you have in mind.
- **Improve documentation** — especially translations of the Chinese deep-dive documents.
- **Write tests** for an area that is currently untested.
- **Send a pull request** for a focused fix or feature.

## Before you start

1. Search existing issues and pull requests to avoid duplicate work.
2. For a large change (new subsystem, database migration, security-sensitive code, new dependency, breaking API change), open an issue first so we can agree on scope and design before you invest time.
3. Keep pull requests small and focused. A series of reviewable commits is easier to merge than one large rewrite.
4. If you use AI-assisted coding tools, you are still responsible for understanding, reviewing and testing every line you submit.

## Development environment

### Requirements

- Go 1.27+
- Node.js 24+
- Docker Desktop (or Docker Engine on Linux)
- [Task](https://taskfile.dev/) is optional but convenient

### Backend and web

```bash
cp .env.example .env
# Fill in SUPERBOT_SECRET_KEY and, optionally, an LLM API key.

task dev-infra   # PostgreSQL on 127.0.0.1:55432
task migrate     # apply goose migrations

task api         # Go API on :8080
task worker      # Go worker
task web         # Next.js on :3000
```

Without Task:

```bash
docker compose -f deploy/docker-compose.dev.yml up -d postgres
go run ./cmd/superbot migrate
go run ./cmd/superbot serve
go run ./cmd/superbot worker
cd web && npm run dev
```

### Desktop

```bash
cd desktop
npm ci
npm run typecheck
SUPERBOT_WEB_URL=http://127.0.0.1:3000 npm run dev
```

### Tests

```bash
# Backend (no external services required for unit tests)
go test ./...
go vet ./...

# Integration tests against a disposable PostgreSQL database
SUPERBOT_TEST_DATABASE_URL="postgres://superbot:superbot@127.0.0.1:55432/superbot_test?sslmode=disable" \
  go test -tags integration -run Integration ./internal/agent/...

# Web
cd web
npm run typecheck
npm run build
npm run test:e2e        # requires a running server; set SUPERBOT_E2E_BASE_URL

# Desktop
cd desktop
npm run typecheck
```

The CI workflow runs gofmt, `go vet`, sqlc generation checks, Go tests, integration tests with a real Postgres, and the Next.js typecheck/build.

## Project layout

| Path | Contents |
|---|---|
| `cmd/superbot` | Entry point (`serve`, `worker`, `migrate`, `device`, …) |
| `internal/agent` | Agent loop, orchestration, context management |
| `internal/llm` | Provider normalization (OpenAI-compatible + Anthropic) |
| `internal/tools` | File, shell, Python, web, memory and skill tools |
| `internal/sandbox` | Docker sandbox and computer lifecycle |
| `internal/httpapi` | REST, SSE, WebSocket and noVNC proxying |
| `internal/memory` | Recall, deduplication, review and consolidation |
| `internal/store` | sqlc-generated database access |
| `migrations/` | goose migrations |
| `queries/` | sqlc queries |
| `web/` | Next.js 16 frontend |
| `desktop/` | Electron client and local device agent |
| `deploy/` | Compose stack, Caddy, images, scripts |
| `docs/` | Engineering notes and verification records |

## Coding standards

### Go

- Run `gofmt`; code must be formatted.
- Run `go vet ./...`.
- Wrap errors with `%w` and add context. Do not swallow errors silently.
- Pass `context.Context` through request and tool paths.
- Never log secrets, tokens, cookies or full provider payloads.
- File tools must stay within their `os.Root`; do not add path handling that bypasses it.
- Treat MCP servers, skills, model output and fetched web content as untrusted input.
- Keep database changes in a new numbered goose migration. Run `sqlc generate` and commit the regenerated `internal/store` code — do not edit generated files by hand.
- Add tests for behavior, not implementation details. Table-driven tests are preferred.
- Avoid new dependencies unless they are well maintained and clearly justified.

### TypeScript / Next.js

- Keep TypeScript strict; run `npm run typecheck` and `npm run build`.
- Do not hardcode user-facing strings: add keys to both the Chinese and English dictionaries.
- Preserve keyboard navigation, focus management and accessible labels.
- UI changes should include a screenshot or a short recording in the pull request.
- Do not put real credentials, server addresses or personal data in screenshots, fixtures or tests.

### Desktop

- Run `npm run typecheck` before opening a pull request.
- Device-agent changes must preserve signature verification, replay protection and the native approval dialog.
- Auto-update changes must be tested with an unsigned development build; do not commit signing certificates or credentials.

### Documentation

- Update [`docs/functional-parity-matrix.md`](docs/functional-parity-matrix.md) only when you have real evidence for the new status.
- If you change configuration, update both `.env.example` and `deploy/.env.production.example`.
- Bilingual documentation is encouraged. If you cannot write both languages, open the pull request in one language and note that a translation is needed.

## Pull requests

A good pull request:

- explains **what** changed and **why**;
- references the related issue when one exists;
- includes tests for new behavior and describes how you tested it;
- includes screenshots for UI changes;
- updates the changelog for user-visible changes;
- keeps unrelated refactors out of the same change;
- passes CI.

Please make sure you have the right to submit the code and that it does not include copied proprietary code, credentials or personal data.

## Commit messages

Write imperative, concise messages that describe the behavior change:

```
Add per-bot memory import
Fix SSE reconnect after worker restart
Document the OIDC callback URL
```

Conventional prefixes (`feat:`, `fix:`, `docs:`) are welcome but not required.

## License

superbot is licensed under the [GNU Affero General Public License v3.0](LICENSE). By submitting a contribution, you agree that your contribution is licensed under the same terms and that you have the right to grant that license.

If you incorporate third-party code, it must be compatible with the AGPL and you must preserve its copyright and license notice. Add the attribution to [`ACKNOWLEDGMENTS.md`](ACKNOWLEDGMENTS.md) or the relevant source file.

## Questions

If something in this guide is unclear, open a discussion or an issue with the `question` label. We would rather answer a question than see you struggle.
