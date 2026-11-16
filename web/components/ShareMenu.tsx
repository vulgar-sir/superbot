"use client";

import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Link2 } from "lucide-react";

import { BotAvatar } from "./BotAvatar";
import type { Bot } from "@/lib/types";

export function ShareMenu({
  open,
  anchor,
  bot,
  onClose,
  onCreate,
  onCopyLink,
}: {
  open: boolean;
  anchor: { rect: DOMRect } | null;
  bot: Bot | null;
  onClose: () => void;
  onCreate: () => void;
  onCopyLink: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState<{ left: number; top: number } | null>(null);

  useLayoutEffect(() => {
    if (!open || !anchor) {
      setPosition(null);
      return;
    }
    const width = 227;
    const padding = 8;
    let left = anchor.rect.right - width;
    let top = anchor.rect.bottom + 8;
    const height = ref.current?.offsetHeight ?? 72;
    if (left + width > window.innerWidth - padding) left = window.innerWidth - width - padding;
    if (left < padding) left = padding;
    if (top + height > window.innerHeight - padding) {
      top = anchor.rect.top - height - 8;
    }
    if (top < padding) top = padding;
    setPosition({ left, top });
  }, [open, anchor]);

  useEffect(() => {
    if (!open) return;
    const onDown = (event: MouseEvent) => {
      const target = event.target as HTMLElement;
      if (target.closest('[aria-label="分享"]')) return;
      if (!ref.current?.contains(target)) onClose();
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        onClose();
      }
    };
    window.addEventListener("mousedown", onDown, true);
    window.addEventListener("keydown", onKey, true);
    return () => {
      window.removeEventListener("mousedown", onDown, true);
      window.removeEventListener("keydown", onKey, true);
    };
  }, [open, onClose]);

  if (!open || typeof document === "undefined") return null;

  return createPortal(
    <div
      ref={ref}
      className="fixed z-50 w-[227px] rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-1.5 shadow-[var(--sb-shadow-popup)]"
      style={{ left: position?.left ?? 0, top: position?.top ?? 0, visibility: position ? "visible" : "hidden" }}
      role="menu"
    >
      {bot ? (
        <button
          type="button"
          onClick={() => {
            onClose();
            onCreate();
          }}
          className="flex w-full items-center gap-3 rounded-xl px-2.5 py-2.5 text-left transition hover:bg-[var(--sb-hover)]"
        >
          <BotAvatar
            name={bot.name}
            id={bot.id}
            size={32}
            shape={bot.avatar_shape}
            color={bot.avatar_color}
          />
          <span className="min-w-0 flex-1">
            <span className="block truncate text-[14px] leading-[19px] text-[var(--sb-text)]">
              创建副本
            </span>
            <span className="mt-0.5 block truncate text-[12px] leading-[16px] text-[var(--sb-text-2)]">
              分享模板链接
            </span>
          </span>
        </button>
      ) : (
        <button
          type="button"
          onClick={() => {
            onClose();
            onCopyLink();
          }}
          className="flex w-full items-center gap-3 rounded-xl px-2.5 py-2.5 text-left transition hover:bg-[var(--sb-hover)]"
        >
          <span className="flex h-8 w-8 items-center justify-center rounded-full bg-[var(--sb-hover)] text-[var(--sb-text-2)]">
            <Link2 className="h-4 w-4" />
          </span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-[14px] leading-[19px] text-[var(--sb-text)]">
              复制会话链接
            </span>
            <span className="mt-0.5 block truncate text-[12px] leading-[16px] text-[var(--sb-text-2)]">
              分享当前对话
            </span>
          </span>
        </button>
      )}
    </div>,
    document.body,
  );
}
