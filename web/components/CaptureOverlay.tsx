"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Loader2, MonitorPlay, Square, X } from "lucide-react";

import { api } from "@/lib/api";
import { useToast } from "./ui/Toast";

const MAX_FRAMES = 8;
const MAX_SECONDS = 600;
const FRAME_INTERVAL_MS = 1500;

export function CaptureOverlay({
  open,
  onClose,
  onInsert,
  onSaved,
}: {
  open: boolean;
  onClose: () => void;
  onInsert: (text: string) => void;
  onSaved: () => void;
}) {
  const { toast } = useToast();
  const videoRef = useRef<HTMLVideoElement>(null);
  const streamRef = useRef<MediaStream | null>(null);
  const framesRef = useRef<string[]>([]);
  const timerRef = useRef<number | null>(null);
  const frameTimerRef = useRef<number | null>(null);
  const startedRef = useRef(0);
  const secondsRef = useRef(0);
  const [recording, setRecording] = useState(false);
  const [seconds, setSeconds] = useState(0);
  const [frameCount, setFrameCount] = useState(0);
  const [description, setDescription] = useState("");
  const [analyzing, setAnalyzing] = useState(false);
  const [draft, setDraft] = useState("");
  const [error, setError] = useState<string | null>(null);

  const clearTimers = useCallback(() => {
    if (timerRef.current !== null) window.clearInterval(timerRef.current);
    if (frameTimerRef.current !== null) window.clearInterval(frameTimerRef.current);
    timerRef.current = null;
    frameTimerRef.current = null;
  }, []);

  const stopStream = useCallback(() => {
    streamRef.current?.getTracks().forEach((track) => track.stop());
    streamRef.current = null;
    if (videoRef.current) videoRef.current.srcObject = null;
  }, []);

  const reset = useCallback(() => {
    clearTimers();
    stopStream();
    framesRef.current = [];
    startedRef.current = 0;
    secondsRef.current = 0;
    setRecording(false);
    setSeconds(0);
    setFrameCount(0);
    setAnalyzing(false);
    setDraft("");
    setError(null);
    setDescription("");
  }, [clearTimers, stopStream]);

  useEffect(() => {
    if (!open) reset();
    return () => {
      if (!open) return;
      clearTimers();
      stopStream();
    };
  }, [open, reset, clearTimers, stopStream]);

  const captureFrame = useCallback(() => {
    const video = videoRef.current;
    if (!video || video.videoWidth === 0 || video.videoHeight === 0) return;
    const scale = Math.min(1, 1280 / video.videoWidth);
    const canvas = document.createElement("canvas");
    canvas.width = Math.round(video.videoWidth * scale);
    canvas.height = Math.round(video.videoHeight * scale);
    const context = canvas.getContext("2d");
    if (!context) return;
    context.drawImage(video, 0, 0, canvas.width, canvas.height);
    if (framesRef.current.length >= MAX_FRAMES) return;
    framesRef.current.push(canvas.toDataURL("image/jpeg", 0.7));
    setFrameCount(framesRef.current.length);
  }, []);

  const analyze = useCallback(async () => {
    if (framesRef.current.length === 0) {
      setError("没有捕获到画面，请重试。");
      return;
    }
    setAnalyzing(true);
    setError(null);
    try {
      const result = await api.analyzeCapture({
        duration: secondsRef.current,
        description: description.trim(),
        frames: framesRef.current,
      });
      setDraft(result.content);
    } catch (err) {
      setError(err instanceof Error ? err.message : "生成 Skill 草稿失败");
    } finally {
      setAnalyzing(false);
    }
  }, [description]);

  const stopRecording = useCallback(() => {
    if (!recording) return;
    clearTimers();
    captureFrame();
    stopStream();
    setRecording(false);
    void analyze();
  }, [analyze, captureFrame, clearTimers, recording, stopStream]);

  const startRecording = useCallback(async () => {
    setError(null);
    if (!navigator.mediaDevices?.getDisplayMedia) {
      setError("当前页面不是安全上下文（需要 HTTPS）或系统不支持屏幕捕获。");
      return;
    }
    try {
      const stream = await navigator.mediaDevices.getDisplayMedia({
        video: { frameRate: 4 },
        audio: false,
      });
      streamRef.current = stream;
      stream.getVideoTracks()[0]?.addEventListener("ended", () => {
        if (streamRef.current) stopRecording();
      });
      const video = videoRef.current;
      if (video) {
        video.srcObject = stream;
        await video.play().catch(() => undefined);
      }
      framesRef.current = [];
      setFrameCount(0);
      startedRef.current = Date.now();
      secondsRef.current = 0;
      setSeconds(0);
      setRecording(true);
      window.setTimeout(captureFrame, 300);
      timerRef.current = window.setInterval(() => {
        const elapsed = Math.floor((Date.now() - startedRef.current) / 1000);
        secondsRef.current = elapsed;
        setSeconds(elapsed);
        if (elapsed >= MAX_SECONDS) stopRecording();
      }, 1000);
      frameTimerRef.current = window.setInterval(captureFrame, FRAME_INTERVAL_MS);
    } catch (err) {
      setError(err instanceof Error ? err.message : "无法开始屏幕录制");
      stopStream();
    }
  }, [captureFrame, stopRecording, stopStream]);

  const saveSkill = async () => {
    try {
      await api.createSkill({
        name: description.trim() || "录屏任务",
        description: `屏幕录制 ${secondsRef.current} 秒后生成的 Skill 草稿。`,
        content: draft,
      });
      toast("已保存为 Skill 草稿", "success");
      onSaved();
      onClose();
    } catch (err) {
      toast(err instanceof Error ? err.message : "保存 Skill 失败", "danger");
    }
  };

  if (!open || typeof document === "undefined") return null;

  return createPortal(
    <div className="fixed inset-0 z-[100] flex items-center justify-center bg-black/50 p-6 backdrop-blur-sm">
      <div className="flex max-h-[88vh] w-full max-w-[720px] flex-col overflow-hidden rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] shadow-[var(--sb-shadow-modal)]">
        <div className="flex items-center justify-between border-b border-[var(--sb-border)] px-5 py-4">
          <div>
            <div className="font-mono text-[9px] uppercase tracking-[0.22em] text-[var(--sb-text-2)]">
              capture
            </div>
            <h2 className="mt-1 text-[17px] font-medium">教它做一项任务</h2>
          </div>
          <button
            type="button"
            onClick={() => {
              reset();
              onClose();
            }}
            className="sb-icon-btn"
            data-size="sm"
            aria-label="关闭录制"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="flex-1 overflow-y-auto p-5">
          {draft ? (
            <div>
              <div className="mb-2 text-[13px] text-[var(--sb-text-2)]">Skill 草稿</div>
              <textarea
                value={draft}
                onChange={(event) => setDraft(event.target.value)}
                className="field min-h-[320px] w-full resize-y font-mono text-[12px] leading-[18px]"
                aria-label="Skill 草稿"
              />
            </div>
          ) : analyzing ? (
            <div className="flex flex-col items-center justify-center py-20 text-[13px] text-[var(--sb-text-2)]">
              <Loader2 className="mb-3 h-6 w-6 animate-spin" />
              正在分析关键帧并生成 Skill 草稿…
            </div>
          ) : (
            <div>
              <div className="flex items-center gap-2 text-[14px] font-medium">
                <MonitorPlay className="h-4 w-4" />
                录制屏幕操作
              </div>
              <p className="mt-1.5 text-[12.5px] leading-[20px] text-[var(--sb-text-2)]">
                开始后 superbot 会每隔 1.5 秒截取一帧（最多 8 帧），最长录制 10 分钟。请先完成一次完整的操作流程，再停止录制。
              </p>
              <input
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                placeholder="这项任务是做什么的？（可选）"
                className="field mt-3 w-full"
                disabled={recording}
              />
              <div className="relative mt-4 overflow-hidden rounded-xl border border-[var(--sb-border)] bg-black/90">
                <video
                  ref={videoRef}
                  muted
                  playsInline
                  className="aspect-video w-full object-contain"
                />
                {recording && (
                  <div className="absolute left-3 top-3 flex items-center gap-2 rounded-full bg-black/65 px-2.5 py-1 text-[11px] text-white">
                    <span className="h-2 w-2 animate-pulse rounded-full bg-red-500" />
                    {Math.floor(seconds / 60)}:{String(seconds % 60).padStart(2, "0")} · {frameCount} 帧
                  </div>
                )}
              </div>
              {error && (
                <div className="mt-3 rounded-lg bg-[var(--sb-danger-soft)] px-3 py-2 text-[12px] text-[var(--sb-danger)]">
                  {error}
                </div>
              )}
              <div className="mt-4 flex items-center justify-end gap-2">
                {recording ? (
                  <button
                    type="button"
                    onClick={stopRecording}
                    className="sb-btn"
                    data-variant="primary"
                  >
                    <Square className="h-3.5 w-3.5 fill-current" />
                    停止并生成草稿
                  </button>
                ) : (
                  <button
                    type="button"
                    onClick={() => void startRecording()}
                    className="sb-btn"
                    data-variant="primary"
                  >
                    <MonitorPlay className="h-3.5 w-3.5" />
                    开始录制
                  </button>
                )}
              </div>
            </div>
          )}
        </div>

        {draft && (
          <div className="flex items-center justify-between gap-2 border-t border-[var(--sb-border)] px-5 py-3">
            <button
              type="button"
              onClick={reset}
              className="sb-btn"
              data-variant="secondary"
              data-size="sm"
            >
              重新录制
            </button>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => {
                  onInsert(draft);
                  onClose();
                }}
                className="sb-btn"
                data-variant="secondary"
                data-size="sm"
              >
                插入到输入框
              </button>
              <button
                type="button"
                onClick={() => void saveSkill()}
                className="sb-btn"
                data-variant="primary"
                data-size="sm"
              >
                保存为 Skill
              </button>
            </div>
          </div>
        )}
      </div>
    </div>,
    document.body,
  );
}
