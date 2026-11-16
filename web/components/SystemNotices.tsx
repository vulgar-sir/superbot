"use client";

import { useCallback, useEffect, useState } from "react";
import { ChevronDown, ChevronRight, Clock, Flag, Layers, Loader2, Workflow } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import { cn, formatTime } from "@/lib/format";
import type { ConversationSummary, Message } from "@/lib/types";
import { useLanguage } from "./LanguageProvider";
import { Modal } from "./ui/Modal";

/**
 * Divider row marking where older history was folded into a checkpoint
 * summary. Clicking it shows the notes the bot now works from.
 */
export function CheckpointDivider({
  message,
  conversationId,
}: {
  message: Message;
  conversationId?: string | null;
}) {
  const { t } = useLanguage();
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [summary, setSummary] = useState<ConversationSummary | null>(null);
  // True when the marker's own checkpoint is gone and the latest is shown.
  const [substituted, setSubstituted] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const close = useCallback(() => setOpen(false), []);

  const metadata = message.metadata ?? {};
  const number = Number(metadata.number);
  const hasNumber = Number.isFinite(number) && number > 0;
  const fallback = metadata.source === "fallback";
  const summaryID = typeof metadata.summary_id === "string" ? metadata.summary_id : "";
  const label = hasNumber ? t("chat.checkpoint.label", { n: number }) : t("chat.checkpoint.labelNoNumber");

  useEffect(() => {
    if (!open || !conversationId) return;
    let cancelled = false;
    setLoading(true);
    setError(null);
    api
      .listConversationSummaries(conversationId)
      .then((result) => {
        if (cancelled) return;
        // Newest first: open the checkpoint this marker refers to, or the
        // latest one when it cannot be found.
        const summaries = result.summaries ?? [];
        const exact = summaryID ? summaries.find((item) => item.id === summaryID) : undefined;
        setSummary(exact ?? summaries[0] ?? null);
        setSubstituted(Boolean(summaryID) && !exact && summaries.length > 0);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof ApiError ? err.message : t("chat.checkpoint.loadFailed"));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open, conversationId, summaryID, t]);

  return (
    <div className="flex justify-center py-1" data-message-id={message.id} data-kind="context_checkpoint">
      <button
        type="button"
        onClick={() => setOpen(true)}
        disabled={!conversationId}
        aria-haspopup="dialog"
        title={t("chat.checkpoint.open")}
        className="sb-agent-card"
      >
        <span className="h-px min-w-[24px] flex-1 bg-[var(--sb-border)]" aria-hidden />
        <Layers className="h-3.5 w-3.5 shrink-0" aria-hidden />
        <span>{label}</span>
        {fallback && (
          <span className="rounded-full bg-[var(--sb-warn-soft)] px-1.5 py-px text-[11px] text-[var(--sb-warn)]">
            {t("chat.checkpoint.fallback")}
          </span>
        )}
        <span className="h-px min-w-[24px] flex-1 bg-[var(--sb-border)]" aria-hidden />
      </button>
      <Modal
        open={open}
        onClose={close}
        title={hasNumber ? t("chat.checkpoint.title", { n: number }) : t("chat.checkpoint.titleNoNumber")}
        subtitle={t("chat.checkpoint.explain")}
        size="md"
        footer={
          <button type="button" className="sb-btn" data-variant="secondary" data-size="sm" onClick={close}>
            {t("chat.checkpoint.close")}
          </button>
        }
      >
        {fallback && (
          <div className="mb-3 rounded-lg bg-[var(--sb-warn-soft)] px-2.5 py-1.5 text-[12px] text-[var(--sb-warn)]">
            {t("chat.checkpoint.fallback")}
          </div>
        )}
        {loading ? (
          <div className="flex items-center justify-center gap-2 py-8 text-[12px] text-[var(--sb-text-3)]">
            <Loader2 className="sb-spin h-4 w-4" />
            {t("chat.checkpoint.loading")}
          </div>
        ) : error ? (
          <div className="rounded-lg bg-[var(--sb-danger-soft)] px-3 py-2 text-[12px] text-[var(--sb-danger)]">
            {error}
          </div>
        ) : summary ? (
          <>
            {substituted && (
              <p className="mb-2 text-[12px] leading-[18px] text-[var(--sb-text-3)]">
                {t("chat.checkpoint.newer")}
              </p>
            )}
            <div
              className="max-h-[52vh] overflow-y-auto whitespace-pre-wrap break-words rounded-xl border border-[var(--sb-border)] bg-[var(--sb-bg)] p-3 text-[13px] leading-[20px] text-[var(--sb-text)]"
              tabIndex={0}
            >
              {summary.content}
            </div>
            <div className="mt-2 text-[11px] text-[var(--sb-text-4)]">{formatTime(summary.created_at)}</div>
          </>
        ) : (
          <div className="py-8 text-center text-[12px] text-[var(--sb-text-3)]">
            {t("chat.checkpoint.empty")}
          </div>
        )}
      </Modal>
    </div>
  );
}

const SUBAGENT_PREFIX = /^(子代理已完成[：:]\s*|Subagent finished:\s*)/;
const PREVIEW_CHARS = 160;

/**
 * Folded card for a subagent result that was handed back to the bot. The
 * collapsed state shows a short preview; expanding replaces it with the full
 * text the bot received.
 */
export function SubagentResultCard({ message }: { message: Message }) {
  const { t } = useLanguage();
  const [expanded, setExpanded] = useState(false);
  const body = message.content.replace(SUBAGENT_PREFIX, "").trim();
  const injectText =
    message.metadata?.inject === "true" && typeof message.metadata.inject_text === "string"
      ? message.metadata.inject_text.trim()
      : "";
  const full = injectText || body;
  const chars = Array.from(body);
  const preview = chars.length > PREVIEW_CHARS ? `${chars.slice(0, PREVIEW_CHARS).join("")}…` : body;
  const expandable = full.length > 0 && full !== preview;
  const panelID = `subagent-result-${message.id}`;

  return (
    <div className="flex justify-center py-1" data-message-id={message.id} data-kind="subagent_result">
      <div className="w-full max-w-[520px] rounded-xl border border-[var(--sb-border)] px-3 py-1">
        <button
          type="button"
          onClick={() => expandable && setExpanded((prev) => !prev)}
          aria-expanded={expandable ? expanded : undefined}
          aria-controls={expandable ? panelID : undefined}
          className={cn("sb-agent-card !justify-start !py-1.5", !expandable && "cursor-default")}
        >
          <Workflow className="h-3.5 w-3.5 shrink-0" aria-hidden />
          <span className="font-medium">{t("chat.subagent.title")}</span>
          {expandable && (
            <span className="ml-auto flex items-center gap-0.5 text-[11px] text-[var(--sb-text-3)]">
              {expanded ? t("chat.subagent.collapse") : t("chat.subagent.expand")}
              {expanded ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
            </span>
          )}
        </button>
        <div
          id={panelID}
          className={cn(
            "whitespace-pre-wrap break-words pb-2 text-[12px] leading-[18px] text-[var(--sb-text-2)]",
            expanded && "max-h-[320px] overflow-y-auto",
          )}
        >
          {expanded ? full : preview}
        </div>
      </div>
    </div>
  );
}

/**
 * Thin divider for a goal check: the server reminded the lead bot about the
 * group goal. The reminder text itself is available on demand.
 */
export function GoalNudgeDivider({ message }: { message: Message }) {
  const { t } = useLanguage();
  const [expanded, setExpanded] = useState(false);
  const injectText =
    typeof message.metadata?.inject_text === "string" ? message.metadata.inject_text.trim() : "";
  const panelID = `goal-nudge-${message.id}`;

  return (
    <div className="flex flex-col items-center py-1" data-message-id={message.id} data-kind="goal_nudge">
      <button
        type="button"
        onClick={() => injectText && setExpanded((prev) => !prev)}
        aria-expanded={injectText ? expanded : undefined}
        aria-controls={injectText ? panelID : undefined}
        title={injectText ? t("chat.goalNudge.open") : undefined}
        className={cn("sb-agent-card", !injectText && "cursor-default")}
      >
        <span className="h-px min-w-[24px] flex-1 bg-[var(--sb-border)]" aria-hidden />
        <Flag className="h-3.5 w-3.5 shrink-0" aria-hidden />
        <span>{t("chat.goalNudge.label")}</span>
        {injectText && (
          <ChevronRight className={cn("h-3 w-3 opacity-50 transition", expanded && "rotate-90")} aria-hidden />
        )}
        <span className="h-px min-w-[24px] flex-1 bg-[var(--sb-border)]" aria-hidden />
      </button>
      {expanded && injectText && (
        <div
          id={panelID}
          className="max-h-[320px] w-full max-w-[520px] overflow-y-auto whitespace-pre-wrap break-words rounded-lg bg-[var(--sb-hover)] px-2.5 py-1.5 text-[11px] leading-[16px] text-[var(--sb-text-2)]"
        >
          <div className="mb-1 text-[var(--sb-text-3)]">{t("chat.goalNudge.sent")}</div>
          {injectText}
        </div>
      )}
    </div>
  );
}

/** A group routine posted this message on its schedule; nobody typed it. */
export function isRoutineTrigger(message: Message): boolean {
  return message.sender_type === "user" && message.metadata?.kind === "routine";
}

// The server prefixes the routine prompt with a "【例程：name】" line.
const ROUTINE_PREFIX = /^(?:【例程[:：]([^】\n]*)】|\[Routine:([^\]\n]*)\])[ \t]*\r?\n?/;

/**
 * Thin divider for a scheduled routine firing in a group. The prompt it sent
 * to the group is available on demand.
 */
export function RoutineTriggerDivider({ message }: { message: Message }) {
  const { t } = useLanguage();
  const [expanded, setExpanded] = useState(false);
  const match = message.content.match(ROUTINE_PREFIX);
  const metaName =
    typeof message.metadata?.routine_name === "string" ? message.metadata.routine_name.trim() : "";
  const name = metaName || (match?.[1] ?? match?.[2] ?? "").trim();
  const prompt = (match ? message.content.slice(match[0].length) : message.content).trim();
  const panelID = `routine-trigger-${message.id}`;

  return (
    <div className="flex flex-col items-center py-1" data-message-id={message.id} data-kind="routine">
      <button
        type="button"
        onClick={() => prompt && setExpanded((prev) => !prev)}
        aria-expanded={prompt ? expanded : undefined}
        aria-controls={prompt ? panelID : undefined}
        title={prompt ? t("chat.routine.open") : undefined}
        className={cn("sb-agent-card", !prompt && "cursor-default")}
      >
        <span className="h-px min-w-[24px] flex-1 bg-[var(--sb-border)]" aria-hidden />
        <Clock className="h-3.5 w-3.5 shrink-0" aria-hidden />
        <span className="min-w-0 truncate">
          {name ? t("chat.routine.label", { name }) : t("chat.routine.labelNoName")}
        </span>
        <span className="shrink-0 text-[11px] text-[var(--sb-text-4)]">{formatTime(message.created_at)}</span>
        {prompt && (
          <ChevronRight className={cn("h-3 w-3 shrink-0 opacity-50 transition", expanded && "rotate-90")} aria-hidden />
        )}
        <span className="h-px min-w-[24px] flex-1 bg-[var(--sb-border)]" aria-hidden />
      </button>
      {expanded && prompt && (
        <div
          id={panelID}
          className="max-h-[320px] w-full max-w-[520px] overflow-y-auto whitespace-pre-wrap break-words rounded-lg bg-[var(--sb-hover)] px-2.5 py-1.5 text-[11px] leading-[16px] text-[var(--sb-text-2)]"
        >
          <div className="mb-1 text-[var(--sb-text-3)]">{t("chat.routine.sent")}</div>
          {prompt}
        </div>
      )}
    </div>
  );
}
