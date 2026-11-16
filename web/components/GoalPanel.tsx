"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  ArrowDown,
  ArrowRight,
  ArrowUp,
  Check,
  ChevronRight,
  Circle,
  Flag,
  Loader2,
  Plus,
  Trash2,
} from "lucide-react";

import { api, ApiError } from "@/lib/api";
import { cn } from "@/lib/format";
import type { Bot, Goal, GoalAction, GoalEvent, GoalResponse, GoalStage, GoalTask } from "@/lib/types";
import { BotAvatar } from "./BotAvatar";
import { useLanguage } from "./LanguageProvider";
import { StatusPill } from "./StatusPill";
import { useConfirm } from "./ui/Confirm";
import { Modal } from "./ui/Modal";

const MAX_STAGES = 8;
const ACTIVITY_LIMIT = 15;
const NOTICE_MS = 6000;
const LONG_TEXT = 140;

export const GOAL_SECTION_ID = "sb-goal-section";

type GoalErrorState = { scope: string; message: string };

export type GoalController = {
  /** null until the first load for this conversation returned. */
  data: GoalResponse | null;
  loading: boolean;
  /** null = no error; "" = failed without a server message. */
  loadError: string | null;
  /** Scope of the mutation in flight, if any. */
  busy: string | null;
  notice: string | null;
  error: GoalErrorState | null;
  act: (action: GoalAction, scope?: string) => Promise<boolean>;
  applyEvent: (goal: unknown) => void;
  reload: () => Promise<void>;
  clearError: () => void;
};

// Go encodes empty slices as null; give the UI arrays to map over.
function normalizeGoal(goal: Goal | null | undefined): Goal | null {
  if (!goal || typeof goal !== "object" || typeof goal.status !== "string") return null;
  return {
    ...goal,
    constraints: goal.constraints ?? [],
    events: goal.events ?? [],
    stages: (goal.stages ?? []).map((stage) => ({ ...stage, tasks: stage.tasks ?? [] })),
  };
}

function normalizeResponse(response: GoalResponse): GoalResponse {
  return { ...response, goal: normalizeGoal(response.goal), members: response.members ?? [] };
}

/**
 * Loads the group goal of a team conversation and applies the user's actions.
 * The page owns one instance so the header chip, the info panel and the SSE
 * handler all see the same state.
 */
export function useGroupGoal(conversationId: string | null, enabled: boolean): GoalController {
  const [data, setData] = useState<GoalResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<GoalErrorState | null>(null);
  const currentRef = useRef<string | null>(null);
  const busyRef = useRef(false);
  const noticeTimerRef = useRef<number | null>(null);

  const reload = useCallback(async () => {
    const id = currentRef.current;
    if (!id) return;
    setLoading(true);
    try {
      const result = await api.getGoal(id);
      if (currentRef.current !== id) return;
      setData(normalizeResponse(result));
      setLoadError(null);
    } catch (err) {
      if (currentRef.current !== id) return;
      setLoadError(err instanceof ApiError ? err.message : "");
    } finally {
      if (currentRef.current === id) setLoading(false);
    }
  }, []);

  useEffect(() => {
    currentRef.current = enabled ? conversationId : null;
    busyRef.current = false;
    setData(null);
    setLoading(false);
    setLoadError(null);
    setBusy(null);
    setNotice(null);
    setError(null);
    if (enabled && conversationId) void reload();
  }, [conversationId, enabled, reload]);

  useEffect(() => {
    return () => {
      if (noticeTimerRef.current !== null) window.clearTimeout(noticeTimerRef.current);
    };
  }, []);

  const act = useCallback(async (action: GoalAction, scope = "header") => {
    const id = currentRef.current;
    if (!id || busyRef.current) return false;
    busyRef.current = true;
    setBusy(scope);
    setError(null);
    try {
      const result = await api.goalAction(id, action);
      if (currentRef.current !== id) return false;
      setData(normalizeResponse(result));
      if (noticeTimerRef.current !== null) window.clearTimeout(noticeTimerRef.current);
      setNotice(result.message?.trim() || null);
      noticeTimerRef.current = window.setTimeout(() => setNotice(null), NOTICE_MS);
      return true;
    } catch (err) {
      if (currentRef.current === id) {
        setError({ scope, message: err instanceof ApiError ? err.message : "" });
      }
      return false;
    } finally {
      if (currentRef.current === id) {
        busyRef.current = false;
        setBusy(null);
      }
    }
  }, []);

  // `goal.updated` carries the whole goal; the roster stays from the last GET.
  const applyEvent = useCallback((raw: unknown) => {
    if (!currentRef.current) return;
    const goal = normalizeGoal(raw as Goal);
    if (!goal) return;
    setData((prev) => (prev ? { ...prev, goal } : { goal, members: [] }));
  }, []);

  const clearError = useCallback(() => setError(null), []);

  return { data, loading, loadError, busy, notice, error, act, applyEvent, reload, clearError };
}

