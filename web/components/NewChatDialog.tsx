"use client";

import Link from "next/link";
import { useState } from "react";
import { ArrowRight, Loader2, Plus, Users, X } from "lucide-react";
import { BotAvatar } from "./BotAvatar";
import { cn, formatCNY } from "@/lib/format";
import type { Bot, Team } from "@/lib/types";

export function NewChatDialog({
  open,
  bots,
  teams,
  creating,
  creatingTeam,
  onClose,
  onCreate,
  onCreateTeam,
}: {
  open: boolean;
  bots: Bot[];
  teams: Team[];
  creating: boolean;
  creatingTeam: boolean;
  onClose: () => void;
  onCreate: (botId: string) => void;
  onCreateTeam: (teamId: string) => void;
}) {
  const [tab, setTab] = useState<"direct" | "team">("direct");
  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <button
        type="button"
        className="absolute inset-0 bg-black/70 backdrop-blur-sm"
        onClick={onClose}
        aria-label="关闭"
      />
      <div className="surface relative z-10 w-full max-w-lg animate-fade-up overflow-hidden rounded-2xl shadow-[0_30px_100px_rgba(0,0,0,0.55)]">
        <div className="flex items-start justify-between border-b border-line/70 px-5 py-4">
          <div>
            <div className="font-mono text-[9px] uppercase tracking-[0.24em] text-mint">new room</div>
            <h2 className="mt-1 text-[17px] font-medium text-white">开一个新房间</h2>
            <p className="mt-1 text-[12px] text-muted">单聊一个 Bot，或召集一支团队。</p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="flex h-8 w-8 items-center justify-center rounded-lg border border-line text-muted transition hover:text-white"
            aria-label="关闭"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="flex gap-1 border-b border-line/70 px-4 pt-3">
          <Tab active={tab === "direct"} onClick={() => setTab("direct")}>
            单聊
          </Tab>
          <Tab active={tab === "team"} onClick={() => setTab("team")}>
            团队协作
          </Tab>
        </div>

        <div className="max-h-[52vh] space-y-2 overflow-y-auto p-4">
          {tab === "direct" && (
            <>
              {bots.length === 0 && (
                <EmptyState
                  title="还没有 Bot"
                  description="先创建一个 Bot，给它人设、模型和工具。"
                  href="/bots"
                  cta="去创建 Bot"
                />
              )}
              {bots.map((bot) => (
                <button
                  key={bot.id}
                  type="button"
                  disabled={creating}
                  onClick={() => onCreate(bot.id)}
                  className="group flex w-full items-center gap-3 rounded-xl border border-line/70 bg-panel-2/40 px-3.5 py-3 text-left transition hover:border-mint/30 hover:bg-mint/[0.04] disabled:opacity-50"
                >
                  <BotAvatar name={bot.name} id={bot.id} size={38} />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="text-[13px] font-medium text-white">{bot.name}</span>
                      <span className="rounded border border-line/70 px-1.5 py-0.5 font-mono text-[9px] text-muted">
                        {bot.model}
                      </span>
                    </div>
                    <div className="mt-0.5 truncate text-[11.5px] text-muted">
                      {bot.description || "未填写职责描述"}
                    </div>
                  </div>
                  {creating ? (
                    <Loader2 className="h-4 w-4 animate-spin text-mint" />
                  ) : (
                    <ArrowRight className="h-4 w-4 text-muted/50 transition group-hover:translate-x-0.5 group-hover:text-mint" />
                  )}
                </button>
              ))}
            </>
          )}

          {tab === "team" && (
            <>
              {teams.length === 0 && (
                <EmptyState
                  title="还没有团队"
                  description="把 2-6 个 Bot 组成团队，让协调者自动派活。"
                  href="/teams"
                  cta="去组建团队"
                />
              )}
              {teams.map((team) => (
                <button
                  key={team.id}
                  type="button"
                  disabled={creatingTeam || team.bots.length === 0}
                  onClick={() => onCreateTeam(team.id)}
                  className="group flex w-full items-center gap-3 rounded-xl border border-line/70 bg-panel-2/40 px-3.5 py-3 text-left transition hover:border-mint/30 hover:bg-mint/[0.04] disabled:opacity-50"
                >
                  <div className="flex -space-x-2">
                    {team.bots.slice(0, 3).map((item) => (
                      <BotAvatar
                        key={item.bot.id}
                        name={item.bot.name}
                        id={item.bot.id}
                        size={32}
                        className="ring-2 ring-panel"
                      />
                    ))}
                    {team.bots.length === 0 && (
                      <span className="flex h-8 w-8 items-center justify-center rounded-xl border border-line text-muted">
                        <Users className="h-3.5 w-3.5" />
                      </span>
                    )}
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="text-[13px] font-medium text-white">{team.name}</span>
                      <span className="rounded border border-line/70 px-1.5 py-0.5 font-mono text-[9px] text-muted">
                        {team.bots.length} bots
                      </span>
                    </div>
                    <div className="mt-0.5 truncate text-[11.5px] text-muted">
                      {team.description || "圆桌协作模式"}
                    </div>
                  </div>
                  <div className="text-right">
                    {creatingTeam ? (
                      <Loader2 className="h-4 w-4 animate-spin text-mint" />
                    ) : (
                      <ArrowRight className="h-4 w-4 text-muted/50 transition group-hover:translate-x-0.5 group-hover:text-mint" />
                    )}
                    <div className="mt-1 font-mono text-[8.5px] text-muted/40">
                      {formatCNY(0)} cost baseline
                    </div>
                  </div>
                </button>
              ))}
            </>
          )}
        </div>

        <div className="flex items-center justify-between border-t border-line/70 px-5 py-3">
          <span className="font-mono text-[9px] uppercase tracking-[0.16em] text-muted/60">
            {tab === "direct" ? "direct room · 单 Bot" : "roundtable · 多 Bot"}
          </span>
          <Link
            href={tab === "direct" ? "/bots" : "/teams"}
            className="text-[11.5px] text-muted transition hover:text-mint"
          >
            {tab === "direct" ? "管理 Bot" : "管理团队"} →
          </Link>
        </div>
      </div>
    </div>
  );
}

function Tab({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "relative px-3 pb-2.5 pt-1 text-[12px] transition",
        active ? "text-mint" : "text-muted hover:text-text",
      )}
    >
      {children}
      {active && <span className="absolute inset-x-2 bottom-0 h-[2px] rounded-full bg-mint" />}
    </button>
  );
}

function EmptyState({
  title,
  description,
  href,
  cta,
}: {
  title: string;
  description: string;
  href: string;
  cta: string;
}) {
  return (
    <div className="rounded-xl border border-dashed border-line px-5 py-8 text-center">
      <div className="text-[13px] text-text">{title}</div>
      <p className="mt-1 text-[11.5px] text-muted">{description}</p>
      <Link
        href={href}
        className="mt-4 inline-flex items-center gap-1.5 rounded-lg border border-mint/30 bg-mint/10 px-3 py-1.5 text-[11.5px] text-mint"
      >
        <Plus className="h-3.5 w-3.5" />
        {cta}
      </Link>
    </div>
  );
}
