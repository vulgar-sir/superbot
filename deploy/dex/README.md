# Self-hosted OIDC (Dex)

The compose stack runs `ghcr.io/dexidp/dex` behind Caddy at `/dex`; the API
reads `SUPERBOT_OIDC_ISSUER`, `SUPERBOT_OIDC_CLIENT_ID`,
`SUPERBOT_OIDC_CLIENT_SECRET`, `SUPERBOT_OIDC_REDIRECT_URL` from `.env`.

1. Copy `config.example.yaml` to `/srv/superbot/dex/config.yaml` and replace:
   - `YOUR_HOST` with the public host (including the port when non-default, e.g. `superbot.example.com:32608`)
   - `REPLACE_WITH_RANDOM_SECRET` with a random client secret
   - `YOUR_ADMIN_EMAIL` with the existing admin email (so OIDC logs into the
     same account instead of JIT-creating a second one)
   - `REPLACE_WITH_BCRYPT_HASH` with `bcrypt` of the SSO password
2. Add the four `SUPERBOT_OIDC_*` values to `.env` and recreate `api`/`worker`.
3. `curl http://HOST/dex/.well-known/openid-configuration` must return 200;
   `/api/auth/oidc/status` must report `configured: true`.

The production password used for verification is stored locally in
`tmp/dex/credentials.json` (not in git). Rotate it by regenerating the hash and
recreating Dex.
