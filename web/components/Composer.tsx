"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowUp,
  AudioLines,
  MessageCircleQuestion,
  Target,
  Loader2,
  Mic,
  MicOff,
  Paperclip,
  Plus,
  Square,
  X,
} from "lucide-react";

import { cn } from "@/lib/format";
import { BotAvatar } from "./BotAvatar";
import { CaptureOverlay } from "./CaptureOverlay";
import { useLanguage } from "./LanguageProvider";
import { useToast } from "./ui/Toast";
import { Menu, type MenuItem } from "./ui/Menu";
import type { Attachment, Bot } from "@/lib/types";

type SpeechRecognitionLike = {
  lang: string;
  continuous: boolean;
  interimResults: boolean;
  start: () => void;
  stop: () => void;
  onresult: ((event: any) => void) | null;
  onend: (() => void) | null;
  onerror: ((event: any) => void) | null;
};

export function Composer({
  value,
  onChange,
  onSend,
  onStop,
  busy,
  disabled,
  placeholder,
  onOpenVoice,
  mentionBots = [],
  attachments = [],
  uploadingAttachments = false,
  onUploadFiles,
  onSendVoice,
  onRemoveAttachment,
  onSideQuestion,
}: {
  value: string;
  onChange: (value: string) => void;
  onSend: () => void;
  onStop: () => void;
  busy: boolean;
  disabled?: boolean;
  placeholder?: string;
  onOpenVoice?: () => void;
  mentionBots?: Bot[];
  attachments?: Attachment[];
  uploadingAttachments?: boolean;
  onUploadFiles?: (files: File[]) => void | Promise<void>;
  onSendVoice?: (file: File) => void | Promise<void>;
  onRemoveAttachment?: (id: string) => void;
  /** Opens the side-question panel; also enables the `/side` hint while busy. */
  onSideQuestion?: () => void;
}) {
  // Sending stays available while a cycle is running: the message is
  // delivered to the bot as an interjection before its next step.
  const hasContent = value.trim().length > 0 || attachments.length > 0;
  const canSend = !disabled && hasContent;
  const [listening, setListening] = useState(false);
  const [speechError, setSpeechError] = useState<string | null>(null);
  const [plusAnchor, setPlusAnchor] = useState<{ rect: DOMRect } | null>(null);
  const [captureOpen, setCaptureOpen] = useState(false);
  const [recordingAudio, setRecordingAudio] = useState(false);
  const [audioSeconds, setAudioSeconds] = useState(0);
  const audioRecorderRef = useRef<MediaRecorder | null>(null);
  const audioStreamRef = useRef<MediaStream | null>(null);
  const audioChunksRef = useRef<Blob[]>([]);
  const audioTimerRef = useRef<number | null>(null);
  const audioCancelRef = useRef(false);
  const [mention, setMention] = useState<{ start: number; query: string } | null>(null);
  const [mentionIndex, setMentionIndex] = useState(0);
  const recognitionRef = useRef<SpeechRecognitionLike | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const { toast } = useToast();
  const { t } = useLanguage();

  useEffect(() => {
    return () => {
      recognitionRef.current?.stop();
    };
  }, []);

  useEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 220)}px`;
  }, [value]);

  const toggleMic = () => {
    setSpeechError(null);
    if (listening) {
      recognitionRef.current?.stop();
      setListening(false);
      return;
    }
    const globalWindow = window as any;
    const Recognition = globalWindow.SpeechRecognition || globalWindow.webkitSpeechRecognition;
    if (!Recognition) {
      setSpeechError("当前浏览器不支持语音输入");
      return;
    }
    const recognition: SpeechRecognitionLike = new Recognition();
    recognition.lang = navigator.language?.startsWith("zh") ? "zh-CN" : "en-US";
    recognition.continuous = false;
    recognition.interimResults = true;
    recognition.onresult = (event: any) => {
      let transcript = "";
      for (let i = event.resultIndex; i < event.results.length; i += 1) {
        transcript += event.results[i][0].transcript;
      }
      if (transcript) onChange(`${value}${value ? " " : ""}${transcript}`);
    };
    recognition.onend = () => setListening(false);
    recognition.onerror = (event: any) => {
      setListening(false);
      setSpeechError(event?.error === "not-allowed" ? "麦克风权限被拒绝" : "语音识别失败");
    };
    recognitionRef.current = recognition;
    setListening(true);
    recognition.start();
  };

  const mentionMatches = useMemo(() => {
    if (!mention || mentionBots.length === 0) return [];
    const query = mention.query.toLowerCase();
    return mentionBots.filter((bot) => bot.name.toLowerCase().includes(query)).slice(0, 6);
  }, [mention, mentionBots]);

  const updateMention = (next: string, cursor: number) => {
    if (mentionBots.length === 0) return;
    const before = next.slice(0, cursor);
    const match = before.match(/(^|\s)@([^\s@]*)$/);
    if (match) {
      setMention({ start: cursor - match[2].length - 1, query: match[2] });
      setMentionIndex(0);
    } else {
      setMention(null);
    }
  };

  const insertMention = (bot: Bot) => {
    if (!mention) return;
    const cursor = textareaRef.current?.selectionStart ?? value.length;
    const next = `${value.slice(0, mention.start)}@${bot.name} ${value.slice(cursor)}`;
    onChange(next);
    setMention(null);
    const position = mention.start + bot.name.length + 2;
    window.requestAnimationFrame(() => {
      const el = textareaRef.current;
      if (!el) return;
      el.focus();
      el.setSelectionRange(position, position);
    });
  };

  const appendToComposer = (text: string) => {
    onChange(`${value}${value ? "\n\n" : ""}${text}`);
    window.requestAnimationFrame(() => {
      const el = textareaRef.current;
      if (!el) return;
      el.focus();
      el.setSelectionRange(el.value.length, el.value.length);
    });
  };

  const handleFile = (event: React.ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(event.target.files ?? []);
    event.target.value = "";
    if (files.length > 0 && onUploadFiles) void onUploadFiles(files);
  };

  const cleanupAudio = () => {
    if (audioTimerRef.current !== null) window.clearInterval(audioTimerRef.current);
    audioTimerRef.current = null;
    audioStreamRef.current?.getTracks().forEach((track) => track.stop());
    audioStreamRef.current = null;
    audioRecorderRef.current = null;
    setRecordingAudio(false);
    setAudioSeconds(0);
  };

  const stopAudioRecorder = (cancel: boolean) => {
    audioCancelRef.current = cancel;
    const recorder = audioRecorderRef.current;
    if (recorder && recorder.state !== "inactive") {
      recorder.stop();
      return;
    }
    cleanupAudio();
  };

  const startVoiceMessage = async () => {
    if (!navigator.mediaDevices?.getUserMedia) {
      toast("当前环境不支持录音（需要 HTTPS 或桌面客户端）", "danger");
      return;
    }
    try {
      let deviceID = "";
      try {
        deviceID = window.localStorage.getItem("superbot-mic-device") ?? "";
      } catch {
        // ignore
      }
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: deviceID ? { deviceId: { exact: deviceID } } : true,
      });
      audioStreamRef.current = stream;
      audioChunksRef.current = [];
      audioCancelRef.current = false;
      const recorder = new MediaRecorder(stream);
      audioRecorderRef.current = recorder;
      recorder.ondataavailable = (event) => {
        if (event.data.size > 0) audioChunksRef.current.push(event.data);
      };
      recorder.onstop = () => {
        const cancelled = audioCancelRef.current;
        const type = recorder.mimeType || "audio/webm";
        const chunks = audioChunksRef.current;
        cleanupAudio();
        if (cancelled || chunks.length === 0) return;
        const blob = new Blob(chunks, { type });
        const file = new File([blob], `voice-${Date.now()}.webm`, { type });
        void onSendVoice?.(file);
      };
      recorder.start();
      setRecordingAudio(true);
      setAudioSeconds(0);
      const started = Date.now();
      audioTimerRef.current = window.setInterval(() => {
        const elapsed = Math.floor((Date.now() - started) / 1000);
        setAudioSeconds(elapsed);
        if (elapsed >= 300) stopAudioRecorder(false);
      }, 500);
    } catch {
      cleanupAudio();
      toast("无法访问麦克风", "danger");
    }
  };

  const plusItems: MenuItem[] = [
    {
      key: "attach",
      label: t("composer.attach"),
      icon: <Paperclip className="h-3.5 w-3.5" />,
      onSelect: () => fileInputRef.current?.click(),
    },
    {
      key: "voice-message",
      label: t("composer.voiceMessage"),
      icon: <Mic className="h-3.5 w-3.5" />,
      onSelect: () => void startVoiceMessage(),
    },
    {
      key: "teach",
      label: t("composer.teach"),
      icon: <Target className="h-3.5 w-3.5" />,
      onSelect: () => setCaptureOpen(true),
    },
    ...(onSideQuestion
      ? [
          {
            key: "side-question",
            label: t("composer.sideQuestion"),
            description: "/side",
            icon: <MessageCircleQuestion className="h-3.5 w-3.5" />,
            onSelect: onSideQuestion,
          } satisfies MenuItem,
        ]
      : []),
  ];

  return (
    <div className="relative">
      {recordingAudio && (
        <div className="mb-2 flex items-center gap-3 rounded-xl border border-[var(--sb-border)] bg-[var(--sb-pane)] px-3 py-2">
          <span className="h-2 w-2 shrink-0 animate-pulse rounded-full bg-[var(--sb-danger)]" />
          <span className="text-[12.5px]">
            {t("composer.recording")} {Math.floor(audioSeconds / 60)}:{String(audioSeconds % 60).padStart(2, "0")}
          </span>
          <span className="text-[11px] text-[var(--sb-text-3)]">{t("composer.maxFive")}</span>
          <div className="ml-auto flex items-center gap-1.5">
            <button
              type="button"
              onClick={() => stopAudioRecorder(true)}
              className="flex h-7 items-center gap-1 rounded-lg px-2 text-[11.5px] text-[var(--sb-text-2)] transition hover:bg-[var(--sb-hover)]"
            >
              <X className="h-3 w-3" />
              {t("composer.cancel")}
            </button>
            <button
              type="button"
              onClick={() => stopAudioRecorder(false)}
              className="flex h-7 items-center gap-1 rounded-lg bg-[var(--sb-text)] px-2 text-[11.5px] text-[var(--sb-pane)]"
            >
              <ArrowUp className="h-3 w-3" />
              {t("composer.sendVoice")}
            </button>
          </div>
        </div>
      )}
      {(attachments.length > 0 || uploadingAttachments) && (
        <div className="mb-2 flex flex-wrap gap-2">
          {attachments.map((attachment) => (
            <div
              key={attachment.id}
              className="flex items-center gap-2 rounded-lg border border-[var(--sb-border)] bg-[var(--sb-pane)] py-1 pl-1 pr-2"
            >
              {attachment.mime_type.startsWith("image/") ? (
                <img
                  src={attachment.url}
                  alt={attachment.filename}
                  className="h-8 w-8 rounded-md object-cover"
                />
              ) : (
                <span className="flex h-8 w-8 items-center justify-center rounded-md bg-[var(--sb-hover)] text-[10px] font-medium uppercase text-[var(--sb-text-2)]">
                  {attachment.filename.split(".").pop()?.slice(0, 4) || "FILE"}
                </span>
              )}
              <span className="max-w-[140px] truncate text-[12px] text-[var(--sb-text-2)]">
                {attachment.filename}
              </span>
              <button
                type="button"
                onClick={() => onRemoveAttachment?.(attachment.id)}
                className="flex h-5 w-5 items-center justify-center rounded text-[var(--sb-text-4)] transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-text)]"
                aria-label={`移除 ${attachment.filename}`}
              >
                <X className="h-3 w-3" />
              </button>
            </div>
          ))}
          {uploadingAttachments && (
            <div className="flex items-center gap-2 rounded-lg border border-[var(--sb-border)] px-3 py-2 text-[12px] text-[var(--sb-text-2)]">
              <Loader2 className="sb-spin h-3.5 w-3.5" />
              上传中…
            </div>
          )}
        </div>
      )}
      {mention && mentionMatches.length > 0 && (
        <div className="absolute bottom-full left-0 z-30 mb-1.5 w-[260px] overflow-hidden rounded-xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-1 shadow-[var(--sb-shadow-popup)]">
          <div className="px-2 py-1 text-[11px] text-[var(--sb-text-4)]">提及一位 Bot</div>
          {mentionMatches.map((bot, index) => (
            <button
              key={bot.id}
              type="button"
              onMouseDown={(event) => {
                event.preventDefault();
                insertMention(bot);
              }}
              onMouseEnter={() => setMentionIndex(index)}
              className={cn(
                "flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left transition",
                index === mentionIndex && "bg-[var(--sb-hover)]",
              )}
            >
              <BotAvatar
                name={bot.name}
                id={bot.id}
                size={22}
                shape={bot.avatar_shape}
                color={bot.avatar_color}
                avatar={bot.avatar}
              />
              <span className="truncate text-[13px]">{bot.name}</span>
              <span className="ml-auto truncate text-[11px] text-[var(--sb-text-4)]">{bot.title}</span>
            </button>
          ))}
        </div>
      )}
      <div className={cn("sb-composer", disabled && "opacity-60")}>
        <button
          type="button"
          className="sb-composer-btn"
          aria-label={t("composer.more")}
          title={t("composer.more")}
          onClick={(event) => setPlusAnchor({ rect: event.currentTarget.getBoundingClientRect() })}
          disabled={disabled}
        >
          <Plus className="h-[18px] w-[18px]" />
        </button>
        <textarea
          id="superbot-composer"
          ref={textareaRef}
          className="sb-composer-input"
          value={value}
          rows={1}
          onChange={(event) => {
            onChange(event.target.value);
            updateMention(event.target.value, event.target.selectionStart ?? event.target.value.length);
          }}
          onBlur={() => window.setTimeout(() => setMention(null), 140)}
          onKeyDown={(event) => {
            if (mention && mentionMatches.length > 0) {
              if (event.key === "ArrowDown") {
                event.preventDefault();
                setMentionIndex((prev) => (prev + 1) % mentionMatches.length);
                return;
              }
              if (event.key === "ArrowUp") {
                event.preventDefault();
                setMentionIndex((prev) => (prev - 1 + mentionMatches.length) % mentionMatches.length);
                return;
              }
              if (event.key === "Enter" || event.key === "Tab") {
                event.preventDefault();
                insertMention(mentionMatches[mentionIndex]);
                return;
              }
              if (event.key === "Escape") {
                event.preventDefault();
                setMention(null);
                return;
              }
            }
            if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
              event.preventDefault();
              if (canSend) onSend();
            }
            if (event.key === "ArrowUp" && !value && event.metaKey) {
              event.preventDefault();
            }
          }}
          placeholder={placeholder ?? t("composer.placeholder")}
          disabled={disabled}
        />
        <button
          type="button"
          onClick={toggleMic}
          disabled={disabled}
          className={cn("sb-composer-btn", listening && "!text-[var(--sb-danger)]")}
          aria-label={listening ? t("composer.stopVoiceInput") : t("composer.voiceInput")}
          title={listening ? t("composer.stopVoiceInput") : t("composer.voiceInput")}
        >
          {listening ? <MicOff className="h-[18px] w-[18px]" /> : <Mic className="h-[18px] w-[18px]" />}
        </button>
        {busy && (
          <button
            type="button"
            onClick={onStop}
            className={hasContent ? "sb-composer-btn" : "sb-send-btn"}
            aria-label={t("composer.stop")}
            title={t("composer.stop")}
          >
            <Square className="h-3 w-3 fill-current" />
          </button>
        )}
        {busy && !hasContent ? null : hasContent ? (
          <button
            type="button"
            onClick={onSend}
            disabled={!canSend}
            className="sb-send-btn"
            aria-label={busy ? t("composer.sendInterjection") : t("composer.send")}
            title={busy ? t("composer.sendInterjection") : t("composer.send")}
          >
            <ArrowUp className="h-[18px] w-[18px]" strokeWidth={2.2} />
          </button>
        ) : (
          <button
            type="button"
            onClick={onOpenVoice}
            className="sb-voice-btn"
            aria-label={t("composer.voiceChat")}
            title={t("composer.voiceChat")}
          >
            <AudioLines className="h-[18px] w-[18px]" strokeWidth={2} />
          </button>
        )}
      </div>
      {busy && onSideQuestion && !/^\/side(\s|$)/i.test(value) && (
        <div className="px-4 pt-1.5 text-[11px] leading-[16px] text-[var(--sb-text-3)]" data-side-hint>
          {t("composer.sideHintLead")}{" "}
          <button
            type="button"
            className="underline decoration-[var(--sb-border-strong)] underline-offset-2 transition hover:text-[var(--sb-text)]"
            onClick={() => {
              onChange(value.trim() ? `/side ${value.replace(/^\s+/, "")}` : "/side ");
              window.requestAnimationFrame(() => {
                const el = textareaRef.current;
                if (!el) return;
                el.focus();
                el.setSelectionRange(el.value.length, el.value.length);
              });
            }}
          >
            {t("composer.sideHintAction")}
          </button>
        </div>
      )}
      {speechError && (
        <div className="px-4 pt-1.5 text-[11px] text-[var(--sb-danger)]">{speechError}</div>
      )}
      <Menu
        open={plusAnchor !== null}
        anchor={plusAnchor}
        items={plusItems}
        onClose={() => setPlusAnchor(null)}
        align="left"
        minWidth={200}
      />
      <input ref={fileInputRef} type="file" multiple className="hidden" onChange={handleFile} />
      <CaptureOverlay
        open={captureOpen}
        onClose={() => setCaptureOpen(false)}
        onInsert={appendToComposer}
        onSaved={() => undefined}
      />
    </div>
  );
}
