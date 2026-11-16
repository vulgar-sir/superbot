"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Clock, Loader2, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import { cn } from "@/lib/format";
import type { Routine } from "@/lib/types";
import { formatNext } from "./BotRoutineList";
import { useLanguage } from "./LanguageProvider";
import { Menu } from "./ui/Menu";
import { Modal } from "./ui/Modal";
import { useConfirm } from "./ui/Confirm";

const GROUP_ROUTINES_SECTION_ID = "sb-group-routines-section";

type Translate = (key: string, vars?: Record<string, string | number>) => string;

type RoutineForm = {
  name: string;
  cron: string;
  prompt: string;
  enabled: boolean;
};

// The team routine parser accepts 5-field cron plus descriptors such as
// @every/@hourly (internal/routines/service.go).
const PRESETS = [
  { key: "groupRoutines.preset.daily", value: "0 9 * * *" },
  { key: "groupRoutines.preset.weekdays", value: "0 9 * * 1-5" },
  { key: "groupRoutines.preset.hourly", value: "@every 1h" },
  { key: "groupRoutines.preset.halfHour", value: "@every 30m" },
] as const;

const errorClass =
  "mt-2 whitespace-pre-wrap break-words rounded-lg bg-[var(--sb-danger-soft)] px-2.5 py-1.5 text-[12px] leading-[18px] text-[var(--sb-danger)]";

function emptyForm(): RoutineForm {
  return { name: "", cron: PRESETS[0].value, prompt: "", enabled: true };
}

function describeSchedule(expr: string, t: Translate): string {
  let rest = expr.trim().replace(/\s+/g, " ");
  let zone = "";
  const prefixed = rest.match(/^(?:CRON_TZ|TZ)=(\S+) (.+)$/);
  if (prefixed) {
    zone = prefixed[1];
    rest = prefixed[2];
  }
  const label = describeBareSchedule(rest, t);
  return zone ? `${label} · ${zone}` : label;
}

function describeBareSchedule(expr: string, t: Translate): string {
  const preset = PRESETS.find((item) => item.value === expr);
  if (preset) return t(preset.key);
  if (expr === "@hourly" || expr === "0 * * * *") return t("groupRoutines.preset.hourly");
  if (expr === "@daily" || expr === "@midnight") return t("groupRoutines.dailyAt", { time: "0:00" });
  if (expr.startsWith("@every ")) return t("groupRoutines.every", { interval: expr.slice(7) });
  const fields = expr.split(" ");
  if (
    fields.length === 5 &&
    /^\d{1,2}$/.test(fields[0]) &&
    /^\d{1,2}$/.test(fields[1]) &&
    fields[2] === "*" &&
    fields[3] === "*"
  ) {
    const time = `${Number(fields[1])}:${fields[0].padStart(2, "0")}`;
    if (fields[4] === "*") return t("groupRoutines.dailyAt", { time });
    if (fields[4] === "1-5") return t("groupRoutines.weekdaysAt", { time });
  }
  return expr;
}

function errorText(err: unknown, fallback: string, t: Translate): string {
  if (!(err instanceof ApiError)) return fallback;
  if (/invalid cron expression|never runs/i.test(err.message)) return t("groupRoutines.invalidCron");
  return err.message || fallback;
}

/**
 * Scheduled routines of one group, shown in the group's info panel. A routine
 * posts its prompt into the group conversation when it is due.
 */
