"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Activity as ActivityIcon, ArrowUpRight, Bot as BotIcon, CheckSquare, Loader2, MessageSquare, Plug, Search, Settings, Users } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import type { Activity, Bot } from "@/lib/types";
import { BotAvatar } from "./BotAvatar";
import { timeAgo } from "@/lib/format";

type Command = {
  id: string;
  label: string;
  description: string;
  href: string;
  keywords: string;
  icon: React.ReactNode;
};

type MessageHit = {
  id: string;
  conversation_id: string;
  conversation_title: string;
  content: string;
  sender_type: string;
  bot_name: string;
  created_at: string;
};

type Result =
  | { kind: "bot"; bot: Bot }
  | { kind: "message"; message: MessageHit }
  | { kind: "activity"; activity: Activity; bot?: Bot }
  | { kind: "command"; command: Command };

const COMMANDS: Command[] = [
  {
    id: "settings",
    label: "设置",
    description: "账户、模型、保险库与用量",
    href: "/settings",
    keywords: "settings shezhi 设置 账户 模型",
    icon: <Settings className="h-4 w-4" />,
  },
  {
    id: "bots",
    label: "Bot 管理",
    description: "查看和编辑所有 Bot",
    href: "/bots",
    keywords: "bots bot 机器人 管理",
    icon: <BotIcon className="h-4 w-4" />,
  },
  {
    id: "teams",
    label: "群聊",
    description: "创建和管理多 Bot 群聊",
    href: "/teams",
    keywords: "group team 群聊 团队",
    icon: <Users className="h-4 w-4" />,
  },
  {
    id: "tasks",
    label: "任务",
    description: "任务板与 Bot 分配",
    href: "/tasks",
    keywords: "task tasks 任务 项目 看板",
    icon: <CheckSquare className="h-4 w-4" />,
  },
  {
    id: "activity",
    label: "动态",
    description: "Bot 的主动更新与未读",
    href: "/activity",
    keywords: "activity 动态 通知",
    icon: <ActivityIcon className="h-4 w-4" />,
  },
  {
    id: "extensions",
    label: "连接应用",
    description: "技能、MCP 与第三方连接器",
    href: "/extensions",
    keywords: "connector skill mcp 连接 应用 技能",
    icon: <Plug className="h-4 w-4" />,
  },
];

