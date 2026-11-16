"use client";


import { BotAvatar } from "./BotAvatar";
import { Markdown } from "./Markdown";
import { stripModelArtifacts } from "@/lib/modelText";
import type { Bot, StreamState } from "@/lib/types";

export function StreamingMessage({
  stream,
  bot,
  status,
  showSender = false,
}: {
  stream: StreamState;
  bot?: Bot;
  status?: string;
  showSender?: boolean;
}) {
  const hasText = stream.text.length > 0;
  const running = !status || status === "running";

  return (
    <div className="sb-message-row" data-side="left">
      <div className={showSender ? "w-full min-w-0 pl-[30px]" : "w-full min-w-0"}>
        {showSender && (
          <div className="sb-sender-row">
            <BotAvatar
              name={bot?.name ?? "Bot"}
              id={stream.botId}
              size={20}
              shape={bot?.avatar_shape}
              color={bot?.avatar_color}
            />
            <span className="font-medium text-[var(--sb-text)]">{bot?.name ?? "Bot"}</span>
            {running && <span className="sb-pulse">正在输入…</span>}
          </div>
        )}
        <div className="relative sb-bubble-wrap">
          <div className="sb-bubble" data-group="single">
            {hasText ? (
              <div>
                <Markdown content={stripModelArtifacts(stream.text, true)} />
                {running && <span className="sb-caret" />}
              </div>
            ) : (
              <div className="flex items-center gap-1 py-1 text-[15px] text-[var(--sb-text-2)]">
                <span className="sb-pulse h-1.5 w-1.5 rounded-full bg-[var(--sb-text-3)]" />
                <span className="sb-pulse h-1.5 w-1.5 rounded-full bg-[var(--sb-text-3)] [animation-delay:120ms]" />
                <span className="sb-pulse h-1.5 w-1.5 rounded-full bg-[var(--sb-text-3)] [animation-delay:240ms]" />
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
