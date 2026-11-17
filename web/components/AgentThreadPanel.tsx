"use client";

import { useEffect, useState } from "react";
import { Loader2 } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import type { AgentThreadDetail, Bot } from "@/lib/types";
import { BotAvatar } from "./BotAvatar";
import { ChatMessage } from "./ChatMessage";
import { Modal } from "./ui/Modal";

export function AgentThreadPanel({
  threadID,
  onClose,
  onOpenLive,
}: {
  threadID: string;
  onClose: () => void;
  // Opens the thread in the main view, with tool calls and live output.
  onOpenLive?: (detail: AgentThreadDetail) => void;
}) {
  const [detail, setDetail] = useState<AgentThreadDetail | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const data = await api.getAgentThread(threadID);
        if (!cancelled) setDetail(data);
      } catch (err) {
        if (!cancelled) setError(err instanceof ApiError ? err.message : "加载 Bot 私信失败");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [threadID]);

  const botFor = (botID: string | null): Bot | undefined => {
    if (!detail || !botID) return undefined;
    if (detail.bot_a.id === botID) return detail.bot_a;
    if (detail.bot_b.id === botID) return detail.bot_b;
    return undefined;
  };

  return (
    <Modal
      open
      onClose={onClose}
      title={
        <span className="flex items-center gap-2">
          {detail ? (
            <span className="flex -space-x-2">
              <BotAvatar
                name={detail.bot_a.name}
                id={detail.bot_a.id}
                size={24}
                shape={detail.bot_a.avatar_shape}
                color={detail.bot_a.avatar_color}
                avatar={detail.bot_a.avatar}
              />
              <BotAvatar
                name={detail.bot_b.name}
                id={detail.bot_b.id}
                size={24}
                shape={detail.bot_b.avatar_shape}
                color={detail.bot_b.avatar_color}
                avatar={detail.bot_b.avatar}
                className="ring-2 ring-[var(--sb-pane)]"
              />
            </span>
          ) : null}
          <span>Bot 私信</span>
        </span>
      }
      subtitle={
        detail ? `${detail.bot_a.name} ↔ ${detail.bot_b.name}` : "加载中…"
      }
      size="lg"
    >
      <div className="min-h-[240px]">
        {!detail && !error && (
          <div className="flex items-center justify-center py-16">
            <Loader2 className="sb-spin h-5 w-5 text-[var(--sb-text-3)]" />
          </div>
        )}
        {error && (
          <div className="rounded-xl bg-[var(--sb-danger-soft)] px-3.5 py-3 text-[12px] text-[var(--sb-danger)]">
            {error}
          </div>
        )}
        {detail && detail.messages.length === 0 && (
          <div className="sb-empty">
            <div className="sb-empty-title">还没有私信</div>
            <div className="sb-empty-desc">Bot 之间的交流会被折叠到主聊天里，这里可以查看全文。</div>
          </div>
        )}
        {detail && detail.messages.length > 0 && (
          <div className="space-y-1">
            {detail.messages.map((message) => (
              <ChatMessage
                key={message.id}
                message={message}
                bot={botFor(message.sender_bot_id)}
                bots={[detail.bot_a, detail.bot_b]}
                groupPosition="single"
                showSender
              />
            ))}
          </div>
        )}
        {detail && onOpenLive && (
          <div className="mt-4 flex justify-center">
            <button
              type="button"
              className="sb-btn"
              data-variant="secondary"
              data-size="sm"
              onClick={() => onOpenLive(detail)}
            >
              查看完整过程（工具调用与实时进度）
            </button>
          </div>
        )}
        <div className="mt-4 text-center text-[11px] text-[var(--sb-text-4)]">
          Bot 之间的私信 · 只读
        </div>
      </div>
    </Modal>
  );
}
