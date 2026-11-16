"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Bell, CheckCheck } from "lucide-react";

import { api } from "@/lib/api";
import { timeAgo } from "@/lib/format";
import { playNotificationSound } from "@/lib/notificationSound";
import type { Notification, NotificationPreferences } from "@/lib/types";

export function NotificationCenter({
  onOpenConversation,
  hideTrigger = false,
  openSignal = 0,
}: {
  onOpenConversation: (conversationID: string, messageID?: string) => void;
  hideTrigger?: boolean;
  openSignal?: number;
}) {
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<Notification[]>([]);
  const [unread, setUnread] = useState(0);
  const [loading, setLoading] = useState(false);
  const [anchor, setAnchor] = useState<DOMRect | null>(null);
  const [filter, setFilter] = useState<"all" | "unread" | "messages" | "approvals" | "agents">("all");
  const panelRef = useRef<HTMLDivElement>(null);
  const seenRef = useRef<Set<string>>(new Set());
  const firstLoadRef = useRef(true);
  const prefsRef = useRef<NotificationPreferences>({
    desktop: true,
    sound: "default",
    volume: 0.6,
    messages: true,
    approvals: true,
    agents: true,
  });

  useEffect(() => {
    let cancelled = false;
    api
      .getNotificationPreferences()
      .then((result) => {
        if (!cancelled) prefsRef.current = result.preferences;
      })
      .catch(() => undefined);
    const onPrefs = (event: Event) => {
      const detail = (event as CustomEvent<NotificationPreferences>).detail;
      if (detail) prefsRef.current = detail;
    };
    window.addEventListener("superbot:notification-prefs", onPrefs);
    return () => {
      cancelled = true;
      window.removeEventListener("superbot:notification-prefs", onPrefs);
    };
  }, []);

  const refreshCount = useCallback(async () => {
    try {
      const result = await api.unreadNotificationCount();
      setUnread(result.unread);
    } catch {
      // ignore transient polling errors
    }
  }, []);

  useEffect(() => {
    void refreshCount();
    const timer = window.setInterval(() => void refreshCount(), 20_000);
    return () => window.clearInterval(timer);
  }, [refreshCount]);

  useEffect(() => {
    if (!open) return;
    const onDown = (event: MouseEvent) => {
      if (panelRef.current?.contains(event.target as Node)) return;
      if ((event.target as HTMLElement).closest('[aria-label="通知"]')) return;
      setOpen(false);
    };
    window.addEventListener("mousedown", onDown, true);
    return () => window.removeEventListener("mousedown", onDown, true);
  }, [open]);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const result = await api.listNotifications();
      setItems(result.notifications);
      setUnread(result.unread);
      if (firstLoadRef.current) {
        for (const item of result.notifications) seenRef.current.add(item.id);
        firstLoadRef.current = false;
      } else {
        const prefs = prefsRef.current;
        for (const item of result.notifications) {
          if (seenRef.current.has(item.id)) continue;
          seenRef.current.add(item.id);
          if (item.read) continue;
          const enabled =
            item.kind === "tool_approval"
              ? prefs.approvals
              : item.kind === "subagent" || item.kind === "agent" || item.kind === "activity"
                ? prefs.agents
                : prefs.messages;
          if (!enabled) continue;
          playNotificationSound(prefs.sound, prefs.volume);
          if (prefs.desktop && window.superbotDesktop) {
            const metadata = item.metadata ?? {};
            const conversationID = typeof metadata.conversation_id === "string" ? metadata.conversation_id : "";
            const messageID = typeof metadata.message_id === "string" ? metadata.message_id : "";
            const target = conversationID
              ? `/?conv=${conversationID}${messageID ? `&message=${messageID}` : ""}`
              : undefined;
            void window.superbotDesktop.showNotification(item.title, item.body, target);
          }
        }
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    const timer = window.setInterval(() => void load(), 30_000);
    return () => window.clearInterval(timer);
  }, [load]);

  const openPanel = () => {
    if (!open) void load();
    setOpen((prev) => !prev);
  };

  useEffect(() => {
    if (!openSignal) return;
    const width = 360;
    const synthetic = {
      top: 0,
      right: window.innerWidth - 16,
      bottom: 56,
      left: window.innerWidth - width - 16,
      width,
      height: 56,
      x: window.innerWidth - width - 16,
      y: 0,
      toJSON: () => ({}),
    } as DOMRect;
    setAnchor(synthetic);
    setOpen(true);
    void load();
  }, [openSignal, load]);

  const select = async (item: Notification) => {
    setOpen(false);
    if (!item.read) {
      setItems((prev) => prev.map((entry) => (entry.id === item.id ? { ...entry, read: true } : entry)));
      setUnread((prev) => Math.max(0, prev - 1));
      void api.markNotificationRead(item.id).catch(() => undefined);
    }
    const metadata = item.metadata ?? {};
    const conversationID = typeof metadata.conversation_id === "string" ? metadata.conversation_id : "";
    const messageID = typeof metadata.message_id === "string" ? metadata.message_id : "";
    if (conversationID) onOpenConversation(conversationID, messageID || undefined);
  };

  const markAll = async () => {
    await api.markAllNotificationsRead().catch(() => undefined);
    setItems((prev) => prev.map((item) => ({ ...item, read: true })));
    setUnread(0);
  };

  return (
    <>
      {!hideTrigger && (
      <button
        type="button"
        className="sb-icon-btn relative"
        aria-label="通知"
        title="通知"
        onClick={(event) => {
          setAnchor(event.currentTarget.getBoundingClientRect());
          openPanel();
        }}
      >
        <Bell className="h-4 w-4" />
        {unread > 0 && (
          <span className="absolute -right-0.5 -top-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-[var(--sb-danger)] px-1 text-[9px] font-medium text-white">
            {unread > 99 ? "99+" : unread}
          </span>
        )}
      </button>
      )}

      {open &&
        anchor &&
        createPortal(
          <div
            ref={panelRef}
            className="fixed z-50 flex max-h-[480px] w-[360px] flex-col overflow-hidden rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] shadow-[var(--sb-shadow-popup)]"
            style={{
              top: Math.min(anchor.bottom + 8, window.innerHeight - 500),
              left: Math.max(8, Math.min(anchor.right - 360, window.innerWidth - 368)),
            }}
          >
            <div className="flex items-center justify-between border-b border-[var(--sb-border)] px-4 py-3">
              <div className="text-[14px] font-medium">通知</div>
              <button
                type="button"
                onClick={() => void markAll()}
                className="flex items-center gap-1 text-[11px] text-[var(--sb-text-2)] transition hover:text-[var(--sb-text)]"
              >
                <CheckCheck className="h-3.5 w-3.5" />
                全部已读
              </button>
            </div>
            <div className="flex items-center gap-1 border-b border-[var(--sb-border)] px-3 py-1.5">
              {(
                [
                  ["all", "全部"],
                  ["unread", "未读"],
                  ["messages", "消息"],
                  ["approvals", "审批"],
                  ["agents", "子代理"],
                ] as const
              ).map(([key, label]) => (
                <button
                  key={key}
                  type="button"
                  onClick={() => setFilter(key)}
                  className={
                    filter === key
                      ? "h-6 rounded-full bg-[var(--sb-hover)] px-2.5 text-[11px] text-[var(--sb-text)]"
                      : "h-6 rounded-full px-2.5 text-[11px] text-[var(--sb-text-3)] transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-text)]"
                  }
                >
                  {label}
                </button>
              ))}
            </div>
            <div className="flex-1 overflow-y-auto p-1.5">
              {loading ? (
                <div className="px-3 py-8 text-center text-[12px] text-[var(--sb-text-3)]">加载中…</div>
              ) : items.length === 0 ? (
                <div className="px-3 py-10 text-center text-[12px] text-[var(--sb-text-3)]">暂无通知</div>
              ) : (
                items
                  .filter((item) => {
                    if (filter === "all") return true;
                    if (filter === "unread") return !item.read;
                    const kind = (item.kind || "").toLowerCase();
                    if (filter === "approvals") return kind.includes("approval") || kind.includes("tool");
                    if (filter === "agents") return kind.includes("subagent") || kind.includes("agent");
                    return kind.includes("message") || kind.includes("slack") || kind.includes("email") || kind === "system";
                  })
                  .map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    onClick={() => void select(item)}
                    className="flex w-full items-start gap-2.5 rounded-xl px-3 py-2.5 text-left transition hover:bg-[var(--sb-hover)]"
                  >
                    <span
                      className={`mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full ${
                        item.read ? "bg-transparent" : "bg-[var(--sb-accent)]"
                      }`}
                    />
                    <span className="min-w-0 flex-1">
                      <span className="flex items-center gap-2">
                        <span className="truncate text-[13px] leading-[18px]">{item.title}</span>
                        <span className="ml-auto shrink-0 text-[10px] text-[var(--sb-text-4)]">
                          {timeAgo(item.created_at)}
                        </span>
                      </span>
                      {item.body && (
                        <span className="mt-0.5 line-clamp-2 block text-[12px] leading-[17px] text-[var(--sb-text-2)]">
                          {item.body}
                        </span>
                      )}
                    </span>
                  </button>
                ))
              )}
            </div>
          </div>,
          document.body,
        )}
    </>
  );
}
