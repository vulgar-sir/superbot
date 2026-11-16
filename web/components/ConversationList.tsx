"use client";

import { Coins, MessageSquarePlus, Plus, Users } from "lucide-react";
import { BotAvatar } from "./BotAvatar";
import { cn, formatCNY, timeAgo } from "@/lib/format";
import type { Bot, Conversation, Team } from "@/lib/types";

export function ConversationList({
  conversations,
  bots,
  teams,
  activeId,
  onSelect,
  onNew,
}: {
  conversations: Conversation[];
  bots: Bot[];
  teams: Team[];
  activeId: string | null;
  onSelect: (id: string) => void;
  onNew: () => void;
}) {
  const botFor = (conversation: Conversation) =>
    bots.find((bot) => bot.id === conversation.direct_bot_id);
  const teamFor = (conversation: Conversation) =>
    teams.find((team) => team.id === conversation.team_id);

  return (
    <aside className="hidden w-[288px] shrink-0 flex-col border-r border-line/70 bg-panel/35 md:flex">
      <div className="flex items-center justify-between px-4 pb-3 pt-5">
        <div>
          <div className="font-mono text-[10px] uppercase tracking-[0.22em] text-muted">rooms</div>
          <div className="mt-0.5 text-[15px] font-medium text-white">会话</div>
        </div>
        <button
          type="button"
          onClick={onNew}
          className="flex h-8 w-8 items-center justify-center rounded-lg border border-line bg-panel-2 text-muted transition hover:border-mint/35 hover:text-mint"
          aria-label="新建会话"
        >
          <Plus className="h-4 w-4" />
        </button>
      </div>

      <div className="no-scrollbar flex-1 space-y-1 overflow-y-auto px-2.5 pb-4">
        {conversations.length === 0 && (
          <div className="mx-2 mt-6 rounded-xl border border-dashed border-line px-4 py-8 text-center">
            <MessageSquarePlus className="mx-auto h-6 w-6 text-muted/60" />
            <div className="mt-3 text-[13px] text-muted">还没有会话</div>
            <div className="mt-1 text-[11px] text-muted/60">选一个 Bot 开始对话</div>
          </div>
        )}
        {conversations.map((conversation) => {
          const bot = botFor(conversation);
          const team = teamFor(conversation);
          const teamAvatar = team?.bots[0]?.bot;
          const active = conversation.id === activeId;
          return (
            <button
              key={conversation.id}
              type="button"
              onClick={() => onSelect(conversation.id)}
              className={cn(
                "group relative flex w-full items-start gap-2.5 rounded-xl px-2.5 py-2.5 text-left transition",
                active
                  ? "bg-mint/[0.07] text-white"
                  : "text-text/80 hover:bg-white/[0.035] hover:text-white",
              )}
            >
              {active && (
                <span className="absolute left-0 top-1/2 h-6 w-[2px] -translate-y-1/2 rounded-full bg-mint" />
              )}
              <BotAvatar
                name={team ? team.name : bot?.name ?? conversation.title}
                id={team ? teamAvatar?.id ?? team.id : conversation.direct_bot_id ?? conversation.id}
                size={32}
              />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate text-[12.5px] font-medium">
                    {conversation.title || bot?.name || team?.name || "未命名会话"}
                  </span>
                  <span className="ml-auto shrink-0 font-mono text-[9px] text-muted/50">
                    {timeAgo(conversation.updated_at)}
                  </span>
                </div>
                <div className="mt-1 flex items-center gap-2">
                  <span className="truncate font-mono text-[9.5px] text-muted/60">
                    {team ? (
                      <span className="inline-flex items-center gap-1">
                        <Users className="h-2.5 w-2.5" />
                        team · {team.bots.length} bots
                      </span>
                    ) : (
                      bot?.model ?? "direct"
                    )}
                  </span>
                  <span className="ml-auto inline-flex items-center gap-0.5 font-mono text-[9px] text-muted/50">
                    <Coins className="h-2.5 w-2.5" />
                    {formatCNY(conversation.spent_micros)}
                  </span>
                </div>
              </div>
            </button>
          );
        })}
      </div>

      <div className="border-t border-line/60 px-4 py-3">
        <button
          type="button"
          onClick={onNew}
          className="flex w-full items-center justify-center gap-2 rounded-xl border border-mint/25 bg-mint/[0.06] py-2 text-[11.5px] font-medium text-mint transition hover:bg-mint/[0.1]"
        >
          <Plus className="h-3.5 w-3.5" />
          新建会话
        </button>
      </div>
    </aside>
  );
}