function passedCount(goal: Goal): number {
  return goal.stages.filter((stage) => stage.status === "passed").length;
}

/** Compact header entry: where the goal stands, and a way to open it. */
export function GoalChip({ goal, onClick }: { goal: Goal | null | undefined; onClick: () => void }) {
  const { t } = useLanguage();
  if (!goal || !["drafting", "active", "paused"].includes(goal.status)) return null;
  const total = goal.stages.length;
  let detail: string;
  if (goal.status === "drafting") detail = t("goal.status.drafting");
  else if (goal.status === "paused") detail = t("goal.status.paused");
  else if (goal.current >= total) detail = t("goal.chip.allPassed");
  else detail = t("goal.chip.stage", { n: goal.current + 1, m: total });
  const label = `${t("goal.title")} · ${detail}`;
  return (
    <button
      type="button"
      onClick={onClick}
      className="sb-btn min-w-0 shrink"
      data-variant="outline"
      data-size="sm"
      data-goal-chip={goal.status}
      aria-label={label}
      title={label}
    >
      <Flag className="h-3.5 w-3.5 shrink-0" aria-hidden />
      <span className="truncate">
        <span className="hidden md:inline">{t("goal.title")} · </span>
        {detail}
      </span>
    </button>
  );
}

function goalTone(status: Goal["status"]): "live" | "idle" | "busy" {
  if (status === "active" || status === "complete") return "live";
  if (status === "paused") return "busy";
  return "idle";
}

function taskTone(status: GoalTask["status"]): "live" | "idle" | "busy" {
  if (status === "done") return "live";
  if (status === "doing") return "busy";
  return "idle";
}

function InlineError({ ctl, scope }: { ctl: GoalController; scope: string }) {
  const { t } = useLanguage();
  if (!ctl.error || ctl.error.scope !== scope) return null;
  return (
    <div
      className="mt-2 whitespace-pre-wrap break-words rounded-lg bg-[var(--sb-danger-soft)] px-2.5 py-1.5 text-[12px] leading-[18px] text-[var(--sb-danger)]"
      role="alert"
    >
      {ctl.error.message || t("goal.actionFailed")}
    </div>
  );
}

function LongText({ text, className }: { text: string; className?: string }) {
  const { t } = useLanguage();
  const [open, setOpen] = useState(false);
  const long = text.length > LONG_TEXT || text.split("\n").length > 3;
  return (
    <div className={className}>
      <div className={cn("whitespace-pre-wrap break-words", long && !open && "line-clamp-3")}>{text}</div>
      {long && (
        <button
          type="button"
          onClick={() => setOpen((prev) => !prev)}
          aria-expanded={open}
          className="mt-0.5 text-[11px] text-[var(--sb-text-3)] transition hover:text-[var(--sb-text)]"
        >
          {open ? t("goal.collapse") : t("goal.expand")}
        </button>
      )}
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="mt-2.5 first:mt-0">
      <div className="text-[11px] leading-[16px] text-[var(--sb-text-3)]">{label}</div>
      <div className="mt-0.5 text-[12px] leading-[18px] text-[var(--sb-text)]">{children}</div>
    </div>
  );
}

function GoalBrief({ goal }: { goal: Goal }) {
  const { t } = useLanguage();
  return (
    <>
      <Field label={t("goal.objective")}>
        <LongText text={goal.objective} />
      </Field>
      <Field label={t("goal.acceptance")}>
        <LongText text={goal.acceptance} />
      </Field>
      {goal.constraints.length > 0 && (
        <Field label={t("goal.constraints")}>
          <ul className="list-disc space-y-0.5 pl-4">
            {goal.constraints.map((item, index) => (
              <li key={index} className="break-words">
                {item}
              </li>
            ))}
          </ul>
        </Field>
      )}
    </>
  );
}

