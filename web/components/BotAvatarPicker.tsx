"use client";

import { useEffect, useRef, useState } from "react";
import { Loader2, Upload, Wand2 } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import { BotAvatar } from "./BotAvatar";
import { useToast } from "./ui/Toast";
import { avatarHue } from "@/lib/format";
import { AVATAR_COLORS, AVATAR_SHAPES } from "@/lib/avatars";

export function BotAvatarPicker({
  bot,
  open,
  saving,
  onClose,
  onChange,
  onAvatarChanged,
}: {
  bot: { id: string; name: string; avatar_shape: string; avatar_color: string; avatar?: string };
  open: boolean;
  saving: boolean;
  onClose: () => void;
  onChange: (shape: string, color: string) => void;
  onAvatarChanged?: () => void | Promise<void>;
}) {
  const { toast } = useToast();
  const panelRef = useRef<HTMLDivElement>(null);
  const defaultShape = (AVATAR_SHAPES as readonly string[]).includes(bot.avatar_shape)
    ? bot.avatar_shape
    : "square";
  const defaultColor = (() => {
    if (AVATAR_COLORS.some(([key]) => key === bot.avatar_color)) return bot.avatar_color;
    const hue = avatarHue(bot.id);
    const candidates: Array<[string, number]> = [
      ["red", 356],
      ["orange", 22],
      ["yellow", 38],
      ["green", 145],
      ["cyan", 174],
      ["blue", 212],
      ["violet", 261],
      ["magenta", 333],
    ];
    let best = candidates[0];
    let distance = 360;
    for (const item of candidates) {
      const delta = Math.abs(item[1] - hue);
      const normalized = Math.min(delta, 360 - delta);
      if (normalized < distance) {
        distance = normalized;
        best = item;
      }
    }
    return best[0];
  })();
  const [tab, setTab] = useState<"bot" | "generate" | "upload">("bot");
  const [shape, setShape] = useState(defaultShape);
  const [color, setColor] = useState(defaultColor);
  const [prompt, setPrompt] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!open) return;
    setShape(defaultShape);
    setColor(defaultColor);
    setTab("bot");
    setPrompt("");
  }, [open, bot.id]);

  useEffect(() => {
    if (!open) return;
    const onDown = (event: MouseEvent) => {
      const target = event.target as HTMLElement;
      if (target.closest('[aria-label="编辑 Bot 头像"]')) return;
      if (!panelRef.current?.contains(target)) onClose();
    };
    const timer = window.setTimeout(() => {
      document.addEventListener("mousedown", onDown);
    }, 0);
    return () => {
      window.clearTimeout(timer);
      document.removeEventListener("mousedown", onDown);
    };
  }, [open, onClose]);

  if (!open) return null;

  const apply = (nextShape: string, nextColor: string) => {
    setShape(nextShape);
    setColor(nextColor);
    onChange(nextShape, nextColor);
  };

  const generate = async () => {
    const text = prompt.trim();
    if (!text) {
      setError("请先描述你想要的头像");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await api.generateBotAvatar(bot.id, text);
      await onAvatarChanged?.();
      toast("头像已生成", "success");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "生成头像失败");
    } finally {
      setBusy(false);
    }
  };

  const upload = async (file: File | undefined) => {
    if (!file) return;
    setBusy(true);
    setError(null);
    try {
      await api.uploadBotAvatar(bot.id, file);
      await onAvatarChanged?.();
      toast("头像已更新", "success");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "上传头像失败");
    } finally {
      setBusy(false);
      if (fileRef.current) fileRef.current.value = "";
    }
  };

  const resetAvatar = async () => {
    setBusy(true);
    setError(null);
    try {
      await api.deleteBotAvatar(bot.id);
      await onAvatarChanged?.();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "重置头像失败");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      ref={panelRef}
      className="absolute left-2 right-2 top-[150px] z-30 rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-3 shadow-[var(--sb-shadow-popup)]"
    >
      <div className="flex items-center gap-1">
        {(
          [
            ["bot", "Bot"],
            ["generate", "生成"],
            ["upload", "上传"],
          ] as const
        ).map(([key, label]) => (
          <button
            key={key}
            type="button"
            onClick={() => setTab(key)}
            className={
              tab === key
                ? "h-7 rounded-full bg-[var(--sb-hover)] px-2.5 text-[12px] text-[var(--sb-text)]"
                : "h-7 rounded-full px-2.5 text-[12px] text-[var(--sb-text-2)] transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-text)]"
            }
          >
            {label}
          </button>
        ))}
        <button
          type="button"
          onClick={() => apply(defaultShape, defaultColor)}
          className="ml-auto text-[12px] text-[var(--sb-text-2)] transition hover:text-[var(--sb-text)]"
        >
          重置
        </button>
      </div>

      {tab === "bot" && (
        <>
          <div className="mt-4 grid grid-cols-5 justify-items-center gap-2">
            {AVATAR_SHAPES.map((item) => {
              const selected = shape === item;
              return (
                <button
                  key={item}
                  type="button"
                  onClick={() => apply(item, color)}
                  className={
                    selected
                      ? "flex h-9 w-9 items-center justify-center rounded-full ring-2 ring-[var(--sb-text)] ring-offset-2 ring-offset-[var(--sb-pane)]"
                      : "flex h-9 w-9 items-center justify-center rounded-full transition hover:bg-[var(--sb-hover)]"
                  }
                  aria-label={`选择头像形状 ${item}`}
                >
                  <BotAvatar id={`picker-${item}-${color}`} name={bot.name} size={30} shape={item} color={color} />
                </button>
              );
            })}
          </div>

          <div className="mt-4 grid grid-cols-6 justify-items-center gap-2">
            {AVATAR_COLORS.map(([key, value]) => {
              const selected = color === key;
              return (
                <button
                  key={key}
                  type="button"
                  onClick={() => apply(shape, key)}
                  className={
                    selected
                      ? "h-5 w-5 rounded-full ring-2 ring-[var(--sb-text)] ring-offset-2 ring-offset-[var(--sb-pane)]"
                      : "h-5 w-5 rounded-full transition hover:scale-110"
                  }
                  style={{ background: value }}
                  aria-label={`选择头像颜色 ${key}`}
                />
              );
            })}
          </div>
        </>
      )}

      {tab === "generate" && (
        <div className="mt-4">
          <label className="field flex min-h-[72px] resize-y items-start gap-2 px-3 py-2">
            <Wand2 className="mt-0.5 h-4 w-4 shrink-0 text-[var(--sb-text-3)]" />
            <textarea
              value={prompt}
              onChange={(event) => setPrompt(event.target.value)}
              placeholder="描述你想要的 Bot 头像，例如：一只戴墨镜的绿色方块"
              className="min-h-[56px] w-full resize-none bg-transparent text-[13px] leading-[20px] outline-none placeholder:text-[var(--sb-text-4)]"
            />
          </label>
          <button
            type="button"
            onClick={() => void generate()}
            disabled={busy}
            className="sb-btn mt-2 w-full"
            data-variant="secondary"
            data-size="sm"
          >
            {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
            生成头像
          </button>
          <div className="mt-1.5 text-center text-[11px] text-[var(--sb-text-4)]">
            使用已配置的图像生成模型，未配置时会提示 requires_config
          </div>
        </div>
      )}

      {tab === "upload" && (
        <div className="mt-4">
          <button
            type="button"
            onClick={() => fileRef.current?.click()}
            disabled={busy}
            className="flex min-h-[140px] w-full flex-col items-center justify-center rounded-xl border border-dashed border-[var(--sb-border)] px-4 text-center transition hover:border-[var(--sb-border-strong)] hover:bg-[var(--sb-hover)]"
          >
            {bot.avatar ? (
              // eslint-disable-next-line @next/next/no-img-element
              <img
                src={bot.avatar}
                alt=""
                className="h-16 w-16 rounded-xl object-cover"
                draggable={false}
              />
            ) : (
              <Upload className="h-5 w-5 text-[var(--sb-text-3)]" />
            )}
            <div className="mt-2 text-[13px] text-[var(--sb-text-2)]">
              {busy ? "正在上传…" : "上传图片"}
            </div>
            <div className="mt-1 text-[12px] text-[var(--sb-text-3)]">PNG / JPEG / WebP / GIF，≤4MB</div>
          </button>
          <input
            ref={fileRef}
            type="file"
            accept="image/png,image/jpeg,image/webp,image/gif"
            className="hidden"
            onChange={(event) => void upload(event.target.files?.[0])}
          />
          {bot.avatar && (
            <button
              type="button"
              onClick={() => void resetAvatar()}
              disabled={busy}
              className="mt-2 w-full text-center text-[12px] text-[var(--sb-text-3)] transition hover:text-[var(--sb-text)]"
            >
              移除图片，恢复形状头像
            </button>
          )}
        </div>
      )}

      {error && <div className="mt-2 text-center text-[11px] text-[var(--sb-danger)]">{error}</div>}

      {saving && (
        <div className="mt-2 text-center text-[11px] text-[var(--sb-text-3)]">正在保存…</div>
      )}
    </div>
  );
}
