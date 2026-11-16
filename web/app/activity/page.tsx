"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Activity as ActivityIcon, ArrowLeft, CheckCheck, Loader2, Search } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import type { Activity, Bot, User } from "@/lib/types";
import { Rail } from "@/components/Rail";
import { Menu } from "@/components/ui/Menu";
import { BotAvatar } from "@/components/BotAvatar";
import { useToast } from "@/components/ui/Toast";
import { useLanguage } from "@/components/LanguageProvider";
import { timeAgo } from "@/lib/format";

export default function ActivityPage() {
  const router = useRouter();
  const { t } = useLanguage();
  const { toast } = useToast();
  const [user, setUser] = useState<User | null>(null);
  const [activities, setActivities] = useState<Activity[]>([]);
  const [bots, setBots] = useState<Bot[]>([]);
  const [booting, setBooting] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [ctx, setCtx] = useState<{ activity: Activity; x: number; y: number } | null>(null);

  const load = useCallback(async () => {
    const [activitiesResponse, botsResponse] = await Promise.all([
      api.listActivities(),
      api.listBots(),
    ]);
    setActivities(activitiesResponse.activities);
    setBots(botsResponse.bots);
  }, []);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const me = await api.me();
        const [activitiesResponse, botsResponse] = await Promise.all([
          api.listActivities(),
          api.listBots(),
        ]);
        if (cancelled) return;
        setUser(me.user);
        setActivities(activitiesResponse.activities);
        setBots(botsResponse.bots);
        setBooting(false);
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "加载失败");
          setBooting(false);
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [router]);

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } finally {
      router.replace("/login");
    }
  }, [router]);

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return activities;
    return activities.filter((activity) =>
      `${activity.bot_name} ${activity.summary}`.toLowerCase().includes(needle),
    );
  }, [activities, query]);

  const unreadCount = activities.filter((activity) => !activity.read).length;

  const openActivity = useCallback(
    async (activity: Activity) => {
      try {
        await api.markActivitiesRead(activity.bot_id);
        if (activity.conversation_id) {
          const message = activity.message_id ? `&message=${activity.message_id}` : "";
          router.push(`/?conv=${activity.conversation_id}${message}`);
          return;
        }
        const result = await api.createConversation({ kind: "direct", bot_id: activity.bot_id });
        router.push(`/?conv=${result.conversation.id}`);
      } catch (err) {
        setError(err instanceof ApiError ? err.message : "打开会话失败");
      }
    },
    [router],
  );

  const markAll = useCallback(async () => {
    try {
      await api.markActivitiesRead();
      await load();
      toast("已全部标记为已读", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "操作失败", "danger");
    }
  }, [load, toast]);

  if (booting) {
    return (
      <div className="flex h-screen items-center justify-center bg-[var(--sb-bg)]">
        <Loader2 className="sb-spin h-5 w-5 text-[var(--sb-text-3)]" />
      </div>
    );
  }
  if (!user) {
    return (
      <div className="flex h-screen items-center justify-center bg-[var(--sb-bg)]">
        <Link href="/login" className="text-[var(--sb-accent-text)]">
          去登录
        </Link>
      </div>
    );
  }

  return (
    <div className="flex h-screen w-full overflow-hidden bg-[var(--sb-bg)]">
      <Rail user={user} onLogout={logout} />
      <main className="flex min-w-0 flex-1 flex-col pb-16 md:pb-0">
        <header className="sb-page-header flex h-[61px] shrink-0 items-center justify-between gap-3 border-b border-[var(--sb-border)] px-6">
          <div className="flex items-center gap-3">
            <Link href="/" className="sb-icon-btn md:hidden" aria-label="返回会话">
              <ArrowLeft className="h-4 w-4" />
            </Link>
            <div className="flex items-center gap-2">
              <h1 className="text-[15px] font-semibold">{t("nav.activity")}</h1>
              {unreadCount > 0 && (
                <span className="rounded-full bg-[var(--sb-hover)] px-2 py-0.5 text-[11px] text-[var(--sb-text-2)]">
                  {unreadCount} 条未读
                </span>
              )}
            </div>
          </div>
          <div className="flex items-center gap-2">
            {unreadCount > 0 && (
              <button
                type="button"
                onClick={() => void markAll()}
                className="sb-btn"
                data-variant="secondary"
                data-size="sm"
              >
                <CheckCheck className="h-3.5 w-3.5" />
                全部已读
              </button>
            )}
            <label className="flex h-9 items-center gap-2 rounded-full border border-[var(--sb-border)] bg-[var(--sb-pane)] px-3">
              <Search className="h-3.5 w-3.5 text-[var(--sb-text-3)]" />
              <input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder={t("activity.searchPlaceholder")}
                className="w-40 bg-transparent text-[12px] outline-none placeholder:text-[var(--sb-text-4)] sm:w-56"
              />
            </label>
          </div>
        </header>

        <div className="flex-1 overflow-y-auto">
          <div className="mx-auto w-full max-w-[820px] px-6 py-6">
            {error && (
              <div className="mb-4 rounded-xl bg-[var(--sb-danger-soft)] px-4 py-3 text-[12px] text-[var(--sb-danger)]">
                {error}
              </div>
            )}
            {filtered.length === 0 ? (
              <div className="sb-empty min-h-[320px] rounded-2xl border border-[var(--sb-border)]">
                <ActivityIcon className="h-6 w-6" />
                <div className="sb-empty-title">{query ? t("activity.noMatch") : t("activity.empty")}</div>
                <div className="sb-empty-desc">Bot 回复、例程和队友私信都会出现在这里。</div>
              </div>
            ) : (
              <div className="space-y-1.5">
                {filtered.map((activity) => (
                  <button
                    key={activity.id}
                    type="button"
                    onClick={() => void openActivity(activity)}
                    onContextMenu={(event) => {
                      event.preventDefault();
                      setCtx({ activity, x: event.clientX, y: event.clientY });
                    }}
                    className="flex w-full items-start gap-3 rounded-xl px-3 py-3 text-left transition hover:bg-[var(--sb-hover)]"
                  >
                    <BotAvatar
                      name={activity.bot_name}
                      id={activity.bot_id}
                      size={34}
                      shape={bots.find((bot) => bot.id === activity.bot_id)?.avatar_shape}
                      color={bots.find((bot) => bot.id === activity.bot_id)?.avatar_color}
                      avatar={bots.find((bot) => bot.id === activity.bot_id)?.avatar}
                    />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="truncate text-[13px] font-medium">{activity.bot_name}</span>
                        {!activity.read && <span className="sb-unread-dot !h-[6px] !w-[6px]" />}
                        <span className="ml-auto shrink-0 text-[11px] text-[var(--sb-text-4)]">
                          {timeAgo(activity.created_at)}
                        </span>
                      </div>
                      <div className="mt-0.5 line-clamp-3 text-[12.5px] leading-[19px] text-[var(--sb-text-2)]">
                        {activity.summary}
                      </div>
                    </div>
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>
      </main>
      <Menu
        open={ctx !== null}
        anchor={ctx ? { x: ctx.x, y: ctx.y } : null}
        items={
          ctx
            ? [
                {
                  key: "open",
                  label: "打开会话",
                  onSelect: () => void openActivity(ctx.activity),
                },
                {
                  key: "read",
                  label: "标记已读",
                  disabled: ctx.activity.read,
                  onSelect: () => {
                    void api
                      .markActivitiesRead(ctx.activity.bot_id)
                      .then(() => load())
                      .catch(() => undefined);
                  },
                },
                {
                  key: "copy",
                  label: "复制摘要",
                  onSelect: () => {
                    void navigator.clipboard.writeText(ctx.activity.summary);
                    toast("已复制", "success");
                  },
                },
              ]
            : []
        }
        onClose={() => setCtx(null)}
      />
    </div>
  );
}