function StageMark({ state }: { state: "passed" | "current" | "pending" }) {
  const { t } = useLanguage();
  const label = t(`goal.stage.${state}`);
  return (
    <span
      className={cn(
        "mt-px flex h-[18px] w-[18px] shrink-0 items-center justify-center rounded-full",
        state === "passed" && "bg-[var(--sb-hover)] text-[var(--sb-success)]",
        state === "current" && "bg-[var(--sb-primary)] text-[var(--sb-primary-contrast)]",
        state === "pending" && "text-[var(--sb-text-4)]",
      )}
      title={label}
    >
      {state === "passed" ? (
        <Check className="h-3 w-3" strokeWidth={2.6} aria-hidden />
      ) : state === "current" ? (
        <ArrowRight className="h-3 w-3" strokeWidth={2.6} aria-hidden />
      ) : (
        <Circle className="h-3 w-3" aria-hidden />
      )}
      <span className="sr-only">{label}</span>
    </span>
  );
}

function TaskRow({ task, bots }: { task: GoalTask; bots: Bot[] }) {
  const { t } = useLanguage();
  const bot = bots.find((item) => item.id === task.bot_id);
  return (
    <li className="rounded-lg bg-[var(--sb-bg)] px-2.5 py-2">
      <div className="flex items-center gap-1.5">
        <BotAvatar
          name={bot?.name ?? task.bot_name}
          id={task.bot_id || task.bot_name}
          size={18}
          shape={bot?.avatar_shape}
          color={bot?.avatar_color}
          avatar={bot?.avatar}
        />
        <span className="min-w-0 flex-1 truncate text-[12px] font-medium leading-[18px]">
          {bot?.name ?? task.bot_name}
        </span>
        <StatusPill tone={taskTone(task.status)} className="shrink-0">
          {t(`goal.task.${task.status}`)}
        </StatusPill>
      </div>
      <div className="mt-1 whitespace-pre-wrap break-words text-[12px] leading-[18px] text-[var(--sb-text)]">
        {task.task}
      </div>
      {task.note && (
        <div className="mt-1 whitespace-pre-wrap break-words border-l-2 border-[var(--sb-border-strong)] pl-2 text-[12px] leading-[18px] text-[var(--sb-text-2)]">
          <span className="text-[var(--sb-text-3)]">{t("goal.task.note")}</span> {task.note}
        </div>
      )}
    </li>
  );
}

type StageForm = "assign" | "pass" | "rework";

function CurrentStageActions({ ctl }: { ctl: GoalController }) {
  const { t } = useLanguage();
  const members = ctl.data?.members ?? [];
  const [form, setForm] = useState<StageForm | null>(null);
  const [member, setMember] = useState("");
  const [text, setText] = useState("");
  const working = ctl.busy !== null;
  const memberName = member || members[0]?.name || "";

  const open = (next: StageForm) => {
    ctl.clearError();
    setText("");
    setForm((prev) => (prev === next ? null : next));
  };

  const submit = async () => {
    const value = text.trim();
    if (!form || !value || working) return;
    let action: GoalAction;
    if (form === "assign") {
      if (!memberName) return;
      action = { action: "assign", bot: memberName, task: value };
    } else if (form === "pass") {
      action = { action: "pass", evidence: value };
    } else {
      action = { action: "rework", feedback: value };
    }
    const ok = await ctl.act(action, form);
    if (ok) {
      setText("");
      setForm(null);
    }
  };

  const fieldID = form ? `sb-goal-${form}-text` : undefined;

  return (
    <div className="mt-2.5">
      <div className="flex flex-wrap gap-1.5">
        {(["assign", "pass", "rework"] as const).map((key) => (
          <button
            key={key}
            type="button"
            className="sb-btn"
            data-variant={form === key ? "primary" : "secondary"}
            data-size="sm"
            aria-expanded={form === key}
            disabled={working}
            onClick={() => open(key)}
          >
            {t(`goal.action.${key}`)}
          </button>
        ))}
      </div>
      {form && (
        <form
          className="mt-2"
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          {form === "assign" && (
            <>
              <label className="sb-field-label" htmlFor="sb-goal-assign-member">
                {t("goal.assign.member")}
              </label>
              <select
                id="sb-goal-assign-member"
                className="sb-field mb-2 !h-9 !text-[13px]"
                value={memberName}
                onChange={(event) => setMember(event.target.value)}
              >
                {members.map((item) => (
                  <option key={item.id} value={item.name}>
                    {item.name}
                  </option>
                ))}
              </select>
            </>
          )}
          <label className="sb-field-label" htmlFor={fieldID}>
            {t(`goal.${form}.label`)}
          </label>
          <textarea
            id={fieldID}
            autoFocus
            value={text}
            onChange={(event) => setText(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Escape") {
                event.stopPropagation();
                setForm(null);
              }
            }}
            className="sb-field !min-h-[72px] !text-[13px] !leading-[20px]"
            placeholder={t(`goal.${form}.placeholder`)}
            required
          />
          <InlineError ctl={ctl} scope={form} />
          <div className="mt-2 flex justify-end gap-2">
            <button
              type="button"
              className="sb-btn"
              data-variant="ghost"
              data-size="sm"
              onClick={() => setForm(null)}
            >
              {t("goal.cancel")}
            </button>
            <button
              type="submit"
              className="sb-btn"
              data-variant="primary"
              data-size="sm"
              disabled={working || !text.trim() || (form === "assign" && !memberName)}
            >
              {ctl.busy === form && <Loader2 className="sb-spin h-3 w-3" />}
              {t(`goal.${form}.submit`)}
            </button>
          </div>
        </form>
      )}
    </div>
  );
}

