"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Archive, Brain, Loader2, MoreHorizontal, Pencil, Pin, PinOff, Plus, RotateCcw, Trash2 } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import { cn, timeAgo } from "@/lib/format";
import type { Memory } from "@/lib/types";
import { useLanguage } from "./LanguageProvider";
import { Menu, type MenuItem } from "./ui/Menu";
import { useConfirm } from "./ui/Confirm";
import { useToast } from "./ui/Toast";

const GROUP_MEMORY_SECTION_ID = "sb-group-memory-section";

const errorClass =
  "mt-2 whitespace-pre-wrap break-words rounded-lg bg-[var(--sb-danger-soft)] px-2.5 py-1.5 text-[12px] leading-[18px] text-[var(--sb-danger)]";

const badgeClass = "shrink-0 rounded-full bg-[var(--sb-hover)] px-1.5 py-px text-[var(--sb-text-3)]";

function errorText(err: unknown, fallback: string): string {
  return err instanceof ApiError && err.message ? err.message : fallback;
}

/**
 * Team-scoped memories of one group, shown in the group's info panel: the
 * agreements and facts everyone in the group shares. Personal facts are kept
 * in each person's own memory, not here.
 */
export function GroupMemoryList({ teamId }: { teamId: string }) {
  const { t } = useLanguage();
  const { toast } = useToast();
  const { confirm, confirmElement } = useConfirm();
  const [memories, setMemories] = useState<Memory[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [showArchived, setShowArchived] = useState(false);
  const [adding, setAdding] = useState(false);
  const [draft, setDraft] = useState("");
  const [saving, setSaving] = useState(false);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editDraft, setEditDraft] = useState("");
  const [menu, setMenu] = useState<{ memory: Memory; rect: DOMRect } | null>(null);
  // Similar older memories found when the last one was added; the user can
  // retire the ones the new memory makes outdated.
  const [related, setRelated] = useState<{ newId: string; items: Memory[] } | null>(null);
  const loadSeq = useRef(0);

  const load = useCallback(async () => {
    const seq = ++loadSeq.current;
    setLoading(true);
    setLoadError(null);
    try {
      const result = await api.listTeamMemories(teamId, showArchived);
      if (seq !== loadSeq.current) return;
      setMemories(result.memories);
    } catch (err) {
      if (seq !== loadSeq.current) return;
      // Empty string means "no server message"; the render falls back to the
      // dictionary text so a language switch does not need a reload.
      setLoadError(err instanceof ApiError ? err.message : "");
    } finally {
      if (seq === loadSeq.current) setLoading(false);
    }
  }, [teamId, showArchived]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    setMemories([]);
    setActionError(null);
    setRelated(null);
    setAdding(false);
    setEditingId(null);
    setMenu(null);
  }, [teamId]);

  const replace = useCallback((memory: Memory) => {
    setMemories((prev) => prev.map((item) => (item.id === memory.id ? memory : item)));
  }, []);

  const add = useCallback(async () => {
    const content = draft.trim();
    if (!content) return;
    setSaving(true);
    setActionError(null);
    try {
      const result = await api.createTeamMemory(teamId, content);
      setMemories((prev) => [result.memory, ...prev.filter((item) => item.id !== result.memory.id)]);
      setDraft("");
      setAdding(false);
      const retired = result.superseded?.length ?? 0;
      if (result.duplicate) {
        toast(t("memory.duplicate"));
      } else if (result.pending) {
        toast(t("memory.savedPending"));
      } else {
        toast(retired > 0 ? t("memory.supersededCount", { n: retired }) : t("groupMemory.added"), "success");
      }
      const candidates = (result.related ?? []).filter((item) => !item.archived_at);
      setRelated(candidates.length > 0 ? { newId: result.memory.id, items: candidates } : null);
      if (retired > 0) void load();
    } catch (err) {
      setActionError(errorText(err, t("groupMemory.addFailed")));
    } finally {
      setSaving(false);
    }
  }, [draft, load, t, teamId, toast]);

  const retire = useCallback(
    async (memory: Memory, supersededBy?: string) => {
      setBusyId(memory.id);
      setActionError(null);
      try {
        const result = await api.archiveTeamMemory(teamId, memory.id, supersededBy);
        if (showArchived) {
          replace(result.memory);
        } else {
          setMemories((prev) => prev.filter((item) => item.id !== memory.id));
        }
        setRelated((prev) =>
          prev ? { ...prev, items: prev.items.filter((item) => item.id !== memory.id) } : prev,
        );
        if (supersededBy) toast(t("memory.retired"), "success");
      } catch (err) {
        setActionError(errorText(err, supersededBy ? t("memory.retireFailed") : t("groupMemory.archiveFailed")));
      } finally {
        setBusyId(null);
      }
    },
    [replace, showArchived, t, teamId, toast],
  );

  const togglePin = useCallback(
    async (memory: Memory) => {
      setBusyId(memory.id);
      setActionError(null);
      try {
        replace((await api.pinTeamMemory(teamId, memory.id, !memory.pinned)).memory);
      } catch (err) {
        setActionError(errorText(err, t("memory.pinFailed")));
      } finally {
        setBusyId(null);
      }
    },
    [replace, t, teamId],
  );

  const restore = useCallback(
    async (memory: Memory) => {
      setBusyId(memory.id);
      setActionError(null);
      try {
        replace((await api.restoreTeamMemory(teamId, memory.id)).memory);
        toast(t("memory.restored"), "success");
      } catch (err) {
        setActionError(errorText(err, t("memory.restoreFailed")));
      } finally {
        setBusyId(null);
      }
    },
    [replace, t, teamId, toast],
  );

  const saveEdit = useCallback(
    async (memory: Memory) => {
      const content = editDraft.trim();
      if (!content) return;
      setBusyId(memory.id);
      setActionError(null);
      try {
        replace((await api.updateTeamMemory(teamId, memory.id, content)).memory);
        setEditingId(null);
        toast(t("groupMemory.updated"), "success");
      } catch (err) {
        setActionError(errorText(err, t("groupMemory.updateFailed")));
      } finally {
        setBusyId(null);
      }
    },
    [editDraft, replace, t, teamId, toast],
  );

  const remove = useCallback(
    async (memory: Memory) => {
      const ok = await confirm({
        title: t("groupMemory.confirmDeleteTitle"),
        description: t("groupMemory.confirmDeleteBody"),
        confirmLabel: t("groupMemory.delete"),
        cancelLabel: t("groupMemory.cancel"),
        danger: true,
      });
      if (!ok) return;
      setActionError(null);
      try {
        await api.deleteTeamMemory(teamId, memory.id);
        setMemories((prev) => prev.filter((item) => item.id !== memory.id));
      } catch (err) {
        setActionError(errorText(err, t("groupMemory.deleteFailed")));
      }
    },
    [confirm, t, teamId],
  );

  // Archived rows go last, pinned rows first, newest first otherwise.
  const sorted = [...memories].sort((a, b) => {
    const archived = Number(Boolean(a.archived_at)) - Number(Boolean(b.archived_at));
    if (archived !== 0) return archived;
    const pinned = Number(Boolean(b.pinned)) - Number(Boolean(a.pinned));
    if (pinned !== 0) return pinned;
    return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
  });

  const menuItems = (memory: Memory): MenuItem[] =>
    memory.archived_at
      ? [
          {
            key: "restore",
            label: t("memory.restore"),
            icon: <RotateCcw className="h-3.5 w-3.5" />,
            onSelect: () => void restore(memory),
          },
          { key: "sep", separator: true },
          {
            key: "delete",
            label: t("groupMemory.delete"),
            icon: <Trash2 className="h-3.5 w-3.5" />,
            danger: true,
            onSelect: () => void remove(memory),
          },
        ]
      : [
          {
            key: "edit",
            label: t("groupMemory.edit"),
            icon: <Pencil className="h-3.5 w-3.5" />,
            onSelect: () => {
              setEditingId(memory.id);
              setEditDraft(memory.content);
            },
          },
          {
            key: "pin",
            label: memory.pinned ? t("memory.unpin") : t("memory.pin"),
            icon: memory.pinned ? <PinOff className="h-3.5 w-3.5" /> : <Pin className="h-3.5 w-3.5" />,
            onSelect: () => void togglePin(memory),
          },
          {
            key: "archive",
            label: t("groupMemory.archive"),
            icon: <Archive className="h-3.5 w-3.5" />,
            onSelect: () => void retire(memory),
          },
          { key: "sep", separator: true },
          {
            key: "delete",
            label: t("groupMemory.delete"),
            icon: <Trash2 className="h-3.5 w-3.5" />,
            danger: true,
            onSelect: () => void remove(memory),
          },
        ];

  let body: React.ReactNode;
  if (loading && memories.length === 0) {
    body = (
      <div className="sb-card flex items-center justify-center px-3.5 py-5 text-[var(--sb-text-3)]">
        <Loader2 className="sb-spin h-4 w-4" aria-label={t("groupMemory.loading")} />
      </div>
    );
  } else if (loadError !== null) {
    body = (
      <div className="sb-card flex items-center justify-center gap-2 px-3.5 py-4 text-[12px]">
        <span className="min-w-0 break-words text-[var(--sb-danger)]" role="alert">
          {loadError || t("groupMemory.loadFailed")}
        </span>
        <button
          type="button"
          className="sb-btn shrink-0"
          data-variant="secondary"
          data-size="sm"
          onClick={() => void load()}
        >
          {t("groupMemory.retry")}
        </button>
      </div>
    );
  } else if (memories.length === 0 && !adding) {
    body = (
      <div className="sb-card px-3.5 py-3">
        <p className="text-[12px] leading-[18px] text-[var(--sb-text-2)]">{t("groupMemory.empty")}</p>
        <button
          type="button"
          className="sb-btn mt-2.5"
          data-variant="secondary"
          data-size="sm"
          onClick={() => setAdding(true)}
        >
          <Plus className="h-3 w-3" />
          {t("groupMemory.add")}
        </button>
      </div>
    );
  } else {
    body = (
      <ul className="sb-card max-h-[320px] overflow-y-auto">
        {sorted.map((memory, index) => (
          <li
            key={memory.id}
            className={cn("flex items-start gap-2.5 px-3.5 py-2.5", index > 0 && "border-t border-[var(--sb-border)]")}
            data-memory-id={memory.id}
            data-archived={memory.archived_at ? "true" : undefined}
          >
            <Brain className="mt-0.5 h-4 w-4 shrink-0 text-[var(--sb-text-3)]" aria-hidden />
            <div className="min-w-0 flex-1">
              {editingId === memory.id ? (
                <div>
                  <textarea
                    autoFocus
                    value={editDraft}
                    onChange={(event) => setEditDraft(event.target.value)}
                    className="sb-field !min-h-[64px] !text-[12px]"
                  />
                  <div className="mt-1.5 flex justify-end gap-2">
                    <button
                      type="button"
                      onClick={() => setEditingId(null)}
                      className="text-[11px] text-[var(--sb-text-3)] hover:text-[var(--sb-text)]"
                    >
                      {t("groupMemory.cancel")}
                    </button>
                    <button
                      type="button"
                      disabled={busyId === memory.id || !editDraft.trim()}
                      onClick={() => void saveEdit(memory)}
                      className="text-[11px] font-medium text-[var(--sb-text)] disabled:opacity-50"
                    >
                      {t("groupMemory.save")}
                    </button>
                  </div>
                </div>
              ) : (
                <>
                  <div
                    className={cn(
                      "break-words text-[13px] leading-[18px]",
                      memory.archived_at ? "text-[var(--sb-text-3)]" : "text-[var(--sb-text)]",
                    )}
                  >
                    {memory.content}
                  </div>
                  <div className="mt-1 flex flex-wrap items-center gap-1.5 text-[11px] leading-[16px] text-[var(--sb-text-4)]">
                    {memory.pinned && (
                      <span className="flex items-center" title={t("memory.pinned")}>
                        <Pin className="h-3 w-3" aria-hidden />
                        <span className="sr-only">{t("memory.pinned")}</span>
                      </span>
                    )}
                    <span>{timeAgo(memory.updated_at ?? memory.created_at)}</span>
                    {memory.source === "review" && (
                      <span className={badgeClass} title={t("memory.sourceReviewHint")}>
                        {t("memory.sourceReview")}
                      </span>
                    )}
                    {memory.pending && !memory.archived_at && (
                      <span className={badgeClass} title={t("memory.pendingHint")}>
                        {t("memory.pending")}
                      </span>
                    )}
                    {memory.archived_at && (
                      <span
                        className={badgeClass}
                        title={memory.superseded_by ? t("memory.supersededHint") : undefined}
                      >
                        {memory.superseded_by ? t("memory.superseded") : t("memory.archived")}
                      </span>
                    )}
                  </div>
                </>
              )}
            </div>
            {editingId !== memory.id && (
              <button
                type="button"
                className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-[var(--sb-text-3)] transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-text)] disabled:opacity-50"
                disabled={busyId === memory.id}
                onClick={(event) => setMenu({ memory, rect: event.currentTarget.getBoundingClientRect() })}
                aria-haspopup="menu"
                aria-label={t("groupMemory.more")}
              >
                <MoreHorizontal className="h-4 w-4" />
              </button>
            )}
          </li>
        ))}
      </ul>
    );
  }

  return (
    <section id={GROUP_MEMORY_SECTION_ID} aria-label={t("groupMemory.title")} data-section="group-memory">
      <div className="sb-info-label flex items-center justify-between gap-2">
        <span>{t("groupMemory.title")}</span>
        <span className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setShowArchived((prev) => !prev)}
            aria-pressed={showArchived}
            className={cn(
              "text-[11px] font-normal transition hover:text-[var(--sb-text)]",
              showArchived ? "text-[var(--sb-text)]" : "text-[var(--sb-text-4)]",
            )}
          >
            {showArchived ? t("groupMemory.hideArchived") : t("groupMemory.showArchived")}
          </button>
          <button
            type="button"
            onClick={() => setAdding(true)}
            className="flex h-5 w-5 items-center justify-center rounded text-[var(--sb-text-4)] transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-text)]"
            aria-label={t("groupMemory.add")}
            title={t("groupMemory.add")}
          >
            <Plus className="h-3.5 w-3.5" />
          </button>
        </span>
      </div>

      {adding && (
        <div className="sb-card mb-2 px-3.5 py-3">
          <textarea
            autoFocus
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            className="sb-field !min-h-[64px] !text-[12px]"
            placeholder={t("groupMemory.placeholder")}
            aria-label={t("groupMemory.add")}
          />
          <p className="mt-1.5 text-[11px] leading-[16px] text-[var(--sb-text-3)]">{t("groupMemory.privacyHint")}</p>
          <div className="mt-2 flex justify-end gap-2">
            <button
              type="button"
              className="sb-btn"
              data-variant="secondary"
              data-size="sm"
              onClick={() => {
                setAdding(false);
                setDraft("");
              }}
            >
              {t("groupMemory.cancel")}
            </button>
            <button
              type="button"
              className="sb-btn"
              data-variant="primary"
              data-size="sm"
              disabled={saving || !draft.trim()}
              onClick={() => void add()}
            >
              {saving && <Loader2 className="sb-spin h-3 w-3" />}
              {t("groupMemory.create")}
            </button>
          </div>
        </div>
      )}

      {related && related.items.length > 0 && (
        <div className="sb-card mb-2 px-3.5 py-3" role="status">
          <p className="text-[12px] leading-[18px] text-[var(--sb-text-2)]">{t("memory.related")}</p>
          <ul className="mt-2 space-y-1.5">
            {related.items.map((item) => (
              <li key={item.id} className="flex items-start gap-2">
                <span className="min-w-0 flex-1 break-words text-[12px] leading-[18px]">{item.content}</span>
                <button
                  type="button"
                  className="sb-btn shrink-0"
                  data-variant="secondary"
                  data-size="sm"
                  disabled={busyId === item.id}
                  onClick={() => void retire(item, related.newId)}
                >
                  {t("memory.retire")}
                </button>
              </li>
            ))}
          </ul>
          <div className="mt-2 flex justify-end">
            <button
              type="button"
              className="text-[11px] text-[var(--sb-text-3)] transition hover:text-[var(--sb-text)]"
              onClick={() => setRelated(null)}
            >
              {t("memory.keepAll")}
            </button>
          </div>
        </div>
      )}

      {body}
      {actionError && (
        <div className={errorClass} role="alert">
          {actionError}
        </div>
      )}

      <Menu
        open={menu !== null}
        anchor={menu ? { rect: menu.rect } : null}
        items={menu ? menuItems(menu.memory) : []}
        onClose={() => setMenu(null)}
        align="right"
      />
      {confirmElement}
    </section>
  );
}
