"use client";

import { useEffect, useState } from "react";
import { Loader2 } from "lucide-react";

import { BotAvatar } from "./BotAvatar";
import { Modal } from "./ui/Modal";
import { cn } from "@/lib/format";
import { AVATAR_COLORS as colors, AVATAR_SHAPES as shapes } from "@/lib/avatars";

export function NewBotDialog({
  open,
  creating,
  error,
  team = false,
  onClose,
  onCreate,
}: {
  open: boolean;
  team?: boolean;
  creating: boolean;
  error: string | null;
  onClose: () => void;
  onCreate: (input: { name: string; avatar_shape: string; avatar_color: string }) => void;
}) {
  const [name, setName] = useState("");
  const [shape, setShape] = useState("blob");
  const [color, setColor] = useState("violet");

  useEffect(() => {
    if (open) {
      setName("");
      setShape("blob");
      setColor("violet");
    }
  }, [open]);

  const submit = () => {
    const trimmed = name.trim();
    if (!trimmed || creating) return;
    onCreate({ name: trimmed, avatar_shape: shape, avatar_color: color });
  };

  const activeColor = colors.find(([key]) => key === color)?.[1] ?? "#8351e4";

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={team ? "创建 Team Bot" : "创建新 Bot"}
      size="md"
      panelClassName="!max-w-[552px] !-translate-y-[20px]"
      footer={
        <div className="flex py-2.5">
          <button type="button" className="sb-btn" data-variant="secondary" onClick={onClose}>
            取消
          </button>
          <button
            type="button"
            className="sb-btn"
            data-variant="primary"
            onClick={submit}
            disabled={creating || !name.trim()}
          >
            {creating && <Loader2 className="sb-spin h-3.5 w-3.5" />}
            创建
          </button>
        </div>
      }
    >
      <div className="flex flex-col items-center">
        <div className="relative flex h-[140px] w-[140px] items-center justify-center">
          <BotAvatar
            name={name}
            id={`preview-${shape}-${color}`}
            size={132}
            shape={shape}
            color={color}
          />
        </div>

        <input
          autoFocus
          value={name}
          onChange={(event) => setName(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.nativeEvent.isComposing) submit();
          }}
          placeholder="给你的 Bot 起个名字"
          className="mt-6 h-10 w-full rounded-lg border border-[var(--sb-border)] bg-[var(--sb-pane)] px-3 text-center text-[15px] outline-none transition focus:border-[var(--sb-border-strong)]"
        />

        <div className="mt-6 grid w-full grid-cols-5 justify-items-center gap-x-2.5 gap-y-4">
          {shapes.map((item) => {
            const selected = shape === item;
            return (
              <button
                key={item}
                type="button"
                onClick={() => setShape(item)}
                className={cn(
                  "flex h-12 w-12 items-center justify-center rounded-[12px] transition hover:bg-[var(--sb-hover)]",
                  selected && "bg-[var(--sb-hover)] ring-2 ring-[var(--sb-text)] ring-offset-2 ring-offset-[var(--sb-pane)]",
                )}
                aria-label={`${item} 形状`}
              >
                <BotAvatar
                  name=""
                  id={`shape-${item}`}
                  size={32}
                  shape={item}
                  color={color}
                />
              </button>
            );
          })}
        </div>

        <div className="mt-5 grid w-full grid-cols-6 justify-items-center gap-x-2 gap-y-8">
          {colors.map(([key, value]) => (
            <button
              key={key}
              type="button"
              onClick={() => setColor(key)}
              className={cn(
                "h-5 w-5 rounded-full transition",
                color === key
                  ? "ring-2 ring-[var(--sb-text)] ring-offset-2 ring-offset-[var(--sb-pane)]"
                  : "hover:scale-110",
              )}
              style={{ background: value }}
              aria-label={`${key} 颜色`}
              title={key}
            />
          ))}
        </div>

        {error && (
          <div className="mt-5 w-full rounded-lg bg-[var(--sb-danger-soft)] px-3 py-2 text-[12px] text-[var(--sb-danger)]">
            {error}
          </div>
        )}

        <span className="hidden">{activeColor}</span>
      </div>
    </Modal>
  );
}