function StageItem({
  stage,
  index,
  state,
  bots,
  actions,
}: {
  stage: GoalStage;
  index: number;
  state: "passed" | "current" | "pending";
  bots: Bot[];
  actions?: React.ReactNode;
}) {
  const { t } = useLanguage();
  return (
    <li
      className={cn("flex gap-2.5 px-3.5 py-3", index > 0 && "border-t border-[var(--sb-border)]")}
      data-stage-state={state}
      aria-current={state === "current" ? "step" : undefined}
    >
      <StageMark state={state} />
      <div className="min-w-0 flex-1">
        <div
          className={cn(
            "break-words text-[13px] font-medium leading-[20px]",
            state === "pending" && "text-[var(--sb-text-2)]",
          )}
        >
          {index + 1}. {stage.title}
        </div>
        <LongText text={stage.acceptance} className="text-[12px] leading-[18px] text-[var(--sb-text-2)]" />
        {stage.tasks.length > 0 && (
          <ul className="mt-2 space-y-1.5">
            {stage.tasks.map((task, taskIndex) => (
              <TaskRow key={taskIndex} task={task} bots={bots} />
            ))}
          </ul>
        )}
        {stage.reworks > 0 && (
          <div className="mt-2 rounded-lg bg-[var(--sb-warn-soft)] px-2.5 py-1.5 text-[12px] leading-[18px] text-[var(--sb-warn)]">
            <div className="font-medium">{t("goal.stage.reworks", { n: stage.reworks })}</div>
            {stage.feedback && <div className="whitespace-pre-wrap break-words">{stage.feedback}</div>}
          </div>
        )}
        {state === "passed" && stage.evidence && (
          <div className="mt-2 text-[12px] leading-[18px] text-[var(--sb-text-2)]">
            <span className="text-[var(--sb-text-3)]">{t("goal.stage.evidence")}</span>
            <LongText text={stage.evidence} />
          </div>
        )}
        {actions}
      </div>
    </li>
  );
}

function CompleteForm({ ctl }: { ctl: GoalController }) {
  const { t } = useLanguage();
  const [evidence, setEvidence] = useState("");
  const working = ctl.busy !== null;
  return (
    <form
      className="border-t border-[var(--sb-border)] px-3.5 py-3"
      onSubmit={(event) => {
        event.preventDefault();
        const value = evidence.trim();
        if (!value || working) return;
        void ctl.act({ action: "complete", evidence: value }, "complete");
      }}
    >
      <div className="text-[12px] leading-[18px] text-[var(--sb-text-2)]">{t("goal.complete.hint")}</div>
      <label className="sb-field-label mt-2" htmlFor="sb-goal-complete-text">
        {t("goal.complete.label")}
      </label>
      <textarea
        id="sb-goal-complete-text"
        value={evidence}
        onChange={(event) => setEvidence(event.target.value)}
        className="sb-field !min-h-[72px] !text-[13px] !leading-[20px]"
        placeholder={t("goal.complete.placeholder")}
        required
      />
      <InlineError ctl={ctl} scope="complete" />
      <div className="mt-2 flex justify-end">
        <button
          type="submit"
          className="sb-btn"
          data-variant="primary"
          data-size="sm"
          disabled={working || !evidence.trim()}
        >
          {ctl.busy === "complete" && <Loader2 className="sb-spin h-3 w-3" />}
          {t("goal.action.complete")}
        </button>
      </div>
    </form>
  );
}

