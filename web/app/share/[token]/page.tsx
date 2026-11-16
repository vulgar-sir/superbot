"use client";

import { useCallback, useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { Loader2, Sparkles } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import { BotAvatar } from "@/components/BotAvatar";
import { Logo } from "@/components/Logo";

type SharedProfile = {
  name: string;
  title: string;
  description: string;
  avatar_shape: string;
  avatar_color: string;
  skills: number;
  routines: number;
};

export default function SharePage() {
  const params = useParams<{ token: string }>();
  const router = useRouter();
  const token = params?.token ?? "";
  const [profile, setProfile] = useState<SharedProfile | null>(null);
  const [template, setTemplate] = useState<Record<string, unknown> | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [importing, setImporting] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const result = await api.getBotShare(token);
        if (cancelled) return;
        setProfile(result.profile);
        setTemplate(result.template);
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof ApiError ? err.message : "分享链接无效或已过期");
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [token]);

  const importBot = useCallback(async () => {
    if (!template || importing) return;
    setImporting(true);
    setError(null);
    try {
      const result = await api.importBotTemplate(template);
      const conversation = await api.createConversation({
        kind: "direct",
        bot_id: result.bot.id,
      });
      router.push(`/?conv=${conversation.conversation.id}`);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "导入失败");
    } finally {
      setImporting(false);
    }
  }, [importing, router, template]);

  return (
    <main className="flex min-h-screen w-full flex-col items-center justify-center bg-[var(--sb-bg)] px-6 py-10">
      <Logo />
      <div className="mt-8 w-full max-w-[420px] rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-6 shadow-[var(--sb-shadow-popup)]">
        {loading ? (
          <div className="flex justify-center py-16">
            <Loader2 className="sb-spin h-5 w-5 text-[var(--sb-text-3)]" />
          </div>
        ) : error && !profile ? (
          <div className="py-12 text-center">
            <Sparkles className="mx-auto h-6 w-6 text-[var(--sb-text-3)]" />
            <div className="mt-3 text-[15px] font-medium">无法打开分享链接</div>
            <p className="mt-1.5 text-[12px] text-[var(--sb-text-2)]">{error}</p>
            <Link
              href="/"
              className="sb-btn mt-5"
              data-variant="secondary"
            >
              返回 superbot
            </Link>
          </div>
        ) : profile ? (
          <div className="text-center">
            <div className="flex justify-center">
              <BotAvatar
                name={profile.name}
                id={`${token}-${profile.name}`}
                size={72}
                shape={profile.avatar_shape}
                color={profile.avatar_color}
              />
            </div>
            <h1 className="mt-4 text-[20px] font-semibold tracking-[-0.02em]">{profile.name}</h1>
            {profile.title && (
              <div className="mt-0.5 text-[12px] text-[var(--sb-text-2)]">{profile.title}</div>
            )}
            <p className="mt-3 text-[13px] leading-[20px] text-[var(--sb-text-2)]">
              {profile.description || "一个可以导入到你的 superbot 的 AI 同事模板。"}
            </p>
            <div className="mt-4 flex items-center justify-center gap-2 text-[11px] text-[var(--sb-text-3)]">
              <span className="rounded-full bg-[var(--sb-hover)] px-2.5 py-1">
                {profile.skills} 个技能
              </span>
              <span className="rounded-full bg-[var(--sb-hover)] px-2.5 py-1">
                {profile.routines} 个例程
              </span>
            </div>
            <button
              type="button"
              onClick={() => void importBot()}
              disabled={importing}
              className="sb-btn mt-6 w-full"
              data-variant="primary"
              data-size="lg"
            >
              {importing ? <Loader2 className="sb-spin h-4 w-4" /> : <Sparkles className="h-4 w-4" />}
              导入一份到我的 superbot
            </button>
            {error && (
              <div className="mt-3 rounded-lg bg-[var(--sb-danger-soft)] px-3 py-2 text-[12px] text-[var(--sb-danger)]">
                {error}
              </div>
            )}
            <p className="mt-4 text-[11px] leading-[16px] text-[var(--sb-text-4)]">
              导入会创建一份独立副本，不会影响分享者的 Bot。
              <br />
              需要先登录你自己的 superbot 账号。
            </p>
          </div>
        ) : null}
      </div>
    </main>
  );
}
