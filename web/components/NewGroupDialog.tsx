"use client";

import { useEffect, useState } from "react";
import { Check, Loader2, Users } from "lucide-react";

import { BotAvatar } from "./BotAvatar";
import { Modal } from "./ui/Modal";
import { cn } from "@/lib/format";
import type { Bot } from "@/lib/types";

export function NewGroupDialog({
  open,
  bots,
  primaryBotId,
  creating,
  error,
  onClose,
  onCreate,
}: {
  open: boolean;
  bots: Bot[];
  primaryBotId?: string | null;
  creating: boolean;
  error: string | null;
  onClose: () => void;
  onCreate: (input: { name: string; bot_ids: string[] }) => void;
}) {
  const [name, setName] = useState("");
  const [selected, setSelected] = useState<string[]>([]);

  useEffect(() => {
    if (open) {
      setName("");
      setSelected(
        primaryBotId && bots.some((bot) => bot.id === primaryBotId) ? [primaryBotId] : [],
      );
    }
  }, [open, primaryBotId, bots]);

  const toggle = (botID: string) => {
    setSelected((prev) => {
      if (prev.includes(botID)) return prev.filter((id) => id !== botID);
      if (prev.length >= 6) return prev;
      return [...prev, botID];
    });
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="新建群聊"
      subtitle="最多 6 个 Bot。被 @ 的 Bot 才会发言。"
      size="md"
      footer={
        <>
          <button type="button" className="sb-btn" data-variant="secondary" onClick={onClose}>
            取消
          </button>
          <button
            type="button"
            disabled={creating || !name.trim() || selected.length === 0}
            onClick={() => onCreate({ name: name.trim(), bot_ids: selected })}
            className="sb-btn"
            data-variant="primary"
          >
            {creating ? <Loader2 className="sb-spin h-3.5 w-3.5" /> : <Users className="h-3.5 w-3.5" />}
            创建群聊
          </button>
        </>
      }
    >
      <label className="block">
        <span className="sb-field-label">群聊名称</span>
        <input
          autoFocus
          className="sb-field"
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="例如：产品小队"
        />
      </label>

      <div className="mt-5">
        <div className="mb-2 flex items-center justify-between">
          <span className="sb-field-label !mb-0">成员</span>
          <span className="text-[11px] tabular-nums text-[var(--sb-text-3)]">{selected.length} / 6</span>
        </div>
        <div className="max-h-[300px] overflow-y-auto rounded-xl border border-[var(--sb-border)] p-1">
          {bots.length === 0 && (
            <div className="px-3 py-8 text-center text-[12px] text-[var(--sb-text-2)]">
              还没有 Bot，先创建一个。
            </div>
          )}
          {bots.map((bot) => {
            const checked = selected.includes(bot.id);
            return (
              <button
                key={bot.id}
                type="button"
                onClick={() => toggle(bot.id)}
                className={cn(
                  "flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left transition",
                  checked ? "bg-[var(--sb-hover)]" : "hover:bg-[var(--sb-hover)]",
                )}
              >
                <BotAvatar
                  name={bot.name}
                  id={bot.id}
                  size={30}
                  shape={bot.avatar_shape}
                  color={bot.avatar_color}
                />
                <span className="min-w-0 flex-1">
                  <span className="flex items-center gap-1.5">
                    <span className="truncate text-[13px] leading-[18px]">{bot.name}</span>
                    {bot.id === primaryBotId && (
                      <span className="shrink-0 rounded-full bg-[var(--sb-hover)] px-1.5 py-px text-[10px] text-[var(--sb-text-2)]">
                        主 Bot
                      </span>
                    )}
                  </span>
                  <span className="mt-0.5 block truncate text-[11px] leading-[15px] text-[var(--sb-text-2)]">
                    {bot.title || bot.model}
                  </span>
                </span>
                <span
                  className={cn(
                    "flex h-[18px] w-[18px] shrink-0 items-center justify-center rounded-full border transition",
                    checked
                      ? "border-[var(--sb-primary)] bg-[var(--sb-primary)] text-[var(--sb-primary-contrast)]"
                      : "border-[var(--sb-border-strong)] text-transparent",
                  )}
                >
                  <Check className="h-3 w-3" strokeWidth={3} />
                </span>
              </button>
            );
          })}
        </div>
      </div>

      {error && (
        <div className="mt-4 rounded-lg bg-[var(--sb-danger-soft)] px-3 py-2 text-[12px] text-[var(--sb-danger)]">
          {error}
        </div>
      )}
    </Modal>
  );
}