function eventTime(iso: string, language: "zh" | "en"): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleString(language === "en" ? "en-US" : "zh-CN", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function ActivityList({ events }: { events: GoalEvent[] }) {
  const { t, language } = useLanguage();
  const [open, setOpen] = useState(false);
  if (events.length === 0) return null;
  const recent = events.slice(-ACTIVITY_LIMIT).reverse();
  const actionLabel = (action: string) => {
    const key = `goal.event.${action}`;
    const label = t(key);
    return label === key ? action : label;
  };
  const detailText = (event: GoalEvent) => {
    if (event.action === "report" && (event.detail === "doing" || event.detail === "done")) {
      return t(`goal.task.${event.detail}`);
    }
    return event.detail;
  };
  return (
    <div className="mt-2">
      <button
        type="button"
        onClick={() => setOpen((prev) => !prev)}
        aria-expanded={open}
        aria-controls="sb-goal-activity"
        className="flex w-full items-center gap-1 rounded-lg px-1 py-1.5 text-left text-[12px] text-[var(--sb-text-2)] transition hover:text-[var(--sb-text)]"
      >
        <ChevronRight className={cn("h-3.5 w-3.5 transition", open && "rotate-90")} aria-hidden />
        <span>{t("goal.activity")}</span>
        <span className="text-[11px] tabular-nums text-[var(--sb-text-4)]">{recent.length}</span>
      </button>
      {open && (
        <ol id="sb-goal-activity" className="sb-card divide-y divide-[var(--sb-border)] overflow-hidden">
          {recent.map((event, index) => {
            const detail = detailText(event);
            return (
              <li key={`${event.at}-${index}`} className="px-3.5 py-2 text-[12px] leading-[18px]">
                <div className="flex items-baseline gap-1.5">
                  <span className="min-w-0 truncate font-medium text-[var(--sb-text)]">
                    {event.actor === "用户" ? t("goal.actorUser") : event.actor}
                  </span>
                  <span className="shrink-0 text-[var(--sb-text-2)]">{actionLabel(event.action)}</span>
                  <span className="ml-auto shrink-0 text-[11px] tabular-nums text-[var(--sb-text-4)]">
                    {eventTime(event.at, language)}
                  </span>
                </div>
                {detail && <div className="break-words text-[var(--sb-text-2)]">{detail}</div>}
              </li>
            );
          })}
        </ol>
      )}
    </div>
  );
}

type StageDraft = { key: number; title: string; acceptance: string };

function GoalFormDialog({
  mode,
  initial,
  ctl,
  onClose,
}: {
  mode: "draft" | "revise";
  initial: Goal | null;
  ctl: GoalController;
  onClose: () => void;
}) {
  const { t } = useLanguage();
  const keyRef = useRef(0);
  const nextKey = () => {
    keyRef.current += 1;
    return keyRef.current;
  };
  const [objective, setObjective] = useState(initial?.objective ?? "");
  const [acceptance, setAcceptance] = useState(initial?.acceptance ?? "");
  const [constraints, setConstraints] = useState((initial?.constraints ?? []).join("\n"));
  const [stages, setStages] = useState<StageDraft[]>(() => {
    const source = initial?.stages.length ? initial.stages : [{ title: "", acceptance: "" }];
    return source.map((stage) => ({ key: nextKey(), title: stage.title, acceptance: stage.acceptance }));
  });
  const working = ctl.busy !== null;
  const valid =
    objective.trim().length > 0 &&
    acceptance.trim().length > 0 &&
    stages.length >= 1 &&
    stages.every((stage) => stage.title.trim() && stage.acceptance.trim());

  const updateStage = (key: number, patch: Partial<StageDraft>) =>
    setStages((prev) => prev.map((stage) => (stage.key === key ? { ...stage, ...patch } : stage)));
  const moveStage = (index: number, delta: number) =>
    setStages((prev) => {
      const target = index + delta;
      if (target < 0 || target >= prev.length) return prev;
      const next = [...prev];
      [next[index], next[target]] = [next[target], next[index]];
      return next;
    });

  const submit = async () => {
    if (!valid || working) return;
    const ok = await ctl.act(
      {
        action: mode,
        objective: objective.trim(),
        acceptance: acceptance.trim(),
        constraints: constraints
          .split("\n")
          .map((line) => line.trim())
          .filter(Boolean),
        stages: stages.map((stage) => ({ title: stage.title.trim(), acceptance: stage.acceptance.trim() })),
      },
      "form",
    );
    if (ok) onClose();
  };

  return (
    <Modal
      open
      onClose={onClose}
      title={mode === "draft" ? t("goal.form.titleDraft") : t("goal.form.titleRevise")}
      subtitle={t("goal.form.subtitle")}
      size="md"
      closeOnBackdrop={false}
      footer={
        <>
          <button type="button" className="sb-btn" data-variant="secondary" onClick={onClose}>
            {t("goal.cancel")}
          </button>
          <button
            type="submit"
            form="sb-goal-form"
            className="sb-btn"
            data-variant="primary"
            disabled={!valid || working}
          >
            {ctl.busy === "form" && <Loader2 className="sb-spin h-3.5 w-3.5" />}
            {mode === "draft" ? t("goal.form.submitDraft") : t("goal.form.submitRevise")}
          </button>
        </>
      }
    >
      <form
        id="sb-goal-form"
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
      >
        <label className="sb-field-label" htmlFor="sb-goal-objective">
          {t("goal.objective")}
        </label>
        <textarea
          id="sb-goal-objective"
          value={objective}
          onChange={(event) => setObjective(event.target.value)}
          className="sb-field !min-h-[64px]"
          placeholder={t("goal.form.objectivePlaceholder")}
          maxLength={2000}
          required
        />
        <label className="sb-field-label mt-3" htmlFor="sb-goal-acceptance">
          {t("goal.acceptance")}
        </label>
        <textarea
          id="sb-goal-acceptance"
          value={acceptance}
          onChange={(event) => setAcceptance(event.target.value)}
          className="sb-field !min-h-[64px]"
          placeholder={t("goal.form.acceptancePlaceholder")}
          maxLength={2000}
          required
        />
        <label className="sb-field-label mt-3" htmlFor="sb-goal-constraints">
          {t("goal.form.constraints")}
        </label>
        <textarea
          id="sb-goal-constraints"
          value={constraints}
          onChange={(event) => setConstraints(event.target.value)}
          className="sb-field !min-h-[56px]"
          placeholder={t("goal.form.constraintsPlaceholder")}
        />

        <div className="mt-4 flex items-center justify-between">
          <span className="text-[12px] leading-[16px] text-[var(--sb-text-2)]">
            {t("goal.form.stages", { n: stages.length, max: MAX_STAGES })}
          </span>
          <button
            type="button"
            className="sb-btn"
            data-variant="secondary"
            data-size="sm"
            disabled={stages.length >= MAX_STAGES}
            onClick={() => setStages((prev) => [...prev, { key: nextKey(), title: "", acceptance: "" }])}
          >
            <Plus className="h-3 w-3" />
            {t("goal.form.addStage")}
          </button>
        </div>
        <ol className="mt-2 space-y-2">
          {stages.map((stage, index) => (
            <li key={stage.key} className="rounded-xl border border-[var(--sb-border)] p-2.5">
              <div className="flex items-center gap-1.5">
                <span className="w-5 shrink-0 text-center text-[12px] tabular-nums text-[var(--sb-text-3)]">
                  {index + 1}
                </span>
                <input
                  value={stage.title}
                  onChange={(event) => updateStage(stage.key, { title: event.target.value })}
                  className="sb-field !h-9 min-w-0 flex-1 !text-[13px]"
                  placeholder={t("goal.form.stageTitle")}
                  aria-label={t("goal.form.stageTitleAria", { n: index + 1 })}
                  maxLength={300}
                  required
                />
                <button
                  type="button"
                  className="sb-icon-btn"
                  data-size="sm"
                  data-variant="plain"
                  disabled={index === 0}
                  onClick={() => moveStage(index, -1)}
                  aria-label={t("goal.form.moveUp", { n: index + 1 })}
                  title={t("goal.form.moveUp", { n: index + 1 })}
                >
                  <ArrowUp className="h-3.5 w-3.5" />
                </button>
                <button
                  type="button"
                  className="sb-icon-btn"
                  data-size="sm"
                  data-variant="plain"
                  disabled={index === stages.length - 1}
                  onClick={() => moveStage(index, 1)}
                  aria-label={t("goal.form.moveDown", { n: index + 1 })}
                  title={t("goal.form.moveDown", { n: index + 1 })}
                >
                  <ArrowDown className="h-3.5 w-3.5" />
                </button>
                <button
                  type="button"
                  className="sb-icon-btn"
                  data-size="sm"
                  data-variant="plain"
                  disabled={stages.length <= 1}
                  onClick={() => setStages((prev) => prev.filter((item) => item.key !== stage.key))}
                  aria-label={t("goal.form.removeStage", { n: index + 1 })}
                  title={t("goal.form.removeStage", { n: index + 1 })}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </div>
              <textarea
                value={stage.acceptance}
                onChange={(event) => updateStage(stage.key, { acceptance: event.target.value })}
                className="sb-field mt-1.5 !min-h-[52px] !text-[13px] !leading-[20px]"
                placeholder={t("goal.form.stageAcceptance")}
                aria-label={t("goal.form.stageAcceptanceAria", { n: index + 1 })}
                maxLength={2000}
                required
              />
            </li>
          ))}
        </ol>
        <InlineError ctl={ctl} scope="form" />
      </form>
    </Modal>
  );
}

/**
 * "Goal" section of the group info panel: draft, steer and review the goal
 * the team is working towards.
 */
export function GoalPanel({ ctl, bots }: { ctl: GoalController; bots: Bot[] }) {
  const { t } = useLanguage();
  const { confirm, confirmElement } = useConfirm();
  const [formMode, setFormMode] = useState<"draft" | "revise" | null>(null);
  const { clearError } = ctl;
  const closeForm = useCallback(() => {
    setFormMode(null);
    clearError();
  }, [clearError]);

  const goal = ctl.data?.goal ?? null;
  const working = ctl.busy !== null;
  const total = goal?.stages.length ?? 0;
  const passed = goal ? passedCount(goal) : 0;

  const stopGoal = async (discard: boolean) => {
    const ok = await confirm({
      title: discard ? t("goal.confirm.discardTitle") : t("goal.confirm.stopTitle"),
      description: discard ? t("goal.confirm.discardBody") : t("goal.confirm.stopBody"),
      confirmLabel: discard ? t("goal.action.discard") : t("goal.action.stop"),
      cancelLabel: t("goal.cancel"),
      danger: true,
    });
    if (ok) void ctl.act({ action: "stop" }, "header");
  };

  let body: React.ReactNode;
  if (!ctl.data) {
    body = (
      <div className="sb-card flex items-center justify-center gap-2 px-3.5 py-5 text-[12px] text-[var(--sb-text-3)]">
        {ctl.loadError !== null ? (
          <>
            <span className="min-w-0 break-words text-[var(--sb-danger)]">
              {ctl.loadError || t("goal.loadFailed")}
            </span>
            <button
              type="button"
              className="sb-btn shrink-0"
              data-variant="secondary"
              data-size="sm"
              onClick={() => void ctl.reload()}
            >
              {t("goal.retry")}
            </button>
          </>
        ) : (
          <Loader2 className="sb-spin h-4 w-4" aria-label={t("goal.loading")} />
        )}
      </div>
    );
  } else if (!goal) {
    body = (
      <div className="sb-card px-3.5 py-3">
        <p className="text-[12px] leading-[18px] text-[var(--sb-text-2)]">{t("goal.empty.explain")}</p>
        <button
          type="button"
          className="sb-btn mt-2.5"
          data-variant="primary"
          data-size="sm"
          onClick={() => setFormMode("draft")}
        >
          <Flag className="h-3 w-3" />
          {t("goal.action.set")}
        </button>
        <p className="mt-2 text-[11px] leading-[16px] text-[var(--sb-text-3)]">{t("goal.empty.askLead")}</p>
      </div>
    );
  } else if (goal.status === "drafting") {
    body = (
      <>
        <div className="sb-card px-3.5 py-3">
          <GoalBrief goal={goal} />
        </div>
        <ol className="sb-card mt-2">
          {goal.stages.map((stage, index) => (
            <StageItem key={index} stage={stage} index={index} state="pending" bots={bots} />
          ))}
        </ol>
        <div className="mt-2.5 flex flex-wrap gap-1.5">
          <button
            type="button"
            className="sb-btn"
            data-variant="primary"
            data-size="sm"
            disabled={working}
            onClick={() => void ctl.act({ action: "start" }, "header")}
          >
            {ctl.busy === "header" && <Loader2 className="sb-spin h-3 w-3" />}
            {t("goal.action.start")}
          </button>
          <button
            type="button"
            className="sb-btn"
            data-variant="secondary"
            data-size="sm"
            disabled={working}
            onClick={() => setFormMode("revise")}
          >
            {t("goal.action.edit")}
          </button>
          <button
            type="button"
            className="sb-btn"
            data-variant="ghost"
            data-size="sm"
            disabled={working}
            onClick={() => void stopGoal(true)}
          >
            {t("goal.action.discard")}
          </button>
        </div>
        <InlineError ctl={ctl} scope="header" />
      </>
    );
  } else if (goal.status === "active" || goal.status === "paused") {
    const running = goal.status === "active";
    const percent = total > 0 ? Math.round((passed / total) * 100) : 0;
    body = (
      <>
        <div className="sb-card px-3.5 py-3">
          <div className="flex items-center gap-2">
            <div
              className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full bg-[var(--sb-hover)]"
              role="progressbar"
              aria-valuemin={0}
              aria-valuemax={total}
              aria-valuenow={passed}
              aria-label={t("goal.progress", { n: passed, m: total })}
            >
              <div
                className="h-full rounded-full bg-[var(--sb-success)] transition-[width]"
                style={{ width: `${percent}%` }}
              />
            </div>
            <span className="shrink-0 text-[11px] tabular-nums text-[var(--sb-text-3)]">
              {t("goal.progress", { n: passed, m: total })}
            </span>
          </div>
          <div className="mt-2.5">
            <GoalBrief goal={goal} />
          </div>
          <div className="mt-3 flex flex-wrap gap-1.5">
            <button
              type="button"
              className="sb-btn"
              data-variant="secondary"
              data-size="sm"
              disabled={working}
              onClick={() => void ctl.act({ action: running ? "pause" : "resume" }, "header")}
            >
              {ctl.busy === "header" && <Loader2 className="sb-spin h-3 w-3" />}
              {running ? t("goal.action.pause") : t("goal.action.resume")}
            </button>
            <button
              type="button"
              className="sb-btn"
              data-variant="ghost"
              data-size="sm"
              disabled={working}
              onClick={() => void stopGoal(false)}
            >
              {t("goal.action.stop")}
            </button>
          </div>
          <InlineError ctl={ctl} scope="header" />
          {!running && (
            <p className="mt-2 text-[11px] leading-[16px] text-[var(--sb-text-3)]">{t("goal.pausedHint")}</p>
          )}
        </div>
        <ol className="sb-card mt-2">
          {goal.stages.map((stage, index) => {
            const state =
              stage.status === "passed" ? "passed" : index === goal.current ? "current" : "pending";
            return (
              <StageItem
                key={index}
                stage={stage}
                index={index}
                state={state}
                bots={bots}
                actions={state === "current" && running ? <CurrentStageActions ctl={ctl} /> : undefined}
              />
            );
          })}
          {running && goal.current >= total && <CompleteForm ctl={ctl} />}
        </ol>
      </>
    );
  } else {
    body = (
      <div className="sb-card px-3.5 py-3">
        <Field label={t("goal.objective")}>
          <LongText text={goal.objective} />
        </Field>
        <Field label={t("goal.stagesLabel")}>{t("goal.progress", { n: passed, m: total })}</Field>
        {goal.evidence && (
          <Field label={t("goal.finalEvidence")}>
            <LongText text={goal.evidence} />
          </Field>
        )}
        <button
          type="button"
          className="sb-btn mt-3"
          data-variant="secondary"
          data-size="sm"
          disabled={working}
          onClick={() => setFormMode("draft")}
        >
          <Flag className="h-3 w-3" />
          {t("goal.action.setNew")}
        </button>
      </div>
    );
  }

  return (
    <section id={GOAL_SECTION_ID} aria-label={t("goal.title")} data-goal-status={goal?.status ?? "none"}>
      <div className="sb-info-label flex items-center justify-between gap-2 !mt-0">
        <span>{t("goal.title")}</span>
        {goal && (
          <StatusPill tone={goalTone(goal.status)} pulse={goal.status === "active"}>
            {t(`goal.status.${goal.status}`)}
          </StatusPill>
        )}
      </div>
      {body}
      {ctl.notice && (
        <div
          className="mt-2 whitespace-pre-wrap break-words rounded-lg bg-[var(--sb-hover)] px-2.5 py-1.5 text-[12px] leading-[18px] text-[var(--sb-text-2)]"
          role="status"
        >
          {ctl.notice}
        </div>
      )}
      {goal && <ActivityList events={goal.events} />}
      {formMode && (
        <GoalFormDialog
          mode={formMode}
          initial={formMode === "revise" ? goal : null}
          ctl={ctl}
          onClose={closeForm}
        />
      )}
      {confirmElement}
    </section>
  );
}
