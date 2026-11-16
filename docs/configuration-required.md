# 需要外部条件的配置与验收（2026-11-13）

以下能力代码已完成，但需要外部条件才能做“真实第三方验收”。在条件到位前，界面与 `/api/capabilities` 会显示 `requires_config` / 平台限制，不会伪造成功。

## 1. Passkey / WebAuthn（生产 HTTP IP 仍受限）

- 条件：HTTPS 域名（或 localhost）；浏览器在非安全上下文直接拒绝 WebAuthn
- 配置：`SUPERBOT_WEBAUTHN_RPID`、`SUPERBOT_WEBAUTHN_ORIGINS`（逗号分隔）；未配置时按请求 Host/Proto 推导
- 已用 localhost 安全上下文 + Chrome CDP 虚拟认证器完成真实注册/登录/删除（2026-11-13），协议路径已验证
- 已修复非默认端口 origin 不匹配：Caddy `X-Forwarded-Host` 改用 `{hostport}`，否则 `:32608` 被丢弃
- **已用 Cloudflare Quick Tunnel 的真实 HTTPS 域名完成验收（2026-11-14）**：注册凭据 → 清 Cookie → 安全密钥登录成功 → 删除 204；隧道由 cloudflared 提供、证书受浏览器信任、无警告页。生产 HTTP IP 仍非安全上下文，正式上线仍需自己的域名 + TLS；证据 `tmp/parity/passkey/https-cloudflare.md`
- 生产 80/443 入站被云防火墙拦截（2026-11-14 实测：服务器 `python -m http.server 80` 正常监听，公网 `curl http://202.189.8.109/` 仍为 000），因此无法用 sslip.io/nip.io + Let's Encrypt 自动签发；需要放通 80/443 或提供已有域名证书
- 验收：登录页注册/使用安全密钥；设置→登录方式 列出凭据

## 2. 真实语义记忆（已自托管，无需外部 key）

- 已部署 `embedding` 服务：`deploy/images/embedding`（fastembed 0.9.1 + `intfloat/multilingual-e5-large`，1024 维），模型首次启动从 `hf-mirror.com` 下载并缓存在 `superbot_embedding_models` 卷；`HF_HUB_DISABLE_XET=1` 避免镜像的 Xet CAS 下载卡住
- 配置：`SUPERBOT_EMBEDDING_BASE_URL=http://embedding:8090`、`SUPERBOT_EMBEDDING_MODEL=intfloat/multilingual-e5-large`、任意非空 `SUPERBOT_EMBEDDING_API_KEY`（服务不校验值）
- 切换后执行：`docker exec superbot-api-1 /app/superbot reembed-memory`
- 验收（2026-11-13 已通过）：服务 `/healthz` dim=1024；6 条记忆迁到 `remote:intfloat/multilingual-e5-large`；查询「部署包放在哪个文件夹」最近邻命中「交付物保存在 /srv/releases 目录」0.8816，高于无关记忆；`/api/capabilities` 的 `memory_semantic` = available
- 外部 OpenAI 兼容 embedding 仍可作为替代：设置对应 Base URL/Key/Model 后重新 reembed

## 3. 电话 / SMS（requires_config）

- 条件：Twilio 账号与号码
- 配置：`SUPERBOT_TWILIO_ACCOUNT_SID`、`SUPERBOT_TWILIO_AUTH_TOKEN`、`SUPERBOT_TWILIO_FROM_NUMBER`
- 验收：`/api/phone/status` 配置为真；真实短信/外呼

## 4. X 草稿发布（requires_config）

- 条件：X API v2 凭据并连接 X 连接器
- 验收：Bot 调 `draft_x_post` 生成卡片；在 X 已连接时发布成功，否则明确返回 requires_config

## 5. Web Push 投递（Firefox 已验证；Chrome/FCM 受网络限制）

- 已实现订阅、VAPID、Service Worker 与测试接口
- **已真实验证送达**：Firefox + Mozilla autopush（服务器可达），localhost 安全上下文订阅 → 测试推送 → Firefox PushService 收到、解密并调用 `showNotification` 后 ack；证据 `tmp/parity/notifications/webpush-firefox.log`
- Chrome 使用 FCM，生产服务器**无法访问 fcm.googleapis.com**：4 个解析 IP 直连均 5 秒超时；临时 Tor SOCKS5 也 20 秒超时（2026-11-14 复核）。HTTP 下还需 `--unsafely-treat-insecure-origin-as-secure`
- 条件：服务器能访问 FCM（或配置出海代理）；Firefox 通道已可用
- Electron 自带 Chromium 无推送服务，桌面通知继续走原生 IPC

