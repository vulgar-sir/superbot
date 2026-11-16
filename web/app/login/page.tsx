"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { Fingerprint, Loader2, LogIn, UserRound } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import { loginWithPasskey } from "@/lib/webauthn";
import { Logo } from "@/components/Logo";
import { useLanguage } from "@/components/LanguageProvider";

export default function LoginPage() {
  const router = useRouter();
  const { t } = useLanguage();
  const [email, setEmail] = useState("admin@superbot.local");
  const [password, setPassword] = useState("");
  const [loading, setLoading] = useState(false);
  const [passkeyLoading, setPasskeyLoading] = useState(false);
  const [ssoEnabled, setSsoEnabled] = useState(false);
  const [hydrated, setHydrated] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const emailRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    setHydrated(true);
    const oidcError = new URLSearchParams(window.location.search).get("oidc_error");
    if (oidcError) setError(oidcError);
    api
      .getOIDCStatus()
      .then((status) => setSsoEnabled(status.configured))
      .catch(() => undefined);
    api
      .me()
      .then(() => router.replace("/"))
      .catch(() => undefined);
  }, [router]);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    setLoading(true);
    try {
      await api.login(email.trim(), password);
      router.replace("/");
    } catch (err) {
      if (err instanceof ApiError) setError(err.message);
      else setError("登录失败，请检查服务是否启动");
    } finally {
      setLoading(false);
    }
  }

  async function passkeyLogin() {
    setError(null);
    setPasskeyLoading(true);
    try {
      await loginWithPasskey(email.trim());
      router.replace("/");
    } catch (err) {
      setError(err instanceof Error ? err.message : "安全密钥登录失败");
    } finally {
      setPasskeyLoading(false);
    }
  }

  function switchAccount() {
    setPassword("");
    setError(null);
    emailRef.current?.focus();
    emailRef.current?.select();
  }

  return (
    <main className="flex min-h-screen w-full items-center justify-center bg-[var(--sb-bg)] px-6 py-10">
      <div className="w-full max-w-[500px]">
        <div className="flex justify-center">
          <Logo compact />
        </div>

        <div className="mt-8">
          <h1 className="text-[20px] font-medium tracking-[-0.02em] text-[var(--sb-text)]">
            {t("auth.title")}
          </h1>
          <p className="mt-2 text-[14px] leading-[22px] text-[var(--sb-text-2)]">
            {t("auth.subtitle")}
          </p>

          <form onSubmit={submit} className="mt-7">
            <div className="overflow-hidden rounded-lg border border-[var(--sb-border)] bg-[var(--sb-pane)]">
              <div className="flex items-center gap-3 px-4 py-3">
                <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-[var(--sb-hover)] text-[var(--sb-text-3)]">
                  <UserRound className="h-5 w-5" />
                </span>
                <div className="min-w-0 flex-1">
                  <input
                    ref={emailRef}
                    className="w-full bg-transparent text-[14px] font-medium text-[var(--sb-text)] outline-none"
                    type="email"
                    value={email}
                    onChange={(event) => setEmail(event.target.value)}
                    autoComplete="username"
                    aria-label="邮箱"
                    required
                  />
                  <div className="mt-0.5 text-[12px] text-[var(--sb-text-3)]">
                    {t("auth.adminAccount")}
                  </div>
                </div>
              </div>
              <div className="border-t border-[var(--sb-border)] px-4 py-3">
                <label className="mb-1 block text-[12px] text-[var(--sb-text-2)]">{t("auth.password")}</label>
                <input
                  className="w-full bg-transparent text-[14px] text-[var(--sb-text)] outline-none placeholder:text-[var(--sb-text-4)]"
                  type="password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  autoComplete="current-password"
                  placeholder={t("auth.passwordPlaceholder")}
                  required
                />
              </div>
            </div>

            <div className="mt-3 rounded-lg border border-[var(--sb-border)] bg-[var(--sb-pane)] px-4 py-3">
              <div className="text-[13px] font-medium">{t("auth.before")}</div>
              <p className="mt-1 text-[13px] leading-[20px] text-[var(--sb-text-2)]">
                {t("auth.beforeDesc")}
              </p>
            </div>

            {error && (
              <div className="mt-3 rounded-lg bg-[var(--sb-danger-soft)] px-3.5 py-2.5 text-[12px] leading-[18px] text-[var(--sb-danger)]">
                {error}
              </div>
            )}

            <div className="mt-4 flex items-center gap-3">
              <button
                type="button"
                onClick={switchAccount}
                className="flex h-10 flex-1 items-center justify-center rounded-lg bg-[var(--sb-hover)] text-[13px] font-medium text-[var(--sb-text)] transition hover:bg-[var(--sb-active)]"
              >
                {t("auth.cancel")}
              </button>
              <button
                type="submit"
                disabled={loading || !hydrated}
                className="flex h-10 flex-1 items-center justify-center gap-2 rounded-lg bg-[var(--sb-primary)] text-[13px] font-medium text-[var(--sb-primary-contrast)] transition hover:opacity-90 disabled:opacity-50"
              >
                {loading && <Loader2 className="sb-spin h-4 w-4" />}
                {t("auth.login")}
              </button>
            </div>

            <p className="mt-5 text-center text-[12px] leading-[18px] text-[var(--sb-text-3)]">
              {t("auth.footer")}
            </p>
            <div className="mt-3 flex flex-wrap items-center justify-center gap-2 text-[12px]">
              {ssoEnabled && (
                <>
                  <button
                    type="button"
                    onClick={() => {
                      window.location.href = "/api/auth/oidc/start";
                    }}
                    className="inline-flex items-center gap-1.5 text-[var(--sb-text-2)] underline decoration-[var(--sb-border-strong)] underline-offset-4 transition hover:text-[var(--sb-text)]"
                  >
                    <LogIn className="h-3.5 w-3.5" />
                    {t("auth.sso")}
                  </button>
                  <span className="text-[var(--sb-text-4)]">{t("auth.or")}</span>
                </>
              )}
              <button
                type="button"
                onClick={() => void passkeyLogin()}
                disabled={passkeyLoading}
                className="inline-flex items-center gap-1.5 text-[var(--sb-text-2)] underline decoration-[var(--sb-border-strong)] underline-offset-4 transition hover:text-[var(--sb-text)] disabled:opacity-50"
              >
                {passkeyLoading ? (
                  <Loader2 className="sb-spin h-3.5 w-3.5" />
                ) : (
                  <Fingerprint className="h-3.5 w-3.5" />
                )}
                {t("auth.passkey")}
              </button>
              <span className="text-[var(--sb-text-4)]">{t("auth.or")}</span>
              <button
                type="button"
                onClick={switchAccount}
                className="text-[var(--sb-text-2)] underline decoration-[var(--sb-border-strong)] underline-offset-4 transition hover:text-[var(--sb-text)]"
              >
                {t("auth.otherAccount")}
              </button>
            </div>
          </form>
        </div>
      </div>
    </main>
  );
}
