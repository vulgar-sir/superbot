"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { ArrowUp, Copy, CornerDownLeft, Loader2, MessageCircleQuestion, RotateCcw, Trash2, X } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import { cn, timeAgo } from "@/lib/format";
import type { SideChat } from "@/lib/types";
import { useLanguage } from "./LanguageProvider";
import { Markdown } from "./Markdown";
import { useConfirm } from "./ui/Confirm";
import { useToast } from "./ui/Toast";

/** A side chat as listed; `local` marks one that never reached the server. */
export type SideEntry = SideChat & { local?: boolean };

export type SideController = {
  items: SideEntry[];
  /** The question being answered right now, if any. */
  pending: string | null;
  loading: boolean;
  /** null = no error; "" = failed without a server message. */
  loadError: string | null;
  /** Returns false when the question was not accepted (empty or one is already in flight). */
  ask: (question: string) => Promise<boolean>;
  remove: (entry: SideEntry) => Promise<void>;
  retry: (entry: SideEntry) => Promise<void>;
  reload: () => Promise<void>;
};

const SIDE_COMMAND = /^\/side(?:\s+([\s\S]*))?$/i;

/**
 * Recognises the `/side question` composer command. Returns null for normal
 * messages, otherwise the question ("" when only `/side` was typed).
 */
export function parseSideCommand(text: string): string | null {
  const match = text.trim().match(SIDE_COMMAND);
  if (!match) return null;
  return (match[1] ?? "").trim();
}

/**
 * Side questions of one conversation: quick answers from a snapshot of the
 * chat that never enter the transcript or interrupt a running task.
 */
export function useSideChats(conversationId: string | null): SideController {
  const [items, setItems] = useState<SideEntry[]>([]);
  const [pending, setPending] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const currentRef = useRef<string | null>(null);
  const pendingRef = useRef(false);

  const reload = useCallback(async () => {
    const id = currentRef.current;
    if (!id) return;
    setLoading(true);
    try {
      const result = await api.listSideChats(id);
      if (currentRef.current !== id) return;
      const listed = result.side_chats ?? [];
      // Keep local failures; they exist only in this browser.
      setItems((prev) => [...listed, ...prev.filter((item) => item.local)]);
      setLoadError(null);
    } catch (err) {
      if (currentRef.current !== id) return;
      setLoadError(err instanceof ApiError ? err.message : "");
    } finally {
      if (currentRef.current === id) setLoading(false);
    }
  }, []);

  useEffect(() => {
    currentRef.current = conversationId;
    pendingRef.current = false;
    setItems([]);
    setPending(null);
    setLoading(false);
    setLoadError(null);
    if (conversationId) void reload();
  }, [conversationId, reload]);

  const ask = useCallback(async (raw: string) => {
    const question = raw.trim();
    const id = currentRef.current;
    if (!question || !id || pendingRef.current) return false;
    pendingRef.current = true;
    setPending(question);
    try {
      const result = await api.askSide(id, question);
      if (currentRef.current === id) {
        const entry = result.side_chat;
        setItems((prev) => [...prev.filter((item) => item.id !== entry.id), entry]);
      }
    } catch (err) {
      if (currentRef.current === id) {
        const failed: SideEntry = {
          id: `local-${Date.now()}-${Math.random().toString(16).slice(2, 8)}`,
          bot_id: null,
          question,
          answer: "",
          status: "failed",
          error: err instanceof ApiError ? err.message : "",
          created_at: new Date().toISOString(),
          answered_at: null,
          local: true,
        };
        setItems((prev) => [...prev, failed]);
      }
    } finally {
      if (currentRef.current === id) {
        pendingRef.current = false;
        setPending(null);
      }
    }
    return true;
  }, []);

  const remove = useCallback(async (entry: SideEntry) => {
    const id = currentRef.current;
    if (!id) return;
    if (!entry.local) {
      try {
        await api.deleteSideChat(id, entry.id);
      } catch (err) {
        // Already gone on the server is as good as deleted.
        if (!(err instanceof ApiError && err.status === 404)) throw err;
      }
    }
    if (currentRef.current === id) setItems((prev) => prev.filter((item) => item.id !== entry.id));
  }, []);

  const retry = useCallback(
    async (entry: SideEntry) => {
      if (pendingRef.current) return;
      try {
        await remove(entry);
      } catch {
        // Keep going: the new attempt matters more than tidying the old one.
        setItems((prev) => prev.filter((item) => item.id !== entry.id));
      }
      await ask(entry.question);
    },
    [ask, remove],
  );

  return { items, pending, loading, loadError, ask, remove, retry, reload };
}