export function SearchDialog({
  open,
  initialQuery,
  bots,
  onClose,
  onOpenBot,
  onOpenMessage,
  onNavigate,
}: {
  open: boolean;
  initialQuery: string;
  bots: Bot[];
  onClose: () => void;
  onOpenBot: (bot: Bot) => void;
  onOpenMessage?: (conversationID: string, messageID: string) => void;
  onNavigate?: (href: string) => void;
}) {
  const [query, setQuery] = useState(initialQuery);
  const [activities, setActivities] = useState<Activity[]>([]);
  const [matchedBots, setMatchedBots] = useState<Bot[]>([]);
  const [matchedMessages, setMatchedMessages] = useState<MessageHit[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [highlight, setHighlight] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (open) {
      setQuery(initialQuery);
      setHighlight(0);
      window.setTimeout(() => inputRef.current?.focus(), 30);
    }
  }, [open, initialQuery]);

  useEffect(() => {
    if (!open) return;
    const trimmed = query.trim();
    if (!trimmed) {
      setActivities([]);
      setMatchedBots([]);
      setMatchedMessages([]);
      setError(null);
      return;
    }
    let cancelled = false;
    setLoading(true);
    const timer = window.setTimeout(async () => {
      try {
        const result = await api.search(trimmed);
        if (cancelled) return;
        setActivities(result.activities);
        setMatchedBots(result.bots);
        setMatchedMessages(result.messages ?? []);
        setError(null);
        setHighlight(0);
      } catch (err) {
        if (!cancelled) setError(err instanceof ApiError ? err.message : "搜索失败");
      } finally {
        if (!cancelled) setLoading(false);
      }
    }, 180);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [open, query]);

  const commands = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return COMMANDS;
    return COMMANDS.filter((command) =>
      `${command.label} ${command.description} ${command.keywords}`.toLowerCase().includes(needle),
    );
  }, [query]);

  const results = useMemo<Result[]>(() => {
    const entries: Result[] = commands.map((command) => ({ kind: "command", command }));
    for (const bot of matchedBots) entries.push({ kind: "bot", bot });
    for (const message of matchedMessages) entries.push({ kind: "message", message });
    for (const activity of activities) {
      entries.push({
        kind: "activity",
        activity,
        bot: bots.find((bot) => bot.id === activity.bot_id),
      });
    }
    return entries;
  }, [activities, bots, commands, matchedBots, matchedMessages]);

  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        onClose();
      } else if (event.key === "ArrowDown") {
        event.preventDefault();
        setHighlight((prev) => (results.length === 0 ? 0 : (prev + 1) % results.length));
      } else if (event.key === "ArrowUp") {
        event.preventDefault();
        setHighlight((prev) => (results.length === 0 ? 0 : (prev - 1 + results.length) % results.length));
      } else if (event.key === "Enter") {
        const entry = results[highlight];
        if (!entry) return;
        event.preventDefault();
        onClose();
        if (entry.kind === "bot") onOpenBot(entry.bot);
        else if (entry.kind === "command") onNavigate?.(entry.command.href);
        else if (entry.kind === "message") {
          if (onOpenMessage) {
            onOpenMessage(entry.message.conversation_id, entry.message.id);
          } else {
            onNavigate?.(
              `/?conv=${entry.message.conversation_id}&message=${entry.message.id}`,
            );
          }
        } else if (entry.bot) onOpenBot(entry.bot);
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [open, results, highlight, onClose, onOpenBot, onOpenMessage, onNavigate]);

  if (!open || typeof document === "undefined") return null;

  return createPortal(
    <div
      className="sb-overlay !items-start pt-[13vh]"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div
        className="relative z-10 flex max-h-[70vh] w-full max-w-[640px] flex-col overflow-hidden rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] shadow-[var(--sb-shadow-modal)]"
        style={{ animation: "sb-modal-in 0.16s cubic-bezier(0.16,1,0.3,1) both" }}
        role="dialog"
        aria-modal="true"
      >
        <div className="flex items-center gap-3 px-5 py-4">
          <Search className="h-5 w-5 shrink-0 text-[var(--sb-text-2)]" />
          <input
            ref={inputRef}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="搜索 Bot、聊天、设置…"
            className="sb-search-input min-w-0 flex-1 bg-transparent text-[20px] leading-[28px] text-[var(--sb-text)] outline-none placeholder:text-[var(--sb-text-4)]"
          />
          {loading && <Loader2 className="sb-spin h-4 w-4 text-[var(--sb-text-3)]" />}
        </div>
        <div className="h-px bg-[var(--sb-border)]" />
        <div className="min-h-[160px] flex-1 overflow-y-auto p-2">
          {error && (
            <div className="m-2 rounded-lg bg-[var(--sb-danger-soft)] px-3 py-2 text-[12px] text-[var(--sb-danger)]">
              {error}
            </div>
          )}
          {query.trim() && results.length === 0 && !loading && !error && (
            <div className="px-4 py-12 text-center text-[13px] text-[var(--sb-text-2)]">没有找到匹配内容。</div>
          )}
          {commands.length > 0 && (
            <div className="mb-1">
              <div className="px-3 pb-1 pt-2 text-[12px] text-[var(--sb-text-4)]">
                {query.trim() ? "快捷入口" : "快捷指令"}
              </div>
              {commands.map((command) => {
                const index = results.findIndex(
                  (entry) => entry.kind === "command" && entry.command.id === command.id,
                );
                return (
                  <button
                    key={command.id}
                    type="button"
                    onMouseEnter={() => setHighlight(index)}
                    onClick={() => {
                      onClose();
                      onNavigate?.(command.href);
                    }}
                    className="flex w-full items-center gap-3 rounded-xl px-3 py-4 text-left transition hover:bg-[var(--sb-hover)]"
                    style={highlight === index ? { background: "var(--sb-hover)" } : undefined}
                  >
                    <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-[var(--sb-hover)] text-[var(--sb-text-2)]">
                      {command.icon}
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-[15px] leading-[21px]">{command.label}</span>
                      <span className="block truncate text-[13px] leading-[17px] text-[var(--sb-text-2)]">
                        {command.description}
                      </span>
                    </span>
                  </button>
                );
              })}
            </div>
          )}
          {matchedBots.length > 0 && (
            <div className="mb-1">
              <div className="px-3 pb-1 pt-2 text-[12px] text-[var(--sb-text-4)]">Bot</div>
              {matchedBots.map((bot) => {
                const index = results.findIndex((entry) => entry.kind === "bot" && entry.bot.id === bot.id);
                return (
                  <button
                    key={bot.id}
                    type="button"
                    onMouseEnter={() => setHighlight(index)}
                    onClick={() => {
                      onClose();
                      onOpenBot(bot);
                    }}
                    className="flex w-full items-center gap-3 rounded-xl px-3 py-4 text-left transition hover:bg-[var(--sb-hover)]"
                    style={highlight === index ? { background: "var(--sb-hover)" } : undefined}
                  >
                    <BotAvatar
                      name={bot.name}
                      id={bot.id}
                      size={32}
                      shape={bot.avatar_shape}
                      color={bot.avatar_color}
                    />
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-[15px] leading-[21px]">{bot.name}</div>
                      <div className="truncate text-[13px] leading-[17px] text-[var(--sb-text-2)]">
                        {bot.latest_activity?.summary || bot.title || bot.description || bot.model}
                      </div>
                    </div>
                    {(bot.unread_count ?? 0) > 0 && <span className="sb-unread-dot" />}
                  </button>
                );
              })}
            </div>
          )}
          {matchedMessages.length > 0 && (
            <div className="mb-1">
              <div className="px-3 pb-1 pt-2 text-[12px] text-[var(--sb-text-4)]">聊天</div>
              {matchedMessages.map((message) => {
                const index = results.findIndex(
                  (entry) => entry.kind === "message" && entry.message.id === message.id,
                );
                return (
                  <button
                    key={message.id}
                    type="button"
                    onMouseEnter={() => setHighlight(index)}
                    onClick={() => {
                      onClose();
                      if (onOpenMessage) {
                        onOpenMessage(message.conversation_id, message.id);
                      } else {
                        onNavigate?.(
                          `/?conv=${message.conversation_id}&message=${message.id}`,
                        );
                      }
                    }}
                    className="flex w-full items-start gap-3 rounded-xl px-3 py-3 text-left transition hover:bg-[var(--sb-hover)]"
                    style={highlight === index ? { background: "var(--sb-hover)" } : undefined}
                  >
                    <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-[var(--sb-hover)] text-[var(--sb-text-2)]">
                      <MessageSquare className="h-4 w-4" />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="flex items-center gap-2">
                        <span className="truncate text-[14px] leading-[19px]">
                          {message.conversation_title || message.bot_name || "对话"}
                        </span>
                        <span className="ml-auto shrink-0 text-[11px] text-[var(--sb-text-4)]">
                          {timeAgo(message.created_at)}
                        </span>
                      </span>
                      <span className="mt-0.5 line-clamp-2 text-[13px] leading-[18px] text-[var(--sb-text-2)]">
                        {message.content}
                      </span>
                    </span>
                  </button>
                );
              })}
            </div>
          )}
          {activities.length > 0 && (
            <div>
              <div className="px-3 pb-1 pt-2 text-[12px] text-[var(--sb-text-4)]">动态</div>
              {activities.map((activity) => {
                const index = results.findIndex(
                  (entry) => entry.kind === "activity" && entry.activity.id === activity.id,
                );
                const matched = bots.find((bot) => bot.id === activity.bot_id);
                return (
                  <button
                    key={activity.id}
                    type="button"
                    onMouseEnter={() => setHighlight(index)}
                    onClick={() => {
                      const bot = matched;
                      if (!bot) return;
                      onClose();
                      onOpenBot(bot);
                    }}
                    className="flex w-full items-start gap-3 rounded-xl px-3 py-4 text-left transition hover:bg-[var(--sb-hover)]"
                    style={highlight === index ? { background: "var(--sb-hover)" } : undefined}
                  >
                    <BotAvatar
                      name={activity.bot_name}
                      id={activity.bot_id}
                      size={30}
                      shape={matched?.avatar_shape}
                      color={matched?.avatar_color}
                    />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="truncate text-[14px] leading-[19px]">{activity.bot_name}</span>
                        <span className="ml-auto shrink-0 text-[11px] text-[var(--sb-text-4)]">
                          {timeAgo(activity.created_at)}
                        </span>
                      </div>
                      <div className="mt-0.5 line-clamp-2 text-[13px] leading-[18px] text-[var(--sb-text-2)]">
                        {activity.summary}
                      </div>
                    </div>
                    <ArrowUpRight className="mt-1 h-3.5 w-3.5 shrink-0 text-[var(--sb-text-4)]" />
                  </button>
                );
              })}
            </div>
          )}
        </div>
      </div>
    </div>,
    document.body,
  );
}
