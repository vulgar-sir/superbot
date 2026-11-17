# superbot desktop

Electron shell for the superbot web app. It loads the self-hosted Web UI, adds native window, tray, menu, notification and auto-update plumbing, and stores the session token with Electron `safeStorage`.

This package is part of [superbot](../README.md) and is licensed under [AGPL-3.0](../LICENSE).

## What the desktop client adds

- Frameless window with custom 46×32 window controls.
- Native menu, tray icon and desktop notifications.
- Session token storage through Electron `safeStorage`.
- Local device agent (shell, screenshot, files and app control).
- Capture overlay and keyboard shortcuts.
- NSIS installer and an auto-update feed.

## Build and run

Requirements: Node.js 24+ and npm.

```bash
cd desktop
npm ci
npm run typecheck
npm run build

# Development: point the shell at your server.
SUPERBOT_WEB_URL=https://superbot.example.com npm run dev

# Installers
npm run package:win     # NSIS installer
npm run package:mac     # dmg + zip
npm run package:linux   # AppImage
```

Installers are written to `desktop/release/`.

## Configuration

| Setting | Purpose |
|---|---|
| `SUPERBOT_WEB_URL` | The server URL the desktop shell loads. Defaults to `http://127.0.0.1:3000` for local development. |
| `SUPERBOT_SERVER_URL` | Server URL used by the device agent. |
| `SUPERBOT_DEVICE_TOKEN` | Device token created in the Web UI. |
| `SUPERBOT_DEVICE_AUTO_APPROVE` | Development-only escape hatch. Never use it in a packaged or production build. |

For a packaged app, the device agent reads `device-agent.json` from Electron's `app.getPath("userData")`:

```json
{
  "serverUrl": "https://superbot.example.com",
  "token": "<device-token>"
}
```

## Local device agent

The main process polls the server for device jobs and executes `shell` jobs after a native approval dialog. Create a device in **Settings → 电脑** first; the token is shown only once.

Polled jobs carry an Ed25519 signature over `id|kind|command|nonce`. The main process fetches `/api/device/pubkey` and refuses jobs that fail verification. The device also generates its own Ed25519 keypair, registers the public key, and signs results and artifacts with a nonce. Unsigned or replayed results are rejected.

Packaged builds always show the native allow/deny dialog for each shell command. `SUPERBOT_DEVICE_AUTO_APPROVE=1` is honored only in unpackaged development builds for smoke tests.

## Auto-update feed

`electron-builder.yml` uses the generic provider:

```yaml
publish:
  provider: generic
  url: https://updates.example.com/superbot/desktop/
```

Replace the URL with the HTTPS directory that serves your releases. After each version bump, upload:

- `latest.yml` (required by electron-updater)
- `stable.yml` and `beta.yml` (the app resolves the channel-specific file on Windows; point all three at the same build until you have a separate beta)
- the installer (`.exe` / `.dmg` / `.AppImage`)
- the installer's `.blockmap`

Keep the version in `desktop/package.json`, `web/package.json` and both lockfiles in sync, otherwise the in-app version and the update comparison will disagree.

Unsigned builds trigger the operating system's "unknown publisher" warning. A production release should use a real code-signing certificate. Never commit certificates or signing credentials.

## Security notes

- Use HTTPS for `SUPERBOT_WEB_URL` in production. The device token authorizes command execution on your machine.
- Approve each local shell command deliberately; read the command before allowing it.
- The auto-approve switch is for unpackaged development builds only.
- The desktop client is a shell around the web UI; authorization still happens on the server.

## License

AGPL-3.0. See [LICENSE](../LICENSE) and [NOTICE](../NOTICE).