function IconAction({
  label,
  onClick,
  danger,
  disabled,
  children,
}: {
  label: string;
  onClick: () => void;
  danger?: boolean;
  disabled?: boolean;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      title={label}
      className={cn(
        "flex h-6 w-6 shrink-0 items-center justify-center rounded text-[var(--sb-text-3)] transition hover:bg-[var(--sb-hover)] disabled:opacity-40",
        danger ? "hover:text-[var(--sb-danger)]" : "hover:text-[var(--sb-text)]",
      )}
    >
      {children}
    </button>
  );
}

/**
 * Right-hand drawer for side questions. It takes the place of the info panel
 * while open and follows the same docked / overlay behaviour.
 */
export function SidePanel({
  ctl,
  focusSignal,
  onClose,
  onInsert,
}: {
  ctl: SideController;
  /** Change to a new positive value to move focus to the panel's input; 0 leaves focus alone. */
  focusSignal: number;
  onClose: () => void;
  onInsert: (text: string) => void;
}) {
  const { t } = useLanguage();
  const { toast } = useToast();
  const { confirm, confirmElement } = useConfirm();
  const [draft, setDraft] = useState("");
  const [busyID, setBusyID] = useState<string | null>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const asking = ctl.pending !== null;

  useEffect(() => {
    if (focusSignal > 0) inputRef.current?.focus();
  }, [focusSignal]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: "end" });
  }, [ctl.items.length, ctl.pending]);

  useEffect(() => {
    const el = inputRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 140)}px`;
  }, [draft]);

  const submit = async () => {
    const question = draft.trim();
    if (!question || asking) return;
    setDraft("");
    const accepted = await ctl.ask(question);
    if (!accepted) setDraft(question);
  };

  const copyAnswer = async (entry: SideEntry) => {
    try {
      await navigator.clipboard.writeText(entry.answer);
      toast(t("side.copied"), "success");
    } catch {
      toast(t("side.copyFailed"), "danger");
    }
  };

  const removeEntry = async (entry: SideEntry) => {
    if (entry.status === "answered") {
      const ok = await confirm({
        title: t("side.deleteTitle"),
        description: entry.question.slice(0, 80),
        confirmLabel: t("side.delete"),
        cancelLabel: t("side.cancel"),
        danger: true,
      });
      if (!ok) return;
    }
    setBusyID(entry.id);
    try {
      await ctl.remove(entry);
    } catch (err) {
      toast(err instanceof ApiError && err.message ? err.message : t("side.deleteFailed"), "danger");
    } finally {
      setBusyID(null);
    }
  };

  const empty = ctl.items.length === 0 && !asking;

  return (
    <aside
      className="sb-info"
      aria-label={t("side.title")}
      data-side-panel
      onKeyDown={(event) => {
        if (event.key === "Escape" && !event.defaultPrevented && !event.nativeEvent.isComposing) {
          event.stopPropagation();
          onClose();
        }
      }}
    >
      {/* Close sits on the left: the desktop shell draws its window buttons
          over the right end of this bar. */}
      <div className="sb-info-header !justify-start gap-2.5">
        <button type="button" className="sb-icon-btn" onClick={onClose} aria-label={t("side.close")}>
          <X className="h-4 w-4" />
        </button>
        <h2 className="min-w-0 truncate text-[15px] font-medium leading-[20px]">{t("side.title")}</h2>
      </div>
      <p className="px-4 pb-3 text-[12px] leading-[18px] text-[var(--sb-text-3)]">{t("side.explain")}</p>

      <div className="sb-info-scroll no-scrollbar" aria-live="polite">
        {ctl.loadError !== null && (
          <div className="mb-2 flex items-center gap-2 rounded-lg bg-[var(--sb-danger-soft)] px-3 py-2 text-[12px] leading-[18px] text-[var(--sb-danger)]">
            <span className="min-w-0 flex-1 break-words">{ctl.loadError || t("side.loadFailed")}</span>
            <button
              type="button"
              className="sb-btn shrink-0"
              data-variant="secondary"
              data-size="sm"
              onClick={() => void ctl.reload()}
            >
              {t("side.retry")}
            </button>
          </div>
        )}
        {ctl.loading && ctl.items.length === 0 ? (
          <div className="flex justify-center py-8">
            <Loader2 className="sb-spin h-4 w-4 text-[var(--sb-text-3)]" aria-label={t("side.loading")} />
          </div>
        ) : empty && ctl.loadError === null ? (
          <div className="sb-empty">
            <MessageCircleQuestion className="h-5 w-5" aria-hidden />
            <div className="sb-empty-title">{t("side.emptyTitle")}</div>
            <div className="sb-empty-desc">{t("side.emptyDesc")}</div>
          </div>
        ) : (
          <ol className="space-y-4 pb-1">
            {ctl.items.map((entry) => (
              <li key={entry.id} data-side-status={entry.status}>
                <div className="flex justify-end">
                  <div className="sb-user-bubble max-w-[88%] !text-[14px] !leading-[21px]">{entry.question}</div>
                </div>
                {entry.status === "answered" ? (
                  <>
                    <div className="mt-1.5 max-w-full">
                      <div className="sb-bubble" data-group="single">
                        <Markdown content={entry.answer} className="!text-[14px] !leading-[21px]" />
                      </div>
                    </div>
                    <div className="mt-1 flex items-center gap-0.5 text-[11px] text-[var(--sb-text-4)]">
                      <span className="mr-auto pl-1">{timeAgo(entry.answered_at ?? entry.created_at)}</span>
                      <IconAction label={t("side.copy")} onClick={() => void copyAnswer(entry)}>
                        <Copy className="h-3.5 w-3.5" />
                      </IconAction>
                      <IconAction label={t("side.insert")} onClick={() => onInsert(entry.answer)}>
                        <CornerDownLeft className="h-3.5 w-3.5" />
                      </IconAction>
                      <IconAction
                        label={t("side.delete")}
                        danger
                        disabled={busyID === entry.id}
                        onClick={() => void removeEntry(entry)}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </IconAction>
                    </div>
                  </>
                ) : entry.status === "failed" ? (
                  <div
                    className="mt-1.5 rounded-xl bg-[var(--sb-danger-soft)] px-3 py-2 text-[12px] leading-[18px] text-[var(--sb-danger)]"
                    role="alert"
                  >
                    <div className="font-medium">{t("side.failed")}</div>
                    {entry.error && <div className="whitespace-pre-wrap break-words">{entry.error}</div>}
                    <div className="mt-1.5 flex items-center gap-1.5">
                      <button
                        type="button"
                        className="sb-btn"
                        data-variant="secondary"
                        data-size="sm"
                        disabled={asking || busyID === entry.id}
                        onClick={() => void ctl.retry(entry)}
                      >
                        <RotateCcw className="h-3 w-3" />
                        {t("side.retry")}
                      </button>
                      <button
                        type="button"
                        className="sb-btn"
                        data-variant="ghost"
                        data-size="sm"
                        disabled={busyID === entry.id}
                        onClick={() => void removeEntry(entry)}
                      >
                        {t("side.delete")}
                      </button>
                    </div>
                  </div>
                ) : (
                  <div className="mt-1.5 flex items-center gap-2 pl-1 text-[12px] text-[var(--sb-text-3)]">
                    <Loader2 className="sb-spin h-3.5 w-3.5" aria-hidden />
                    <span className="mr-auto">{t("side.answering")}</span>
                    <IconAction
                      label={t("side.delete")}
                      danger
                      disabled={busyID === entry.id}
                      onClick={() => void removeEntry(entry)}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </IconAction>
                  </div>
                )}
              </li>
            ))}
            {ctl.pending !== null && (
              <li data-side-status="pending">
                <div className="flex justify-end">
                  <div className="sb-user-bubble max-w-[88%] !text-[14px] !leading-[21px]">{ctl.pending}</div>
                </div>
                <div
                  className="mt-1.5 flex items-center gap-2 pl-1 text-[12px] text-[var(--sb-text-3)]"
                  role="status"
                >
                  <Loader2 className="sb-spin h-3.5 w-3.5" aria-hidden />
                  {t("side.answering")}
                </div>
              </li>
            )}
          </ol>
        )}
        <div ref={bottomRef} />
      </div>

      <form
        className="border-t border-[var(--sb-border)] p-3"
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
      >
        <div className="flex items-end gap-2">
          <textarea
            id="superbot-side-input"
            ref={inputRef}
            value={draft}
            rows={1}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
                event.preventDefault();
                void submit();
              }
            }}
            className="sb-field !min-h-[40px] min-w-0 flex-1 !resize-none !py-2 !text-[14px] !leading-[22px]"
            placeholder={t("side.placeholder")}
            aria-label={t("side.placeholder")}
            maxLength={2000}
          />
          <button
            type="submit"
            className="sb-send-btn mb-1.5"
            disabled={asking || !draft.trim()}
            aria-label={t("side.ask")}
            title={asking ? t("side.waitPrevious") : t("side.ask")}
          >
            {asking ? (
              <Loader2 className="sb-spin h-4 w-4" />
            ) : (
              <ArrowUp className="h-[18px] w-[18px]" strokeWidth={2.2} />
            )}
          </button>
        </div>
      </form>
      {confirmElement}
    </aside>
  );
}
