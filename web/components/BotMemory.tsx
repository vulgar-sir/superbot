"use client";

import { useCallback, useEffect, useState } from "react";
import { Brain, Loader2, Pencil, Pin, PinOff, Trash2 } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import type { Bot, Memory, MemoryImportResult } from "@/lib/types";
import { cn, timeAgo } from "@/lib/format";
import { useToast } from "./ui/Toast";
import { useConfirm } from "./ui/Confirm";
import { useLanguage } from "./LanguageProvider";

export function BotMemory({ bot }: { bot: Bot }) {
  const { toast } = useToast();
  const { t } = useLanguage();
  const { confirm, confirmElement } = useConfirm();
  const [editingID, setEditingID] = useState<string | null>(null);
  const [editContent, setEditContent] = useState("");
  const [busyID, setBusyID] = useState<string | null>(null);
  const [sort, setSort] = useState<"new" | "old">("new");
  const [scope, setScope] = useState<"bot" | "user">("bot");
  const [memories, setMemories] = useState<Memory[]>([]);
  const [content, setContent] = useState("");
  const [loading, setLoading] = useState(true);
  const [importing, setImporting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [showArchived, setShowArchived] = useState(false);
  const [tidying, setTidying] = useState(false);
  // Result line of the last tidy / import / de-duplicated save.
  const [notice, setNotice] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const result =
        scope === "bot"
          ? await api.listBotMemories(bot.id, showArchived)
          : await api.listUserMemories(showArchived);
      setMemories(result.memories);
      setError(null);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "加载记忆失败");
    } finally {
      setLoading(false);
    }
  }, [bot.id, scope, showArchived]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    setNotice(null);
    setError(null);
  }, [bot.id, scope]);

  // "Imported 3 · 1 already known · 1 refused as a secret"
  const importSummary = useCallback(
    (result: MemoryImportResult) => {
      const parts = [t("memory.imported", { n: result.imported })];
      if (result.duplicates) parts.push(t("memory.importDuplicates", { n: result.duplicates }));
      if (result.rejected) parts.push(t("memory.importRejected", { n: result.rejected }));
      return parts.join(t("memory.listSeparator"));
    },
    [t],
  );

  const mergeSaved = useCallback((saved: Memory) => {
    setMemories((prev) =>
      prev.some((item) => item.id === saved.id)
        ? prev.map((item) => (item.id === saved.id ? saved : item))
        : [saved, ...prev],
    );
  }, []);

  const togglePin = useCallback(
    async (memory: Memory) => {
      const pinned = !memory.pinned;
      setBusyID(memory.id);
      try {
        const result =
          scope === "bot"
            ? await api.pinBotMemory(bot.id, memory.id, pinned)
            : await api.pinUserMemory(memory.id, pinned);
        setMemories((prev) => prev.map((item) => (item.id === memory.id ? result.memory : item)));
      } catch (err) {
        toast(err instanceof ApiError ? err.message : t("memory.pinFailed"), "danger");
      } finally {
        setBusyID(null);
      }
    },
    [bot.id, scope, t, toast],
  );

  const restoreMemory = useCallback(
    async (memory: Memory) => {
      setBusyID(memory.id);
      try {
        const result =
          scope === "bot"
            ? await api.restoreBotMemory(bot.id, memory.id)
            : await api.restoreUserMemory(memory.id);
        setMemories((prev) => prev.map((item) => (item.id === memory.id ? result.memory : item)));
        toast(t("memory.restored"), "success");
      } catch (err) {
        toast(err instanceof ApiError ? err.message : t("memory.restoreFailed"), "danger");
      } finally {
        setBusyID(null);
      }
    },
    [bot.id, scope, t, toast],
  );

  const tidyMemories = useCallback(async () => {
    setTidying(true);
    setError(null);
    setNotice(null);
    try {
      const result =
        scope === "bot" ? await api.tidyBotMemories(bot.id) : await api.tidyUserMemories();
      const archived = result.report?.archived?.length ?? 0;
      setNotice(archived > 0 ? t("memory.tidyArchived", { n: archived }) : t("memory.tidyNone"));
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t("memory.tidyFailed"));
    } finally {
      setTidying(false);
    }
  }, [bot.id, load, scope, t]);

  const importMemories = useCallback(async () => {
    if (!content.trim()) return;
    setImporting(true);
    setError(null);
    setNotice(null);
    try {
      const result = await api.importBotMemories(bot.id, content);
      setMemories((prev) => [...result.memories, ...prev]);
      setContent("");
      setNotice(importSummary(result));
      toast(t("memory.imported", { n: result.imported }), result.imported > 0 ? "success" : "default");
    } catch (err) {
      const message = err instanceof ApiError ? err.message : "导入失败";
      setError(message);
      toast(message, "danger");
    } finally {
      setImporting(false);
    }
  }, [bot.id, content, importSummary, t, toast]);

  const saveEdit = useCallback(
    async (memoryID: string) => {
      const content = editContent.trim();
      if (!content) return;
      setBusyID(memoryID);
      try {
        const result =
          scope === "bot"
            ? await api.updateBotMemory(bot.id, memoryID, content)
            : await api.updateUserMemory(memoryID, content);
        setMemories((prev) => prev.map((item) => (item.id === memoryID ? result.memory : item)));
        setEditingID(null);
        toast("记忆已更新", "success");
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "更新失败", "danger");
      } finally {
        setBusyID(null);
      }
    },
    [bot.id, editContent, scope, toast],
  );

  const removeMemory = useCallback(
    async (memory: Memory) => {
      const ok = await confirm({
        title: "删除这条记忆？",
        description: memory.content.slice(0, 80),
        confirmLabel: "删除",
        danger: true,
      });
      if (!ok) return;
      setBusyID(memory.id);
      try {
        if (scope === "bot") {
          await api.deleteBotMemory(bot.id, memory.id);
        } else {
          await api.deleteUserMemory(memory.id);
        }
        setMemories((prev) => prev.filter((item) => item.id !== memory.id));
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "删除失败", "danger");
      } finally {
        setBusyID(null);
      }
    },
    [bot.id, confirm, scope, toast],
  );

  // Archived rows go last, pinned rows first, then by the chosen date order.
  const sortedMemories = [...memories].sort((a, b) => {
    const archived = Number(Boolean(a.archived_at)) - Number(Boolean(b.archived_at));
    if (archived !== 0) return archived;
    const pinned = Number(Boolean(b.pinned)) - Number(Boolean(a.pinned));
    if (pinned !== 0) return pinned;
    const delta = new Date(a.created_at).getTime() - new Date(b.created_at).getTime();
    return sort === "new" ? -delta : delta;
  });
  const activeCount = memories.filter((item) => !item.archived_at).length;
  const archivedCount = memories.length - activeCount;

  return (
    <div>
      <div className="mb-3 flex items-center gap-1">
        {(
          [
            ["bot", bot.name],
            ["user", "我的记忆"],
          ] as const
        ).map(([key, label]) => (
          <button
            key={key}
            type="button"
            onClick={() => setScope(key)}
            className={
              scope === key
                ? "h-7 rounded-full bg-[var(--sb-hover)] px-3 text-[12px] text-[var(--sb-text)]"
                : "h-7 rounded-full px-3 text-[12px] text-[var(--sb-text-2)] transition hover:bg-[var(--sb-hover)]"
            }
          >
            {label}
          </button>
        ))}
      </div>
      {scope === "user" ? (
        <div className="rounded-xl border border-[var(--sb-border)] p-3.5">
          <div className="flex items-center gap-2 text-[13px] font-medium">
            <Brain className="h-4 w-4 text-[var(--sb-text-2)]" />
            新增我的记忆
          </div>
          <p className="mt-1 text-[12px] leading-[18px] text-[var(--sb-text-2)]">
            用户级记忆会在这个账户的所有 Bot 中召回，适合长期偏好与个人信息。
          </p>
          <textarea
            value={content}
            onChange={(event) => setContent(event.target.value)}
            className="sb-field mt-3 !min-h-[72px] font-[var(--sb-mono)] !text-[12px]"
            placeholder="例如：偏好简洁的中文回复"
          />
          <div className="mt-2 flex items-center justify-between gap-2">
            <label className="cursor-pointer text-[11.5px] text-[var(--sb-text-3)] transition hover:text-[var(--sb-text)]">
              从文件导入（.json / .txt / .md）
              <input
                type="file"
                accept=".json,.txt,.md,application/json,text/plain"
                className="hidden"
                onChange={async (event) => {
                  const file = event.target.files?.[0];
                  event.target.value = "";
                  if (!file) return;
                  setBusyID("create");
                  setError(null);
                  setNotice(null);
                  try {
                    const text = await file.text();
                    const result = await api.importUserMemories(text);
                    setMemories((prev) => [...result.memories, ...prev]);
                    setNotice(importSummary(result));
                    toast(
                      t("memory.imported", { n: result.imported }),
                      result.imported > 0 ? "success" : "default",
                    );
                  } catch (err) {
                    const message = err instanceof ApiError ? err.message : "导入失败";
                    setError(message);
                    toast(message, "danger");
                  } finally {
                    setBusyID(null);
                  }
                }}
              />
            </label>
            <button
              type="button"
              disabled={!content.trim() || busyID === "create"}
              onClick={async () => {
                setBusyID("create");
                setError(null);
                setNotice(null);
                try {
                  const created = await api.createUserMemory(content.trim());
                  mergeSaved(created.memory);
                  setContent("");
                  if (created.duplicate) {
                    // Nothing new was stored: say so instead of "added".
                    setNotice(t("memory.duplicate"));
                  } else {
                    toast("记忆已添加", "success");
                  }
                } catch (err) {
                  // The server explains refusals (e.g. content that looks
                  // like a credential); show its words next to the form.
                  setError(err instanceof ApiError ? err.message : "添加失败");
                } finally {
                  setBusyID(null);
                }
              }}
              className="sb-btn"
              data-variant="primary"
              data-size="sm"
            >
              添加
            </button>
          </div>
        </div>
      ) : (
      <div className="rounded-xl border border-[var(--sb-border)] p-3.5">
        <div className="flex items-center gap-2 text-[13px] font-medium">
          <Brain className="h-4 w-4 text-[var(--sb-text-2)]" />
          导入记忆
        </div>
        <p className="mt-1 text-[12px] leading-[18px] text-[var(--sb-text-2)]">
          粘贴另一个 AI 的记忆或偏好，每行一条；导入后会成为 {bot.name} 的长期记忆。
        </p>
        <textarea
          value={content}
          onChange={(event) => setContent(event.target.value)}
          className="sb-field mt-3 !min-h-[100px] font-[var(--sb-mono)] !text-[12px]"
          placeholder={"- 用户偏好简体中文\n- 项目在 /workspace/projects/foo\n- 不要使用某个库"}
        />
        <div className="mt-2 flex items-center justify-between gap-3">
          <span className="text-[11px] text-[var(--sb-text-4)]">最多 100 条 / 每行 4000 字符</span>
          <button
            type="button"
            onClick={() => void importMemories()}
            disabled={importing || !content.trim()}
            className="sb-btn"
            data-variant="primary"
            data-size="sm"
          >
            {importing && <Loader2 className="sb-spin h-3 w-3" />}
            导入
          </button>
        </div>
      </div>
      )}

      {error && (
        <div
          className="mt-2 break-words rounded-lg bg-[var(--sb-danger-soft)] px-3 py-2 text-[12px] leading-[18px] text-[var(--sb-danger)]"
          role="alert"
        >
          {error}
        </div>
      )}
      {notice && (
        <div
          className="mt-2 break-words rounded-lg bg-[var(--sb-hover)] px-3 py-2 text-[12px] leading-[18px] text-[var(--sb-text-2)]"
          role="status"
        >
          {notice}
        </div>
      )}

      <div className="mt-4">
        <div className="mb-2 flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
          <span className="text-[12px] text-[var(--sb-text-2)]">当前记忆</span>
          <div className="flex flex-wrap items-center justify-end gap-x-3 gap-y-1">
            <button
              type="button"
              onClick={() => void tidyMemories()}
              disabled={tidying || loading || activeCount < 2}
              title={t("memory.tidyHint")}
              className="flex items-center gap-1 text-[11px] text-[var(--sb-text-3)] transition hover:text-[var(--sb-text)] disabled:opacity-50 disabled:hover:text-[var(--sb-text-3)]"
            >
              {tidying && <Loader2 className="sb-spin h-3 w-3" />}
              {t("memory.tidy")}
            </button>
            <button
              type="button"
              onClick={() => setShowArchived((prev) => !prev)}
              aria-pressed={showArchived}
              className={cn(
                "text-[11px] transition hover:text-[var(--sb-text)]",
                showArchived ? "text-[var(--sb-text)]" : "text-[var(--sb-text-3)]",
              )}
            >
              {showArchived ? t("memory.hideArchived") : t("memory.showArchived")}
            </button>
            <button
              type="button"
              onClick={() => setSort((prev) => (prev === "new" ? "old" : "new"))}
              className="text-[11px] text-[var(--sb-text-3)] transition hover:text-[var(--sb-text)]"
            >
              {sort === "new" ? "最新在前" : "最早在前"}
            </button>
            <span
              className="text-[11px] tabular-nums text-[var(--sb-text-4)]"
              title={archivedCount > 0 ? t("memory.archivedCount", { n: archivedCount }) : undefined}
            >
              {activeCount}
              {archivedCount > 0 && ` + ${archivedCount}`}
            </span>
          </div>
        </div>
        {loading ? (
          <div className="flex justify-center py-6">
            <Loader2 className="sb-spin h-4 w-4 text-[var(--sb-text-3)]" />
          </div>
        ) : memories.length === 0 ? (
          <div className="sb-empty">
            <Brain className="h-5 w-5" />
            <div className="sb-empty-title">还没有记忆</div>
            <div className="sb-empty-desc">Bot 会在对话中用 memory_save 写入长期记忆。</div>
          </div>
        ) : (
          <div className="max-h-[280px] space-y-1.5 overflow-y-auto">
            {sortedMemories.map((memory) => (
              <div
                key={memory.id}
                className="rounded-lg border border-[var(--sb-border)] px-3 py-2.5"
                data-archived={memory.archived_at ? "true" : undefined}
              >
                {editingID === memory.id ? (
                  <div>
                    <textarea
                      autoFocus
                      value={editContent}
                      onChange={(event) => setEditContent(event.target.value)}
                      className="sb-field !min-h-[72px] font-[var(--sb-mono)] !text-[12px]"
                    />
                    <div className="mt-1.5 flex justify-end gap-2">
                      <button
                        type="button"
                        onClick={() => setEditingID(null)}
                        className="text-[11px] text-[var(--sb-text-3)] hover:text-[var(--sb-text)]"
                      >
                        取消
                      </button>
                      <button
                        type="button"
                        disabled={busyID === memory.id || !editContent.trim()}
                        onClick={() => void saveEdit(memory.id)}
                        className="text-[11px] font-medium text-[var(--sb-text)] disabled:opacity-50"
                      >
                        {busyID === memory.id ? "保存中…" : "保存"}
                      </button>
                    </div>
                  </div>
                ) : (
                  <>
                    <div
                      className={cn(
                        "break-words text-[12px] leading-[18px]",
                        memory.archived_at ? "text-[var(--sb-text-3)]" : "text-[var(--sb-text)]",
                      )}
                    >
                      {memory.content}
                    </div>
                    <div className="mt-1 flex items-center justify-between text-[10px] text-[var(--sb-text-4)]">
                      <span className="flex min-w-0 items-center gap-1.5">
                        {memory.pinned && (
                          <span className="flex shrink-0 items-center" title={t("memory.pinned")}>
                            <Pin className="h-3 w-3" aria-hidden />
                            <span className="sr-only">{t("memory.pinned")}</span>
                          </span>
                        )}
                        <span>
                          {memory.scope_type} · {timeAgo(memory.created_at)}
                        </span>
                        {memory.source === "review" && (
                          <span
                            className="shrink-0 rounded-full bg-[var(--sb-hover)] px-1.5 py-px text-[var(--sb-text-3)]"
                            title={t("memory.sourceReviewHint")}
                          >
                            {t("memory.sourceReview")}
                          </span>
                        )}
                        {(memory.source === "tidy" || memory.source === "import") && (
                          <span className="shrink-0 rounded-full bg-[var(--sb-hover)] px-1.5 py-px text-[var(--sb-text-3)]">
                            {memory.source === "tidy" ? t("memory.sourceTidy") : t("memory.sourceImport")}
                          </span>
                        )}
                        {memory.archived_at && (
                          <span className="shrink-0 rounded-full bg-[var(--sb-hover)] px-1.5 py-px text-[var(--sb-text-3)]">
                            {t("memory.archived")}
                          </span>
                        )}
                      </span>
                      <span className="flex shrink-0 items-center gap-2 pl-2">
                        {memory.archived_at ? (
                          <button
                            type="button"
                            disabled={busyID === memory.id}
                            onClick={() => void restoreMemory(memory)}
                            className="text-[11px] text-[var(--sb-text-2)] transition hover:text-[var(--sb-text)] disabled:opacity-50"
                          >
                            {t("memory.restore")}
                          </button>
                        ) : (
                          <button
                            type="button"
                            disabled={busyID === memory.id}
                            onClick={() => void togglePin(memory)}
                            aria-label={memory.pinned ? t("memory.unpin") : t("memory.pin")}
                            title={memory.pinned ? t("memory.unpin") : t("memory.pin")}
                            aria-pressed={Boolean(memory.pinned)}
                            className="transition hover:text-[var(--sb-text)] disabled:opacity-50"
                          >
                            {memory.pinned ? <PinOff className="h-3 w-3" /> : <Pin className="h-3 w-3" />}
                          </button>
                        )}
                        <button
                          type="button"
                          onClick={() => {
                            setEditingID(memory.id);
                            setEditContent(memory.content);
                          }}
                          aria-label="编辑记忆"
                          className="transition hover:text-[var(--sb-text)]"
                        >
                          <Pencil className="h-3 w-3" />
                        </button>
                        <button
                          type="button"
                          onClick={() => void removeMemory(memory)}
                          aria-label="删除记忆"
                          className="transition hover:text-[var(--sb-danger)]"
                        >
                          <Trash2 className="h-3 w-3" />
                        </button>
                      </span>
                    </div>
                  </>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
      {confirmElement}
    </div>
  );
}
