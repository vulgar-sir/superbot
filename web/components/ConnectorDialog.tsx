"use client";

import { useEffect, useState } from "react";
import { Copy, ExternalLink, Link2, Loader2 } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import type { Connector, GitHubDeviceStart } from "@/lib/types";
import { Modal } from "./ui/Modal";
import { useToast } from "./ui/Toast";

const providers = [
  { key: "github", label: "GitHub" },
  { key: "slack", label: "Slack" },
  { key: "notion", label: "Notion" },
  { key: "onepassword", label: "1Password" },
  { key: "email", label: "Email (SMTP)" },
  { key: "linear", label: "Linear" },
  { key: "figma", label: "Figma" },
  { key: "vercel", label: "Vercel" },
  { key: "sentry", label: "Sentry" },
  { key: "postgres", label: "Postgres" },
  { key: "atlassian", label: "Atlassian" },
  { key: "google_docs", label: "Google Docs" },
  { key: "google_drive", label: "Google Drive" },
  { key: "canva", label: "Canva" },
  { key: "onedrive", label: "OneDrive" },
  { key: "msteams", label: "Microsoft Teams" },
];

export function ConnectorDialog({
  open,
  initialProvider,
  onClose,
  onCreated,
}: {
  open: boolean;
  initialProvider?: string;
  onClose: () => void;
  onCreated: (connector: Connector) => void;
}) {
  const { toast } = useToast();
  const [provider, setProvider] = useState(initialProvider ?? "github");
  const [name, setName] = useState("");
  const [token, setToken] = useState("");
  const [clientId, setClientId] = useState("");
  const [emailConfig, setEmailConfig] = useState({ host: "", port: "587", username: "", from: "" });
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [device, setDevice] = useState<GitHubDeviceStart | null>(null);
  const [waiting, setWaiting] = useState(false);
  const [oauthProviders, setOauthProviders] = useState<string[]>([]);
  const [oauthOpening, setOauthOpening] = useState(false);

  useEffect(() => {
    if (open) {
      setError(null);
      setToken("");
      setDevice(null);
      setWaiting(false);
      if (initialProvider) setProvider(initialProvider);
      void api
        .getOAuthStatus()
        .then((result) => setOauthProviders(result.providers))
        .catch(() => setOauthProviders([]));
    }
  }, [open, initialProvider]);

  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      if (event.data?.type === "superbot-oauth-connected") {
        setOauthOpening(false);
        void api.listConnectors().then((result) => {
          const connector = result.connectors.find(
            (item) => item.provider === event.data.provider,
          );
          if (connector) {
            onCreated(connector);
            onClose();
          } else {
            setError("授权成功，但未找到新的连接，请刷新后重试。");
          }
        });
      } else if (event.data?.type === "superbot-oauth-error") {
        setOauthOpening(false);
        setError(String(event.data.error ?? "OAuth 授权失败"));
      }
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [onClose, onCreated]);

  const oauthReady = oauthProviders.includes(provider);

  useEffect(() => {
    if (!open || !device) return;
    let cancelled = false;
    const timer = window.setInterval(async () => {
      try {
        const result = await api.pollGitHubDevice(clientId, device.device_code);
        if (cancelled) return;
        if (!result.pending && result.connector) {
          window.clearInterval(timer);
          onCreated(result.connector);
          onClose();
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof ApiError ? err.message : "等待 GitHub 授权失败");
          setWaiting(false);
        }
      }
    }, Math.max(5, device.interval) * 1000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [open, device, clientId, onClose, onCreated]);

  const connectToken = async () => {
    if (!token.trim()) {
      setError("Token 必填");
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const config =
        provider === "email"
          ? {
              host: emailConfig.host.trim(),
              port: Number(emailConfig.port || "587"),
              username: emailConfig.username.trim(),
              from: emailConfig.from.trim(),
            }
          : {};
      const result = await api.createConnector({
        provider,
        name: name.trim() || providers.find((item) => item.key === provider)?.label || provider,
        token: token.trim(),
        config,
      });
      onCreated(result.connector);
      onClose();
      toast("连接已保存", "success");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "连接失败");
    } finally {
      setSaving(false);
    }
  };

  const startDevice = async () => {
    if (!clientId.trim()) {
      setError("GitHub OAuth App 的 Client ID 必填");
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const result = await api.startGitHubDevice(clientId.trim());
      setDevice(result);
      setWaiting(true);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "启动 GitHub 设备授权失败");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="连接应用"
      subtitle="Token 会加密存储；支持 OAuth 的应用可一键授权。"
      size="md"
      footer={
        <>
          <button type="button" className="sb-btn" data-variant="secondary" onClick={onClose}>
            取消
          </button>
          <button
            type="button"
            className="sb-btn"
            data-variant="primary"
            onClick={() => void connectToken()}
            disabled={saving || !token.trim()}
          >
            {saving && <Loader2 className="sb-spin h-3.5 w-3.5" />}
            用 Token 连接
          </button>
        </>
      }
    >
      <div className="space-y-4">
        {oauthReady && (
          <div className="rounded-xl border border-[var(--sb-border)] bg-[var(--sb-hover)] p-3.5">
            <div className="text-[13px] font-medium">一键连接</div>
            <p className="mt-1 text-[12px] leading-[18px] text-[var(--sb-text-2)]">
              跳转到 {providers.find((item) => item.key === provider)?.label ?? provider} 完成授权，凭据会加密保存。
            </p>
            <button
              type="button"
              className="sb-btn mt-2.5 w-full"
              data-variant="primary"
              data-size="sm"
              disabled={oauthOpening}
              onClick={() => {
                setError(null);
                setOauthOpening(true);
                const popup = window.open(
                  `/api/connectors/oauth/${provider}/start`,
                  "superbot-oauth",
                  "width=520,height=680",
                );
                if (!popup) {
                  setOauthOpening(false);
                  setError("浏览器拦截了授权窗口，请允许弹窗后重试。");
                }
              }}
            >
              {oauthOpening ? <Loader2 className="sb-spin h-3.5 w-3.5" /> : <Link2 className="h-3.5 w-3.5" />}
              一键连接 {providers.find((item) => item.key === provider)?.label ?? provider}
            </button>
          </div>
        )}

        <label className="block">
          <span className="sb-field-label">应用</span>
          <select className="sb-field" value={provider} onChange={(event) => setProvider(event.target.value)}>
            {providers.map((item) => (
              <option key={item.key} value={item.key}>
                {item.label}
              </option>
            ))}
          </select>
        </label>
        <label className="block">
          <span className="sb-field-label">名称</span>
          <input
            className="sb-field"
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder={providers.find((item) => item.key === provider)?.label}
          />
        </label>

        {provider === "github" && (
          <div className="rounded-xl border border-[var(--sb-border)] p-3.5">
            <div className="text-[13px] font-medium">GitHub 设备码授权</div>
            <p className="mt-1 text-[12px] leading-[18px] text-[var(--sb-text-2)]">
              需要一个 GitHub OAuth App 的 Client ID（无需 client secret）。也支持直接在下方填 PAT。
            </p>
            <div className="mt-2.5 flex gap-2">
              <input
                className="sb-field font-[var(--sb-mono)] !text-[12px]"
                value={clientId}
                onChange={(event) => setClientId(event.target.value)}
                placeholder="OAuth App Client ID"
              />
              <button
                type="button"
                onClick={() => void startDevice()}
                disabled={saving}
                className="sb-btn shrink-0"
                data-variant="secondary"
                data-size="sm"
              >
                开始授权
              </button>
            </div>
            {device && (
              <div className="mt-3 rounded-lg bg-[var(--sb-warn-soft)] p-3">
                <div className="text-[12px] text-[var(--sb-warn)]">
                  打开 {device.verification_uri}，输入设备码：
                </div>
                <div className="mt-2 flex items-center gap-2">
                  <span className="font-[var(--sb-mono)] text-[20px] tracking-[0.18em] text-[var(--sb-text)]">
                    {device.user_code}
                  </span>
                  <button
                    type="button"
                    onClick={() => {
                      void navigator.clipboard.writeText(device.user_code);
                      toast("设备码已复制", "success");
                    }}
                    className="text-[var(--sb-text-2)] transition hover:text-[var(--sb-text)]"
                    aria-label="复制设备码"
                  >
                    <Copy className="h-3.5 w-3.5" />
                  </button>
                  <a
                    href={device.verification_uri}
                    target="_blank"
                    rel="noreferrer"
                    className="ml-auto text-[var(--sb-accent-text)]"
                    aria-label="打开 GitHub"
                  >
                    <ExternalLink className="h-4 w-4" />
                  </a>
                </div>
                {waiting && (
                  <div className="mt-2 flex items-center gap-1.5 text-[12px] text-[var(--sb-text-2)]">
                    <Loader2 className="sb-spin h-3 w-3" /> 等待授权…
                  </div>
                )}
              </div>
            )}
          </div>
        )}

        {provider === "email" && (
          <div className="grid grid-cols-2 gap-2 rounded-xl border border-[var(--sb-border)] p-3.5">
            <input
              className="sb-field"
              value={emailConfig.host}
              onChange={(event) => setEmailConfig((prev) => ({ ...prev, host: event.target.value }))}
              placeholder="SMTP host"
            />
            <input
              className="sb-field"
              value={emailConfig.port}
              onChange={(event) => setEmailConfig((prev) => ({ ...prev, port: event.target.value }))}
              placeholder="587"
            />
            <input
              className="sb-field"
              value={emailConfig.username}
              onChange={(event) => setEmailConfig((prev) => ({ ...prev, username: event.target.value }))}
              placeholder="SMTP username"
            />
            <input
              className="sb-field"
              value={emailConfig.from}
              onChange={(event) => setEmailConfig((prev) => ({ ...prev, from: event.target.value }))}
              placeholder="from@example.com"
            />
          </div>
        )}

        <label className="block">
          <span className="sb-field-label">
            {provider === "github"
              ? "Personal Access Token（可选）"
              : provider === "email"
                ? "SMTP 密码"
                : "Access Token"}
          </span>
          <input
            className="sb-field font-[var(--sb-mono)] !text-[12px]"
            type="password"
            value={token}
            onChange={(event) => setToken(event.target.value)}
            placeholder="token…"
          />
        </label>

        {error && (
          <div className="rounded-lg bg-[var(--sb-danger-soft)] px-3 py-2 text-[12px] text-[var(--sb-danger)]">
            {error}
          </div>
        )}
      </div>
    </Modal>
  );
}