## 6. 邮件 / 日历

- 邮件：在扩展页连接 Email 连接器，填写 IMAP/SMTP host、username、from、密码
- 日历：CalDAV 已端到端验证；Google/Outlook OAuth 按用户要求暂不做

## 7. 桌面签名 / 自动更新

- Windows NSIS 安装/升级/卸载已实测；代码签名证书按用户要求暂不使用（安装时可能提示未知发布者）
- 自动更新 feed：`http://202.189.8.109:32608/desktop/`，当前发布 **0.1.2**
- feed 需要同时提供 `latest.yml`、`stable.yml`、`beta.yml`：electron-updater 在 Windows 按 `<channel>.yml` 解析，只放 `latest.yml` 时 stable/beta 会 404。当前只有一个构建，三份文件指向同一版本；出现独立 beta 后再单独覆盖 `beta.yml`
- 0.1.1 → 0.1.2 真实自动更新已实测：打包态启动检查 feed（sha512 与 latest.yml 一致）→ 退出时静默安装 → 落盘 `app.asar` 版本 0.1.2；0.1.2 打包态 `checkForUpdates` 返回 `latest 0.1.2`
- 发布新版本时必须同步更新 `web/package.json`、`desktop/package.json` 与两个 lockfile 的版本号，否则应用内版本显示与更新比较会不一致
- 发布入口：`deploy/publish-desktop-release.sh <desktop/release>`（默认写入 `/srv/superbot/deploy/desktop-release`，并同步 `stable.yml`/`beta.yml`）

## 8. 语音 STT/TTS（已自托管）

- compose 服务：`stt`（`fedirz/faster-whisper-server` + `Systran/faster-whisper-small`，固定中文 `WHISPER__DEFAULT_LANGUAGE=zh`，模型缓存在 `superbot_stt_models` 卷）与 `tts`（`superbot/tts`，Piper `zh_CN-huayan-medium`，音色缓存在 `superbot_tts_voices` 卷）
- `.env`：`SUPERBOT_STT_URL=http://stt:8000/v1/audio/transcriptions`、`SUPERBOT_STT_MODEL=whisper-1`、`SUPERBOT_STT_API_KEY=local-stt`、`SUPERBOT_TTS_URL=http://tts:8091/v1/audio/speech`、`SUPERBOT_TTS_MODEL=piper`、`SUPERBOT_TTS_VOICE=zh_CN-huayan-medium`、`SUPERBOT_TTS_API_KEY=local-tts`
- 验收（2026-11-13 已通过）：`/api/capabilities` voice=available；`/api/voice/speak` 返回 141KB WAV（22050Hz）；同一 WAV 经 `/api/voice/transcribe` 返回识别文本。说话人分离未做
- 说明：模型从 `hf-mirror.com` 下载；TTS 语音配置 JSON 随镜像内置（镜像对小 JSON 的重定向会拒绝 urllib）

## 9. OIDC / SSO（已自托管 Dex）

- compose `dex` 服务（`ghcr.io/dexidp/dex`，Docker Hub 在本机不可达，改用 GHCR），Caddy 暴露 `/dex/*`
- 配置：`/srv/superbot/dex/config.yaml`（模板 `deploy/dex/config.example.yaml`）与 `.env` 的 `SUPERBOT_OIDC_ISSUER/CLIENT_ID/CLIENT_SECRET/REDIRECT_URL`
- 验收（2026-11-14 已通过）：登录页「使用 SSO 登录」→ Dex 登录/授权 → callback → `/api/auth/me` 返回 admin@superbot.local（同邮箱复用管理员，无重复账号）；`/api/capabilities` 的 `oidc=available`
- 生产 SSO 密码保存在本地 `tmp/dex/credentials.json`（不入 git）；轮换时重新生成 bcrypt 并重建 Dex

## 10. 平台限制

- iMessage：仅 macOS 可用；Windows 显示平台限制
- Mac Mail / Notes / Obsidian / iOS 专属能力：`platform_limited`
