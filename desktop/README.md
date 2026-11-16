# superbot desktop

Electron shell for the superbot web app. It loads the production Web UI, adds native
window/tray/menu/notification/auto-update plumbing, and stores the session token with
Electron `safeStorage`.

## Local device agent

The main process can poll the server for device jobs and execute `shell` jobs after a
native approval dialog. Configure it with either:

- `SUPERBOT_DEVICE_TOKEN` / `SUPERBOT_SERVER_URL` environment variables in development, or
- `device-agent.json` in Electron `app.getPath("userData")`:

```json
{"serverUrl":"http://202.189.8.109:32608","token":"<device-token>"}
```

Create a device in the Web UI/API first; the token is shown once. Packaged builds always
show the native allow/deny dialog for each shell command. `SUPERBOT_DEVICE_AUTO_APPROVE=1`
is honored only in unpackaged development builds for smoke tests.

Polled jobs carry an Ed25519 signature over `id|kind|command|nonce`; the main process
fetches `/api/device/pubkey` and refuses jobs that fail verification. The device also
generates an Ed25519 keypair, registers its public key, and signs results/artifacts with a
nonce. Unsigned or replayed results are rejected. Capture overlay and broader cross-platform
capabilities are not implemented.

```bash
npm install
SUPERBOT_WEB_URL=https://your-superbot.example npm run dev
npm run package:win
```

`publish.url` in `electron-builder.yml` must be replaced with the real update feed
before publishing. Code signing is required for a production release.


## Build and update feed

```bash
npm run package:win
```

The NSIS installer is written to `release/`. The current build is unsigned. The
production update feed is served at `http://202.189.8.109:32608/desktop/`; upload
`latest.yml`, the installer and its `.blockmap` there after each version bump.
