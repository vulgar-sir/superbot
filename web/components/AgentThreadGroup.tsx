"use client";

import { useState } from "react";
import { ChevronDown, CornerDownRight, MessagesSquare } from "lucide-react";
import { BotAvatar } from "./BotAvatar";
import { useLanguage } from "./LanguageProvider";
import type { AgentThreadSummary } from "@/lib/agentThreads";
import { cn } from "@/lib/format";
import type { Bot } from "@/lib/types";

type Translate = (key: string, vars?: Record<string, string | number>) => string;

function direction(card: AgentThreadSummary["latest"]): "sent" | "received" {
  return card.metadata?.direction === "received" ? "received" : "sent";
}

// threadLabel describes a whole exchange in one line: a lone card keeps its
// own wording, a longer exchange says who it was with, whether the peer has
// answered, and how many messages it took.
function threadLabel(thread: AgentThreadSummary, t: Translate): string {
  const name = thread.peerName || t("chat.agentThread.teammate");
  const latest = direction(thread.latest);
  if (thread.cards.length === 1) {
    return latest === "received"
      ? t("chat.agentThread.receivedFrom", { name })
      : `${t("chat.agentThread.sentTo", { name })} · ${t("chat.agentThread.waiting")}`;
  }
  const state = latest === "received" ? t("chat.agentThread.replied") : t("chat.agentThread.waiting");
  return `${t("chat.agentThread.with", { name })} · ${state} · ${t("chat.agentThread.count", { count: thread.cards.length })}`;
}

function PeerAvatar({ peer, size }: { peer?: Bot; size: number }) {
  if (!peer) return <MessagesSquare className="h-3.5 w-3.5" />;
  return (
    <BotAvatar
      name={peer.name}
      id={peer.id}
      size={size}
      shape={peer.avatar_shape}
      color={peer.avatar_color}
      avatar={peer.avatar}
    />
  );
}

// AgentThreadGroup shows bot-to-bot private messages folded: one pill per
// exchange, and one expandable row when several exchanges sit together.
export function AgentThreadGroup({
  threads,
  bots,
  bot,
  onOpenAgentThread,
}: {
  threads: AgentThreadSummary[];
  bots: Bot[];
  bot?: Bot;
  onOpenAgentThread?: (threadID: string, conversationID: string) => void;
}) {
  const { t } = useLanguage();
  const [open, setOpen] = useState(false);
  const peerOf = (thread: AgentThreadSummary) => bots.find((item) => item.id === thread.peerBotID);
  const last = threads[threads.length - 1];
  // A reply flagged for injection was handed to the bot that started the
  // thread, which then carries on by itself.
  const handedOff = direction(last.latest) === "received" && last.latest.metadata?.inject === "true";
  const handoffBot = bots.find((item) => item.id === String(last.latest.metadata?.for_bot_id ?? "")) ?? bot;
  const handoff = handedOff && (
    <div className="-mt-1 pb-1 text-center text-[11px] leading-[14px] text-[var(--sb-text-3)]">
      {t("chat.agentThread.handoff", { bot: handoffBot?.name ?? t("chat.currentBot") })}
    </div>
  );
  const openThread = (thread: AgentThreadSummary) =>
    thread.threadID && onOpenAgentThread?.(thread.threadID, thread.conversationID);

  if (threads.length === 1) {
    const thread = threads[0];
    const preview = String(thread.latest.metadata?.preview ?? "");
    return (
      <div className="flex flex-col items-center py-1" data-message-id={thread.latest.id} data-agent-thread={thread.threadID}>
        <button
          type="button"
          onClick={() => openThread(thread)}
          title={preview || undefined}
          className="sb-agent-card max-w-[520px] rounded-full px-3 hover:bg-[var(--sb-hover)]"
        >
          <PeerAvatar peer={peerOf(thread)} size={18} />
          <span className="truncate">{threadLabel(thread, t)}</span>
          <CornerDownRight className="h-3 w-3 shrink-0 opacity-50" />
        </button>
        {handoff}
      </div>
    );
  }

  const names = Array.from(new Set(threads.map((thread) => thread.peerName || t("chat.agentThread.teammate"))));
  const total = threads.reduce((sum, thread) => sum + thread.cards.length, 0);
  const peers = Array.from(new Map(threads.map((thread) => [thread.peerBotID, peerOf(thread)])).values()).slice(0, 3);
  return (
    <div className="flex flex-col items-center py-1" data-message-id={last.latest.id}>
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
        className="sb-agent-card max-w-[520px] rounded-full px-3 hover:bg-[var(--sb-hover)]"
      >
        <span className="flex -space-x-1.5">
          {peers.map((peer, index) => (
            <span key={peer?.id ?? index} className="rounded-full ring-2 ring-[var(--sb-bg)]">
              <PeerAvatar peer={peer} size={18} />
            </span>
          ))}
        </span>
        <span className="truncate">
          {t("chat.agentThread.group", { names: names.join("、"), threads: threads.length, count: total })}
        </span>
        <ChevronDown className={cn("h-3 w-3 shrink-0 opacity-60 transition-transform", open && "rotate-180")} />
      </button>
      {open && (
        <div className="sb-card mb-1 w-full max-w-[520px] overflow-hidden">
          {threads.map((thread, index) => {
            const preview = String(thread.latest.metadata?.preview ?? "");
            return (
              <button
                key={thread.threadID || thread.latest.id}
                type="button"
                onClick={() => openThread(thread)}
                data-agent-thread={thread.threadID}
                className={cn(
                  "sb-card-row w-full text-left",
                  index > 0 && "border-t border-[var(--sb-border)]",
                )}
              >
                <PeerAvatar peer={peerOf(thread)} size={22} />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[12.5px] text-[var(--sb-text)]">{threadLabel(thread, t)}</span>
                  {preview && (
                    <span className="block truncate text-[11.5px] text-[var(--sb-text-3)]">{preview}</span>
                  )}
                </span>
                <CornerDownRight className="h-3 w-3 shrink-0 text-[var(--sb-text-4)]" />
              </button>
            );
          })}
        </div>
      )}
      {handoff}
    </div>
  );
}
