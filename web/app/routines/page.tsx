"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  ArrowLeft,
  Clock,
  Loader2,
  Pause,
  Play,
  Plus,
  Trash2,
  X,
} from "lucide-react";

import { api, ApiError } from "@/lib/api";
import type { Routine, Team, User } from "@/lib/types";
import { Rail } from "@/components/Rail";
import { useLanguage } from "@/components/LanguageProvider";
import { StatusPill } from "@/components/StatusPill";
import { useConfirm } from "@/components/ui/Confirm";
import { cn, timeAgo } from "@/lib/format";

type RoutineForm = {
  team_id: string;
  name: string;
  cron: string;
  prompt: string;
  enabled: boolean;
};

const emptyForm: RoutineForm = {
  team_id: "",
  name: "",
  cron: "0 9 * * *",
  prompt: "",
  enabled: true,
};

export default function RoutinesPage() {
  const { t } = useLanguage();
  const router = useRouter();
  const { confirm, confirmElement } = useConfirm();
  const [user, setUser] = useState<User | null>(null);
  const [teams, setTeams] = useState<Team[]>([]);
  const [routines, setRoutines] = useState<Routine[]>([]);
  const [booting, setBooting] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [form, setForm] = useState<RoutineForm>(emptyForm);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const me = await api.me();
        const [teamsResponse, routinesResponse] = await Promise.all([
          api.listTeams(),
          api.listRoutines(),
        ]);
        if (cancelled) return;
        setUser(me.user);
        setTeams(teamsResponse.teams);
        setRoutines(routinesResponse.routines);
        setBooting(false);
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setError(err instanceof Error ? err.message : "加载失败");
        setBooting(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [router]);

  const teamName = useMemo(
    () => (id: string) => teams.find((team) => team.id === id)?.name ?? "未知团队",
    [teams],
  );

  const openCreate = useCallback(() => {
    setEditingId(null);
    setForm({ ...emptyForm, team_id: teams[0]?.id ?? "" });
    setError(null);
    setEditorOpen(true);
  }, [teams]);

  const openEdit = useCallback((routine: Routine) => {
    setEditingId(routine.id);
    setForm({
      team_id: routine.team_id,
      name: routine.name,
      cron: routine.cron,
      prompt: routine.prompt,
      enabled: routine.enabled,
    });
    setError(null);
    setEditorOpen(true);
  }, []);

  const save = useCallback(async () => {
    if (!form.team_id || !form.name.trim() || !form.cron.trim() || !form.prompt.trim()) {
      setError("团队、名称、cron 和任务内容必填");
      return;
    }
    setSaving(true);
    setError(null);
    try {
      if (editingId) {
        const result = await api.updateRoutine(editingId, {
          name: form.name.trim(),
          cron: form.cron.trim(),
          prompt: form.prompt.trim(),
          enabled: form.enabled,
        });
        setRoutines((prev) => prev.map((item) => (item.id === editingId ? result.routine : item)));
      } else {
        const result = await api.createRoutine({
          team_id: form.team_id,
          name: form.name.trim(),
          cron: form.cron.trim(),
          prompt: form.prompt.trim(),
          enabled: form.enabled,
        });
        setRoutines((prev) => [result.routine, ...prev]);
      }
      setEditorOpen(false);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "保存失败");
    } finally {
      setSaving(false);
    }
  }, [editingId, form]);

  const toggle = useCallback(async (routine: Routine) => {
    try {
      const result = await api.updateRoutine(routine.id, {
        name: routine.name,
        cron: routine.cron,
        prompt: routine.prompt,
        enabled: !routine.enabled,
      });
      setRoutines((prev) => prev.map((item) => (item.id === routine.id ? result.routine : item)));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "更新失败");
    }
  }, []);

  const remove = useCallback(async (routine: Routine) => {
    const ok = await confirm({
      title: `删除例程「${routine.name}」？`,
      description: "删除后不会再自动执行。",
      confirmLabel: "删除",
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteRoutine(routine.id);
      setRoutines((prev) => prev.filter((item) => item.id !== routine.id));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "删除失败");
    }
  }, [confirm]);

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } finally {
      router.replace("/login");
    }
  }, [router]);

  useEffect(() => {
    if (!editorOpen) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setEditorOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [editorOpen]);

  if (booting) {
    return (
      <div className="flex h-screen items-center justify-center">
        <Loader2 className="h-5 w-5 animate-spin text-[var(--sb-text-2)]" />
      </div>
    );
  }
  if (!user) {
    return (
      <div className="flex h-screen items-center justify-center">
        <Link href="/login" className="text-[var(--sb-text-2)]">
          去登录
        </Link>
      </div>
    );
  }

  return (
    <div className="flex h-screen w-full overflow-hidden">
      <Rail user={user} onLogout={logout} />
      <main className="flex min-w-0 flex-1 flex-col pb-16 md:pb-0">
        <header className="sb-page-header flex h-[61px] shrink-0 items-center justify-between gap-3 border-b border-[var(--sb-border)] px-6">
          <div className="flex items-center gap-3">
            <Link
              href="/"
              className="sb-icon-btn"
              aria-label={t("common.backToChat")}
            >
              <ArrowLeft className="h-4 w-4" />
            </Link>
            <div>
              <h1 className="text-[15px] font-semibold">{t("routines.title")}</h1>
            </div>
            <StatusPill tone="idle">{routines.length} {t("routines.unit")}</StatusPill>
          </div>
          <button
            type="button"
            onClick={openCreate}
            className="sb-btn" data-variant="primary" data-size="sm"
          >
            <Plus className="h-3.5 w-3.5" />
            {t("routines.new")}
          </button>
        </header>

        <div className="flex-1 overflow-y-auto px-6 py-6">
          <div className="mb-5 flex items-center gap-2 rounded-xl border border-[var(--sb-border)] bg-[var(--sb-pane)] px-4 py-3">
            <Clock className="h-4 w-4 text-[var(--sb-text-2)]" />
            <div className="text-[11.5px] text-muted">
              {t("routines.hint")}
            </div>
          </div>
          {error && (
            <div className="mb-5 rounded-xl border border-danger/25 bg-danger/10 px-4 py-3 text-[12px] text-danger">
              {error}
            </div>
          )}
          {teams.length === 0 && (
            <div className="mb-5 rounded-xl border border-amber/25 bg-amber/10 px-4 py-3 text-[12px] text-amber">
              {t("routines.noTeams")}{" "}
              <Link href="/teams" className="underline">
                {t("routines.teamsLink")}
              </Link>{" "}
              {t("routines.noTeamsSuffix")}
            </div>
          )}

          {routines.length === 0 ? (
            <div className="flex min-h-[420px] flex-col items-center justify-center rounded-2xl border border-dashed border-[var(--sb-border)] bg-[var(--sb-pane)]">
              <div className="flex h-14 w-14 items-center justify-center rounded-2xl border bg-[var(--sb-hover)]">
                <Clock className="h-6 w-6 text-[var(--sb-text-2)]" />
              </div>
              <h2 className="mt-4 text-[17px] font-medium text-[var(--sb-text)]">{t("routines.emptyTitle")}</h2>
              <p className="mt-1.5 max-w-md text-center text-[12px] text-muted">
                {t("routines.emptyDesc")}
              </p>
              <button
                type="button"
                onClick={openCreate}
                className="mt-5 inline-flex items-center gap-1.5 rounded-lg border border-mint/40 bg-mint px-3.5 py-2 text-[12px] font-semibold text-ink"
              >
                <Plus className="h-3.5 w-3.5" />
                {t("routines.createFirst")}
              </button>
            </div>
          ) : (
            <div className="grid gap-4 md:grid-cols-2 2xl:grid-cols-3">
              {routines.map((routine) => (
                <article key={routine.id} className="flex flex-col rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-5">
                  <div className="flex items-start gap-3">
                    <div className="flex h-10 w-10 items-center justify-center rounded-xl border bg-[var(--sb-hover)] text-[var(--sb-text-2)]">
                      <Clock className="h-4 w-4" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <h2 className="truncate text-[14px] font-medium text-[var(--sb-text)]">{routine.name}</h2>
                        <StatusPill tone={routine.enabled ? "live" : "idle"}>
                          {routine.enabled ? "active" : "paused"}
                        </StatusPill>
                      </div>
                      <div className="mt-1 flex items-center gap-1.5 font-mono text-[10.5px] text-[var(--sb-text-3)]">
                        <span>{teamName(routine.team_id)}</span>
                        <span className="text-muted/30">·</span>
                        <span>{routine.cron}</span>
                      </div>
                    </div>
                  </div>
                  <p className="mt-3 line-clamp-3 min-h-[52px] text-[12.5px] leading-[19px] text-[var(--sb-text-2)]">
                    {routine.prompt}
                  </p>
                  <div className="mt-3 grid grid-cols-2 gap-2 rounded-lg border border-[var(--sb-border)] bg-[var(--sb-hover)] px-2.5 py-2">
                    <div>
                      <div className="text-[11px] text-[var(--sb-text-3)]">{t("routines.next")}</div>
                      <div className="mt-0.5 text-[11.5px] text-[var(--sb-text)]">
                        {routine.next_run_at ? new Date(routine.next_run_at).toLocaleString("zh-CN", { hour12: false }) : "—"}
                      </div>
                    </div>
                    <div>
                      <div className="text-[11px] text-[var(--sb-text-3)]">{t("routines.last")}</div>
                      <div className="mt-0.5 text-[11.5px] text-[var(--sb-text)]">
                        {routine.last_run_at ? timeAgo(routine.last_run_at) : t("routines.never")}
                      </div>
                    </div>
                  </div>
                  <div className="mt-4 flex items-center justify-between border-t border-[var(--sb-border)] pt-3">
                    <div className="flex items-center gap-1.5">
                      <button
                        type="button"
                        onClick={() => toggle(routine)}
                        className={cn(
                          "inline-flex h-8 items-center gap-1 rounded-lg border px-3 text-[12px] transition",
                          routine.enabled
                            ? "border-[var(--sb-border)] text-muted hover:text-text"
                            : "border-[var(--sb-border-strong)] bg-[var(--sb-hover)] text-[var(--sb-text-2)]",
                        )}
                      >
                        {routine.enabled ? <Pause className="h-3 w-3" /> : <Play className="h-3 w-3" />}
                        {routine.enabled ? t("routines.pause") : t("routines.enable")}
                      </button>
                      <button
                        type="button"
                        onClick={() => openEdit(routine)}
                        className="h-8 rounded-lg border border-[var(--sb-border)] px-3 text-[12px] text-[var(--sb-text-2)] transition hover:text-[var(--sb-text)]"
                      >
                        {t("routines.edit")}
                      </button>
                    </div>
                    <button
                      type="button"
                      onClick={() => remove(routine)}
                      className="flex h-7 w-7 items-center justify-center rounded-md text-muted transition hover:bg-danger/10 hover:text-danger"
                      aria-label={t("routines.delete")}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                </article>
              ))}
            </div>
          )}
        </div>
      </main>

      {confirmElement}

      {editorOpen && (
        <div className="sb-overlay">
          <button
            type="button"
            className="absolute inset-0"
            onClick={() => setEditorOpen(false)}
            aria-label={t("common.close")}
          />
          <div className="sb-modal sb-dialog-md">
            <div className="flex items-start justify-between border-b border-[var(--sb-border)] px-5 py-4">
              <div>
                <div className="font-mono text-[9px] uppercase tracking-[0.22em] text-[var(--sb-text-2)]">
                  {editingId ? "edit routine" : "new routine"}
                </div>
                <h2 className="mt-1 text-[17px] font-medium text-[var(--sb-text)]">
                  {editingId ? t("routines.editTitle") : t("routines.newTitle")}
                </h2>
              </div>
              <button
                type="button"
                onClick={() => setEditorOpen(false)}
                className="sb-icon-btn" data-size="sm"
                aria-label={t("common.close")}
              >
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="grid gap-3 overflow-y-auto p-5">
              <Field label={t("routines.team")}>
                <select
                  className="field"
                  value={form.team_id}
                  disabled={Boolean(editingId)}
                  onChange={(event) => setForm((prev) => ({ ...prev, team_id: event.target.value }))}
                >
                  <option value="">{t("routines.selectTeam")}</option>
                  {teams.map((team) => (
                    <option key={team.id} value={team.id}>
                      {team.name} · {team.bots.length} bots
                    </option>
                  ))}
                </select>
              </Field>
              <Field label={t("routines.name")}>
                <input
                  className="field"
                  value={form.name}
                  onChange={(event) => setForm((prev) => ({ ...prev, name: event.target.value }))}
                  placeholder={t("routines.namePlaceholder")}
                />
              </Field>
              <Field label="Cron">
                <input
                  className="field font-mono text-[12px]"
                  value={form.cron}
                  onChange={(event) => setForm((prev) => ({ ...prev, cron: event.target.value }))}
                  placeholder="0 9 * * * 或 @every 1h"
                />
                <div className="mt-1 font-mono text-[9px] text-muted/50">
                  {t("routines.cronHint")}
                </div>
              </Field>
              <Field label={t("routines.promptLabel")}>
                <textarea
                  className="field min-h-[140px]"
                  value={form.prompt}
                  onChange={(event) => setForm((prev) => ({ ...prev, prompt: event.target.value }))}
                  placeholder={t("routines.promptPlaceholder")}
                />
              </Field>
              <label className="flex items-center gap-2 text-[12px] text-muted">
                <input
                  type="checkbox"
                  checked={form.enabled}
                  onChange={(event) => setForm((prev) => ({ ...prev, enabled: event.target.checked }))}
                  className="h-3.5 w-3.5 accent-[var(--color-mint)]"
                />
                {t("routines.enableNow")}
              </label>
            </div>
            <div className="flex items-center justify-between border-t border-[var(--sb-border)] px-5 py-3">
              {error ? <span className="text-[11px] text-danger">{error}</span> : <span />}
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => setEditorOpen(false)}
                  className="rounded-lg border border-[var(--sb-border)] px-3 py-2 text-[11.5px] text-muted hover:text-[var(--sb-text)]"
                >
                  {t("routines.cancel")}
                </button>
                <button
                  type="button"
                  onClick={save}
                  disabled={saving}
                  className="flex items-center gap-1.5 rounded-lg border border-mint/40 bg-mint px-3.5 py-2 text-[11.5px] font-semibold text-ink disabled:opacity-60"
                >
                  {saving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                  {editingId ? t("routines.save") : t("routines.create")}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="sb-field-label">
        {label}
      </label>
      {children}
    </div>
  );
}
