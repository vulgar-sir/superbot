"use client";

import { useCallback, useEffect, useState } from "react";
import { FileText, Image as ImageIcon, Loader2, MoreHorizontal, Trash2 } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import type { Bot, LibraryItem, LibraryItemDetail } from "@/lib/types";
import { Markdown } from "./Markdown";
import { Modal } from "./ui/Modal";
import { Menu } from "./ui/Menu";
import { useConfirm } from "./ui/Confirm";
import { useToast } from "./ui/Toast";
import { timeAgo } from "@/lib/format";

export function BotLibrary({ bot }: { bot: Bot }) {
  const { toast } = useToast();
  const { confirm, confirmElement } = useConfirm();
  const [items, setItems] = useState<LibraryItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [detail, setDetail] = useState<LibraryItemDetail | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [menu, setMenu] = useState<{ item: LibraryItem; rect: DOMRect } | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const result = await api.listBotLibrary(bot.id);
      setItems(result.items);
      setError(null);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "加载资料库失败");
    } finally {
      setLoading(false);
    }
  }, [bot.id]);

  useEffect(() => {
    void load();
  }, [load]);

  const openItem = useCallback(
    async (item: LibraryItem) => {
      setDetailLoading(true);
      try {
        setDetail(await api.getLibraryItem(item.id));
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "加载资料失败", "danger");
      } finally {
        setDetailLoading(false);
      }
    },
    [toast],
  );

  const removeItem = useCallback(
    async (item: LibraryItem) => {
      const ok = await confirm({
        title: `删除「${item.title}」？`,
        description: "删除后无法恢复。",
        confirmLabel: "删除",
        danger: true,
      });
      if (!ok) return;
      try {
        await api.deleteLibraryItem(item.id);
        setItems((prev) => prev.filter((entry) => entry.id !== item.id));
        toast("已删除", "success");
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "删除失败", "danger");
      }
    },
    [confirm, toast],
  );

  return (
    <div>
      {loading && (
        <div className="flex justify-center py-10">
          <Loader2 className="sb-spin h-5 w-5 text-[var(--sb-text-3)]" />
        </div>
      )}
      {!loading && items.length === 0 && (
        <div className="sb-empty">
          <FileText className="h-5 w-5" />
          <div className="sb-empty-title">资料库还是空的</div>
          <div className="sb-empty-desc">
            让 Bot 把报告、页面或文档用 save_to_library 存进来。
          </div>
        </div>
      )}
      <div className="space-y-2">
        {items.map((item) => (
          <div
            key={item.id}
            className="flex items-center gap-2 rounded-xl border border-[var(--sb-border)] px-3 py-2.5 transition hover:bg-[var(--sb-hover)]"
            onContextMenu={(event) => {
              event.preventDefault();
              setMenu({ item, rect: new DOMRect(event.clientX, event.clientY, 0, 0) });
            }}
          >
            <button
              type="button"
              onClick={() => void openItem(item)}
              className="flex min-w-0 flex-1 items-center gap-3 text-left"
            >
              <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-[var(--sb-hover)] text-[var(--sb-text-2)]">
                {item.kind === "media" || item.kind === "file" ? (
                  <ImageIcon className="h-4 w-4" />
                ) : (
                  <FileText className="h-4 w-4" />
                )}
              </span>
              <span className="min-w-0">
                <span className="block truncate text-[13px] leading-[18px]">{item.title}</span>
                <span className="block text-[11px] leading-[15px] text-[var(--sb-text-3)]">
                  {item.kind} · {timeAgo(item.created_at)}
                </span>
              </span>
            </button>
            <button
              type="button"
              className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--sb-text-4)] transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-text)]"
              onClick={(event) => setMenu({ item, rect: event.currentTarget.getBoundingClientRect() })}
              aria-label={`${item.title} 更多操作`}
            >
              <MoreHorizontal className="h-4 w-4" />
            </button>
          </div>
        ))}
      </div>
      {error && (
        <div className="mt-3 rounded-lg bg-[var(--sb-danger-soft)] px-3 py-2 text-[12px] text-[var(--sb-danger)]">
          {error}
        </div>
      )}

      <Menu
        open={menu !== null}
        anchor={menu ? { rect: menu.rect } : null}
        items={
          menu
            ? [
                {
                  key: "open",
                  label: "打开",
                  icon: <FileText className="h-3.5 w-3.5" />,
                  onSelect: () => void openItem(menu.item),
                },
                {
                  key: "delete",
                  label: "删除",
                  icon: <Trash2 className="h-3.5 w-3.5" />,
                  danger: true,
                  onSelect: () => void removeItem(menu.item),
                },
              ]
            : []
        }
        onClose={() => setMenu(null)}
        align="right"
      />

      <Modal
        open={detail !== null || detailLoading}
        onClose={() => setDetail(null)}
        title={detail?.item.title ?? "加载中…"}
        subtitle={detail?.item.description || undefined}
        size="lg"
      >
        {detailLoading || !detail ? (
          <div className="flex justify-center py-16">
            <Loader2 className="sb-spin h-5 w-5 text-[var(--sb-text-3)]" />
          </div>
        ) : (
          <Markdown content={detail.content} />
        )}
      </Modal>
      {confirmElement}
    </div>
  );
}
