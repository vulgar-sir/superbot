"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { CalendarDays, Loader2, Plus, Trash2 } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import type { CalendarEvent, User } from "@/lib/types";
import { Rail } from "@/components/Rail";
import { useToast } from "@/components/ui/Toast";
import { useLanguage } from "@/components/LanguageProvider";

function localInput(date: Date) {
  const offset = date.getTimezoneOffset() * 60_000;
  return new Date(date.getTime() - offset).toISOString().slice(0, 16);
}

export default function CalendarPage() {
  const router = useRouter();
  const { t } = useLanguage();
  const { toast } = useToast();
  const [user, setUser] = useState<User | null>(null);
  const [events, setEvents] = useState<CalendarEvent[]>([]);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [location, setLocation] = useState("");
  const [startsAt, setStartsAt] = useState(localInput(new Date(Date.now() + 3_600_000)));
  const [endsAt, setEndsAt] = useState(localInput(new Date(Date.now() + 7_200_000)));
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [me, result] = await Promise.all([api.me(), api.listCalendarEvents()]);
      setUser(me.user);
      setEvents(result.events);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) router.replace("/login");
      else toast(err instanceof ApiError ? err.message : "读取日历失败", "danger");
    } finally {
      setLoading(false);
    }
  }, [router, toast]);

  useEffect(() => {
    void load();
  }, [load]);

  const create = async () => {
    if (!title.trim()) return;
    setSaving(true);
    try {
      const result = await api.createCalendarEvent({
        title: title.trim(),
        description,
        location,
        starts_at: new Date(startsAt).toISOString(),
        ends_at: new Date(endsAt).toISOString(),
      });
      setEvents((prev) => [...prev, result.event].sort((a, b) => a.starts_at.localeCompare(b.starts_at)));
      setTitle("");
      setDescription("");
      setLocation("");
      toast("日程已创建", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : t("calendar.createFailed"), "danger");
    } finally {
      setSaving(false);
    }
  };

  const remove = async (event: CalendarEvent) => {
    try {
      await api.deleteCalendarEvent(event.id);
      setEvents((prev) => prev.filter((item) => item.id !== event.id));
      toast("日程已删除", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "删除日程失败", "danger");
    }
  };

  if (!user) return <div className="flex h-screen items-center justify-center"><Loader2 className="h-5 w-5 animate-spin" /></div>;

  return (
    <div className="flex h-screen w-full overflow-hidden bg-[var(--sb-bg)]">
      <Rail user={user} onLogout={() => void api.logout().finally(() => router.replace("/login"))} />
      <main className="flex min-w-0 flex-1 flex-col overflow-y-auto pb-16 md:pb-0">
        <header className="sb-page-header flex h-[61px] shrink-0 items-center justify-between border-b border-[var(--sb-border)] px-6">
          <h1 className="text-[17px] font-semibold">{t("calendar.title")}</h1>
          <Link href="/meetings" className="sb-btn" data-variant="secondary" data-size="sm">
            {t("calendar.meetingNotes")}
          </Link>
        </header>
        <div className="mx-auto w-full max-w-[920px] px-6 py-6">
          <section className="rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-4">
            <div className="flex flex-wrap gap-2">
              <input value={title} onChange={(event) => setTitle(event.target.value)} placeholder="日程标题" className="field min-w-[220px] flex-1" />
              <input value={location} onChange={(event) => setLocation(event.target.value)} placeholder="地点（可选）" className="field w-[180px]" />
            </div>
            <div className="mt-2 flex flex-wrap items-center gap-2">
              <input type="datetime-local" value={startsAt} onChange={(event) => setStartsAt(event.target.value)} className="field w-[210px]" />
              <span className="text-[12px] text-[var(--sb-text-3)]">{t("calendar.to")}</span>
              <input type="datetime-local" value={endsAt} onChange={(event) => setEndsAt(event.target.value)} className="field w-[210px]" />
              <button type="button" onClick={() => void create()} disabled={saving || !title.trim()} className="sb-btn" data-variant="primary" data-size="sm">
                {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
                {t("calendar.create")}
              </button>
            </div>
            <input value={description} onChange={(event) => setDescription(event.target.value)} placeholder="描述（可选）" className="field mt-2 w-full" />
          </section>

          {loading ? (
            <div className="flex justify-center py-16"><Loader2 className="h-5 w-5 animate-spin" /></div>
          ) : events.length === 0 ? (
            <div className="mt-6 flex flex-col items-center rounded-2xl border border-dashed border-[var(--sb-border)] py-16 text-[13px] text-[var(--sb-text-2)]">
              <CalendarDays className="mb-3 h-7 w-7 text-[var(--sb-text-4)]" />
              {t("calendar.empty")}
            </div>
          ) : (
            <div className="mt-5 space-y-2">
              {events.map((event) => (
                <div key={event.id} className="flex items-center gap-3 rounded-xl border border-[var(--sb-border)] bg-[var(--sb-pane)] px-4 py-3">
                  <div className="w-[110px] shrink-0 text-[12px] text-[var(--sb-text-2)]">
                    <div>{new Date(event.starts_at).toLocaleDateString()}</div>
                    <div>{new Date(event.starts_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</div>
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-[14px] font-medium">{event.title}</div>
                    <div className="truncate text-[12px] text-[var(--sb-text-3)]">
                      {event.location || "无地点"}
                      {event.description ? ` · ${event.description}` : ""}
                    </div>
                  </div>
                  <button type="button" onClick={() => void remove(event)} className="sb-icon-btn" aria-label="删除日程">
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      </main>
    </div>
  );
}
