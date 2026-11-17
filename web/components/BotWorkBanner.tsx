"use client";

import { useEffect, useState } from "react";
import { ArrowUpRight, Loader2 } from "lucide-react";

import { api } from "@/lib/api";
import type { AgentThreadDetail, Bot } from "@/lib/types";
import { BotAvatar } from "./BotAvatar";

// How far back a bot's private-message work is shown in its chat.
const RECENT_WORK_MS = 48 * 3600 * 1000;
const MAX_THREADS = 3;
// While the bot is still working, the banner re-checks this often.
const POLL_MS = 15_000;

type WorkItem = {
  detail: AgentThreadDetail;
  peer: Bot;
  working: boolean;
  summary: string;
  at: string;
};

function timeLabel(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
}

// BotWorkBanner shows, at the top of a bot's own chat, the tasks other bots
// handed it by private message. That work happens in a bot-to-bot thread, so
// without this the bot's chat stays empty while it is busy.
export function BotWorkBanner({
  bot,
  refreshKey,
  onView,
}: {
  bot: Bot;
  refreshKey: number;
  onView: (detail: AgentThreadDetail) => void;
}) {
  const [items, setItems] = useState<WorkItem[] | null>(null);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const { activities } = await api.listActivities(bot.id);
        const since = Date.now() - RECENT_WORK_MS;
        const threadIDs: string[] = [];
        for (const activity of activities) {
          const id = activity.agent_thread_id;
          if (!id || threadIDs.includes(id)) continue;
          if (new Date(activity.created_at).getTime() < since) continue;
          threadIDs.push(id);
          if (threadIDs.length >= MAX_THREADS) break;
        }
        const details = await Promise.all(
          threadIDs.map((id) => api.getAgentThread(id).catch(() => null)),
        );
        const next: WorkItem[] = [];
        for (const detail of details) {
          if (!detail) continue;
          const peer = detail.bot_a.id === bot.id ? detail.bot_b : detail.bot_a;
          const last = detail.messages.at(-1);
          if (!last) continue;
          // The peer spoke last: the task is with this bot now.
          const working = last.sender_bot_id !== bot.id;
          const task = [...detail.messages].reverse().find((message) => message.sender_bot_id === peer.id);
          const summary = (task?.content ?? last.content).replace(/\s+/g, " ").trim();
          next.push({ detail, peer, working, summary, at: last.created_at });
        }
        if (!cancelled) setItems(next);
      } catch {
        if (!cancelled) setItems([]);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [bot.id, refreshKey, tick]);

  const busy = Boolean(items?.some((item) => item.working));
  useEffect(() => {
    if (!busy) return;
    const timer = window.setInterval(() => setTick((value) => value + 1), POLL_MS);
    return () => window.clearInterval(timer);
  }, [busy]);

  if (!items || items.length === 0) return null;
  return (
    <div className="mb-4 space-y-1.5" data-bot-work>
      {items.map((item) => (
        <button
          key={item.detail.thread.id}
          type="button"
          onClick={() => onView(item.detail)}
          className="flex w-full items-center gap-2.5 rounded-xl border border-[var(--sb-border)] bg-[var(--sb-pane)] px-3 py-2 text-left transition hover:bg-[var(--sb-hover)]"
        >
          <BotAvatar
            name={item.peer.name}
            id={item.peer.id}
            size={22}
            shape={item.peer.avatar_shape}
            color={item.peer.avatar_color}
            avatar={item.peer.avatar}
          />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1.5 text-[12px] text-[var(--sb-text-2)]">
              {item.working ? (
                <>
                  <Loader2 className="sb-spin h-3 w-3 text-[var(--sb-primary)]" />
                  <span>
                    正在处理 <span className="font-medium text-[var(--sb-text)]">{item.peer.name}</span> 发来的任务
                  </span>
                </>
              ) : (
                <span>
                  已回复 <span className="font-medium text-[var(--sb-text)]">{item.peer.name}</span>
                </span>
              )}
              <span className="text-[var(--sb-text-4)]">· {timeLabel(item.at)}</span>
            </div>
            <div className="truncate text-[12px] text-[var(--sb-text-3)]">{item.summary}</div>
          </div>
          <span className="flex shrink-0 items-center gap-0.5 text-[12px] text-[var(--sb-text-2)]">
            查看过程
            <ArrowUpRight className="h-3.5 w-3.5" />
          </span>
        </button>
      ))}
    </div>
  );
}