export function GroupRoutineList({ teamId, teamName }: { teamId: string; teamName: string }) {
  const { t } = useLanguage();
  const { confirm, confirmElement } = useConfirm();
  const [routines, setRoutines] = useState<Routine[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [menu, setMenu] = useState<{ routine: Routine; rect: DOMRect } | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<Routine | null>(null);
  const [form, setForm] = useState<RoutineForm>(emptyForm);
  const [formError, setFormError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const loadSeq = useRef(0);

  const load = useCallback(async () => {
    const seq = ++loadSeq.current;
    setLoading(true);
    setLoadError(null);
    try {
      const result = await api.listRoutines();
      if (seq !== loadSeq.current) return;
      setRoutines(result.routines.filter((routine) => routine.team_id === teamId));
    } catch (err) {
      if (seq !== loadSeq.current) return;
      // Empty string means "no server message"; the render falls back to the
      // dictionary text so a language switch does not need a reload.
      setLoadError(err instanceof ApiError ? err.message : "");
    } finally {
      if (seq === loadSeq.current) setLoading(false);
    }
  }, [teamId]);

  useEffect(() => {
    setRoutines([]);
    setActionError(null);
    setMenu(null);
    void load();
  }, [load]);

  const replace = useCallback((routine: Routine) => {
    setRoutines((prev) => prev.map((item) => (item.id === routine.id ? routine : item)));
  }, []);

  const toggle = useCallback(
    async (routine: Routine) => {
      setBusyId(routine.id);
      setActionError(null);
      try {
        // The update endpoint replaces every field, so send the full routine.
        const result = await api.updateRoutine(routine.id, {
          name: routine.name,
          cron: routine.cron,
          prompt: routine.prompt,
          enabled: !routine.enabled,
        });
        replace(result.routine);
      } catch (err) {
        setActionError(errorText(err, t("groupRoutines.updateFailed"), t));
      } finally {
        setBusyId(null);
      }
    },
    [replace, t],
  );

  const remove = useCallback(
    async (routine: Routine) => {
      const ok = await confirm({
        title: t("groupRoutines.confirmDeleteTitle", { name: routine.name }),
        description: t("groupRoutines.confirmDeleteBody"),
        confirmLabel: t("groupRoutines.delete"),
        cancelLabel: t("groupRoutines.cancel"),
        danger: true,
      });
      if (!ok) return;
      setActionError(null);
      try {
        await api.deleteRoutine(routine.id);
        setRoutines((prev) => prev.filter((item) => item.id !== routine.id));
      } catch (err) {
        setActionError(errorText(err, t("groupRoutines.deleteFailed"), t));
      }
    },
    [confirm, t],
  );

  const openCreate = useCallback(() => {
    setEditing(null);
    setForm(emptyForm());
    setFormError(null);
    setEditorOpen(true);
  }, []);

  const openEdit = useCallback((routine: Routine) => {
    setEditing(routine);
    setForm({
      name: routine.name,
      cron: routine.cron,
      prompt: routine.prompt,
      enabled: routine.enabled,
    });
    setFormError(null);
    setEditorOpen(true);
  }, []);

  const closeEditor = useCallback(() => {
    setEditorOpen(false);
    setEditing(null);
  }, []);

  const save = useCallback(async () => {
    const name = form.name.trim();
    const cron = form.cron.trim();
    const prompt = form.prompt.trim();
    if (!name || !cron || !prompt) {
      setFormError(t("groupRoutines.required"));
      return;
    }
    setSaving(true);
    setFormError(null);
    try {
      if (editing) {
        const result = await api.updateRoutine(editing.id, { name, cron, prompt, enabled: form.enabled });
        replace(result.routine);
      } else {
        const result = await api.createRoutine({
          team_id: teamId,
          name,
          cron,
          prompt,
          enabled: form.enabled,
        });
        setRoutines((prev) => [result.routine, ...prev]);
      }
      closeEditor();
    } catch (err) {
      setFormError(errorText(err, t("groupRoutines.saveFailed"), t));
    } finally {
      setSaving(false);
    }
  }, [closeEditor, editing, form, replace, t, teamId]);

  let body: React.ReactNode;
  if (loading && routines.length === 0) {
    body = (
      <div className="sb-card flex items-center justify-center px-3.5 py-5 text-[var(--sb-text-3)]">
        <Loader2 className="sb-spin h-4 w-4" aria-label={t("groupRoutines.loading")} />
      </div>
    );
  } else if (loadError !== null) {
    body = (
      <div className="sb-card flex items-center justify-center gap-2 px-3.5 py-4 text-[12px]">
        <span className="min-w-0 break-words text-[var(--sb-danger)]" role="alert">
          {loadError || t("groupRoutines.loadFailed")}
        </span>
        <button
          type="button"
          className="sb-btn shrink-0"
          data-variant="secondary"
          data-size="sm"
          onClick={() => void load()}
        >
          {t("groupRoutines.retry")}
        </button>
      </div>
    );
  } else if (routines.length === 0) {
    body = (
      <div className="sb-card px-3.5 py-3">
        <p className="text-[12px] leading-[18px] text-[var(--sb-text-2)]">{t("groupRoutines.empty")}</p>
        <button
          type="button"
          className="sb-btn mt-2.5"
          data-variant="secondary"
          data-size="sm"
          onClick={openCreate}
        >
          <Plus className="h-3 w-3" />
          {t("groupRoutines.new")}
        </button>
      </div>
    );
  } else {
    body = (
      <ul className="sb-card overflow-hidden">
        {routines.map((routine, index) => (
          <li
            key={routine.id}
            className={cn("sb-card-row", index > 0 && "border-t border-[var(--sb-border)]")}
            data-routine-id={routine.id}
          >
            <Clock className="h-4 w-4 shrink-0 text-[var(--sb-text-2)]" aria-hidden />
            <div className="min-w-0 flex-1">
              <div className="truncate text-[14px] leading-[20px]" title={routine.prompt}>
                {routine.name}
              </div>
              <div className="mt-0.5 flex flex-wrap items-center gap-x-2 text-[12px] leading-[16px] text-[var(--sb-text-2)]">
                <span className="break-all">{describeSchedule(routine.cron, t)}</span>
                <span>
                  {!routine.enabled
                    ? t("groupRoutines.disabled")
                    : routine.next_run_at
                      ? t("groupRoutines.next", { time: formatNext(routine.next_run_at) })
                      : t("groupRoutines.pending")}
                </span>
                {routine.last_run_at && (
                  <span>{t("groupRoutines.last", { time: formatNext(routine.last_run_at) })}</span>
                )}
              </div>
            </div>
            <button
              type="button"
              role="switch"
              className="sb-switch"
              data-on={routine.enabled ? "true" : undefined}
              aria-checked={routine.enabled}
              aria-label={t("groupRoutines.toggleAria", { name: routine.name })}
              disabled={busyId === routine.id}
              onClick={() => void toggle(routine)}
            />
            <button
              type="button"
              className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-[var(--sb-text-3)] transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-text)]"
              onClick={(event) => setMenu({ routine, rect: event.currentTarget.getBoundingClientRect() })}
              aria-haspopup="menu"
              aria-label={t("groupRoutines.more", { name: routine.name })}
            >
              <MoreHorizontal className="h-4 w-4" />
            </button>
          </li>
        ))}
      </ul>
    );
  }

  return (
    <section
      id={GROUP_ROUTINES_SECTION_ID}
      aria-label={t("groupRoutines.title")}
      data-section="group-routines"
    >
      <div className="sb-info-label flex items-center justify-between gap-2">
        <span>{t("groupRoutines.title")}</span>
        <button
          type="button"
          onClick={openCreate}
          className="flex h-5 w-5 items-center justify-center rounded text-[var(--sb-text-4)] transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-text)]"
          aria-label={t("groupRoutines.new")}
          title={t("groupRoutines.new")}
        >
          <Plus className="h-3.5 w-3.5" />
        </button>
      </div>
      {body}
      {actionError && (
        <div className={errorClass} role="alert">
          {actionError}
        </div>
      )}

      <Menu
        open={menu !== null}
        anchor={menu ? { rect: menu.rect } : null}
        items={
          menu
            ? [
                {
                  key: "edit",
                  label: t("groupRoutines.edit"),
                  icon: <Pencil className="h-3.5 w-3.5" />,
                  onSelect: () => openEdit(menu.routine),
                },
                { key: "sep", separator: true },
                {
                  key: "delete",
                  label: t("groupRoutines.delete"),
                  icon: <Trash2 className="h-3.5 w-3.5" />,
                  danger: true,
                  onSelect: () => void remove(menu.routine),
                },
              ]
            : []
        }
        onClose={() => setMenu(null)}
        align="right"
      />

      <Modal
        open={editorOpen}
        onClose={closeEditor}
        title={editing ? t("groupRoutines.editTitle") : t("groupRoutines.new")}
        subtitle={t("groupRoutines.subtitle", { team: teamName })}
        size="md"
        footer={
          <>
            <button type="button" className="sb-btn" data-variant="secondary" onClick={closeEditor}>
              {t("groupRoutines.cancel")}
            </button>
            <button
              type="button"
              className="sb-btn"
              data-variant="primary"
              onClick={() => void save()}
              disabled={saving}
            >
              {saving && <Loader2 className="sb-spin h-3.5 w-3.5" />}
              {editing ? t("groupRoutines.save") : t("groupRoutines.create")}
            </button>
          </>
        }
      >
        <div className="space-y-4">
          <label className="block">
            <span className="sb-field-label">{t("groupRoutines.name")}</span>
            <input
              className="sb-field"
              value={form.name}
              onChange={(event) => setForm((prev) => ({ ...prev, name: event.target.value }))}
              placeholder={t("groupRoutines.namePlaceholder")}
            />
          </label>
          <div>
            <label className="block">
              <span className="sb-field-label">{t("groupRoutines.schedule")}</span>
              <input
                className="sb-field font-[var(--sb-mono)]"
                value={form.cron}
                onChange={(event) => setForm((prev) => ({ ...prev, cron: event.target.value }))}
                placeholder={t("groupRoutines.schedulePlaceholder")}
              />
              <span className="mt-1 block text-[11px] leading-[16px] text-[var(--sb-text-3)]">
                {t("groupRoutines.scheduleHint")}
              </span>
            </label>
            <div className="mt-2 flex flex-wrap gap-1.5" role="group" aria-label={t("groupRoutines.presets")}>
              {PRESETS.map((preset) => (
                <button
                  key={preset.value}
                  type="button"
                  aria-pressed={form.cron.trim() === preset.value}
                  onClick={() => setForm((prev) => ({ ...prev, cron: preset.value }))}
                  className="rounded-full bg-[var(--sb-hover)] px-2.5 py-1 text-[12px] text-[var(--sb-text-2)] transition hover:bg-[var(--sb-active)] hover:text-[var(--sb-text)] aria-pressed:bg-[var(--sb-active)] aria-pressed:text-[var(--sb-text)]"
                >
                  {t(preset.key)}
                </button>
              ))}
            </div>
          </div>
          <label className="block">
            <span className="sb-field-label">{t("groupRoutines.prompt")}</span>
            <textarea
              className="sb-field"
              value={form.prompt}
              onChange={(event) => setForm((prev) => ({ ...prev, prompt: event.target.value }))}
              placeholder={t("groupRoutines.promptPlaceholder")}
            />
          </label>
          <div className="flex items-center justify-between gap-3 rounded-xl border border-[var(--sb-border)] p-3">
            <div>
              <div className="text-[13px] leading-[18px]">{t("groupRoutines.enabled")}</div>
              <div className="text-[11px] leading-[16px] text-[var(--sb-text-3)]">
                {t("groupRoutines.enabledHint")}
              </div>
            </div>
            <button
              type="button"
              role="switch"
              className="sb-switch"
              data-on={form.enabled ? "true" : undefined}
              aria-checked={form.enabled}
              aria-label={t("groupRoutines.enabled")}
              onClick={() => setForm((prev) => ({ ...prev, enabled: !prev.enabled }))}
            />
          </div>
          {formError && (
            <div className={errorClass} role="alert">
              {formError}
            </div>
          )}
        </div>
      </Modal>
      {confirmElement}
    </section>
  );
}
