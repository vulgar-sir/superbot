"use client";

import { Activity, Bot as BotIcon, Brain, Coins, Cpu, Hash, Laptop, Radio, Users, Wrench } from "lucide-react";
import { BotAvatar } from "./BotAvatar";
import { StatusPill } from "./StatusPill";
import { ToolCard } from "./ToolCard";
import { BotRoutineList } from "./BotRoutineList";
import { formatCNY, formatNumber } from "@/lib/format";
import type { Bot, Conversation, RouteDecision, StreamState, Team, ToolActivity } from "@/lib/types";

export function RunPanel({
  bot,
  team,
  conversation,
  stream,
  tools,
  routes,
  cycleActive,
  usage,
}: {
  bot?: Bot;
  team?: Team;
  conversation: Conversation | null;
  stream?: StreamState;
  tools: ToolActivity[];
  routes: RouteDecision[];
  cycleActive: boolean;
  usage: { input: number; output: number; cost: number } | null;
}) {
  const toolCount = tools.length;
  const speaker = stream ? team?.bots.find((item) => item.bot.id === stream.botId)?.bot : undefined;
  const identityName = team ? team.name : bot?.name ?? "等待选择 Bot";
  const identityID = team ? team.bots[0]?.bot.id ?? team.id : bot?.id ?? "unknown";
  const identityModel = team ? `${team.mode} · ${team.bots.length} bots` : bot?.model ?? "—";
  return (
    <aside className="hidden w-[332px] shrink-0 flex-col border-l border-line/70 bg-panel/30 lg:flex">
      <div className="flex items-center justify-between border-b border-line/60 px-4 py-4">
        <div>
          <div className="font-mono text-[9px] uppercase tracking-[0.22em] text-muted">telemetry</div>
          <div className="mt-0.5 text-[14px] font-medium text-white">运行监控</div>
        </div>
        <StatusPill tone={cycleActive ? "live" : "idle"} pulse={cycleActive}>
          {cycleActive ? "running" : "idle"}
        </StatusPill>
      </div>

      <div className="no-scrollbar flex-1 space-y-4 overflow-y-auto p-4">
        <div className="rounded-2xl border border-line/80 bg-panel-2/45 p-4">
          <div className="flex items-center gap-3">
            <BotAvatar name={identityName} id={identityID} size={42} />
            <div className="min-w-0">
              <div className="truncate text-[14px] font-medium text-white">{identityName}</div>
              <div className="mt-0.5 flex items-center gap-1.5 font-mono text-[10px] text-muted">
                {team ? <Users className="h-3 w-3" /> : <Cpu className="h-3 w-3" />}
                {identityModel}
              </div>
            </div>
          </div>
          <div className="mt-3 grid grid-cols-2 gap-2">
            <Metric icon={<Hash className="h-3 w-3" />} label="会话" value={conversation ? conversation.kind : "—"} />
            <Metric
              icon={<Laptop className="h-3 w-3" />}
              label="电脑"
              value={team ? "shared" : bot?.computer_profile || "browser"}
            />
            <Metric icon={<Activity className="h-3 w-3" />} label="工具" value={String(toolCount)} />
            <Metric icon={<Coins className="h-3 w-3" />} label="总成本" value={formatCNY(conversation?.spent_micros ?? 0)} />
          </div>
          {team && (
            <div className="mt-3 flex -space-x-1.5">
              {team.bots.map((item) => (
                <BotAvatar
                  key={item.bot.id}
                  name={item.bot.name}
                  id={item.bot.id}
                  size={24}
                  className="ring-2 ring-panel-2"
                />
              ))}
            </div>
          )}
          {speaker && (
            <div className="mt-3 flex items-center gap-2 rounded-lg border border-mint/20 bg-mint/[0.05] px-2.5 py-1.5">
              <span className="pulse-soft h-1.5 w-1.5 rounded-full bg-mint" />
              <span className="font-mono text-[9px] uppercase tracking-[0.12em] text-mint">speaking</span>
              <span className="text-[11px] text-text/85">{speaker.name}</span>
            </div>
          )}
        </div>

        {bot && (
          <div className="rounded-2xl border border-line/80 bg-panel-2/40 p-4">
            <BotRoutineList bot={bot} />
          </div>
        )}

        <div className="rounded-2xl border border-line/80 bg-panel-2/40 p-4">
          <div className="mb-3 flex items-center gap-2 font-mono text-[9px] uppercase tracking-[0.18em] text-muted">
            <Radio className="h-3 w-3 text-mint" />
            current run
          </div>
          <div className="grid grid-cols-3 gap-2">
            <MiniStat label="输入" value={usage ? formatNumber(usage.input) : "—"} />
            <MiniStat label="输出" value={usage ? formatNumber(usage.output) : "—"} />
            <MiniStat label="成本" value={usage ? formatCNY(usage.cost) : "—"} />
          </div>
          <div className="mt-3 h-1 overflow-hidden rounded-full bg-white/[0.06]">
            <div
              className={cycleActive ? "h-full w-1/2 animate-pulse rounded-full bg-mint/70" : "h-full w-0 bg-mint/70"}
            />
          </div>
          <p className="mt-2 font-mono text-[9.5px] leading-relaxed text-muted/60">
            {cycleActive ? "事件流已连接 · 等待下一次工具调用" : "空闲 · 发消息后开始一轮 agent loop"}
          </p>
        </div>

        {team && (
          <div>
            <div className="mb-2 flex items-center gap-2 px-1 font-mono text-[9px] uppercase tracking-[0.18em] text-muted">
              <Users className="h-3 w-3" />
              {team.mode === "group" ? "group turns" : "coordinator log"}
            </div>
            {routes.length === 0 ? (
              <div className="rounded-xl border border-dashed border-line/70 px-4 py-5 text-center font-mono text-[10px] uppercase tracking-[0.14em] text-muted/50">
                {team.mode === "group" ? "发送消息后，被 @ 的 Bot 才会发言" : "waiting for a decision"}
              </div>
            ) : (
              <div className="space-y-2">
                {routes.map((route) => (
                  <div key={`${route.round}-${route.next_bot_id}-${route.action}`} className="rounded-xl border border-line/70 bg-black/15 px-3 py-2.5">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-[8.5px] uppercase tracking-[0.12em] text-muted/50">
                        r{route.round}
                      </span>
                      <span className="rounded bg-white/[0.05] px-1.5 py-0.5 font-mono text-[8.5px] uppercase tracking-[0.1em] text-muted">
                        {route.action}
                      </span>
                      {route.next_bot_name && (
                        <span className="truncate text-[11px] text-text/85">→ {route.next_bot_name}</span>
                      )}
                    </div>
                    {route.reason && (
                      <div className="mt-1 line-clamp-2 text-[10.5px] leading-relaxed text-muted/75">
                        {route.reason}
                      </div>
                    )}
                    {route.instruction && (
                      <div className="mt-1 line-clamp-2 border-l border-mint/20 pl-2 font-mono text-[9.5px] leading-relaxed text-muted/55">
                        {route.instruction}
                      </div>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        {stream?.reasoning && (
          <div className="rounded-2xl border border-line/80 bg-panel-2/40 p-4">
            <div className="mb-2 flex items-center gap-2 font-mono text-[9px] uppercase tracking-[0.18em] text-muted">
              <Brain className="h-3 w-3 text-amber" />
              reasoning
            </div>
            <div className="max-h-40 overflow-y-auto whitespace-pre-wrap font-mono text-[10.5px] leading-relaxed text-muted/80">
              {stream.reasoning}
            </div>
          </div>
        )}

        <div>
          <div className="mb-2 flex items-center gap-2 px-1 font-mono text-[9px] uppercase tracking-[0.18em] text-muted">
            <Wrench className="h-3 w-3" />
            tool timeline
          </div>
          {tools.length === 0 ? (
            <div className="rounded-xl border border-dashed border-line/70 px-4 py-6 text-center font-mono text-[10px] uppercase tracking-[0.14em] text-muted/50">
              no tool calls yet
            </div>
          ) : (
            <div className="space-y-2">
              {tools.map((tool) => (
                <ToolCard key={tool.callId} tool={tool} />
              ))}
            </div>
          )}
        </div>

        <div className="rounded-xl border border-line/60 bg-black/15 px-3 py-2.5">
          <div className="flex items-center gap-2 font-mono text-[9px] uppercase tracking-[0.16em] text-muted/60">
            <BotIcon className="h-3 w-3" />
            {team ? "team brief" : "system prompt"}
          </div>
          <div className="mt-1 line-clamp-3 text-[10.5px] leading-relaxed text-muted/70">
            {team?.description || bot?.system_prompt || "未设置专属设定，使用平台默认协作规则。"}
          </div>
        </div>
      </div>
    </aside>
  );
}

function Metric({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <div className="rounded-lg border border-line/60 bg-black/15 px-2.5 py-2">
      <div className="flex items-center gap-1 font-mono text-[8.5px] uppercase tracking-[0.14em] text-muted/60">
        {icon}
        {label}
      </div>
      <div className="mt-1 truncate font-mono text-[11px] text-text/90">{value}</div>
    </div>
  );
}

function MiniStat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="font-mono text-[8.5px] uppercase tracking-[0.14em] text-muted/60">{label}</div>
      <div className="mt-1 font-mono text-[12px] text-text/90">{value}</div>
    </div>
  );
}
