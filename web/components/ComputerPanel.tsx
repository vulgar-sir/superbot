"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Camera, History, Loader2, Monitor, Power, RefreshCw, RotateCcw, Shrink, Trash2 } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import type { Bot, ComputerInfo } from "@/lib/types";
import { BotAvatar } from "./BotAvatar";
import { StatusPill } from "./StatusPill";
import { useConfirm } from "./ui/Confirm";
import { useToast } from "./ui/Toast";

export function ComputerPanel({ bot, onClose }: { bot: Bot; onClose: () => void }) {
  const { toast } = useToast();
  const { confirm, confirmElement } = useConfirm();
  const [info, setInfo] = useState<ComputerInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [working, setWorking] = useState(false);
  const [snapshots, setSnapshots] = useState<Array<{ id: string; label: string; size_bytes: number; created_at: string }>>([]);
  const [snapshotsOpen, setSnapshotsOpen] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const pollRef = useRef<number | null>(null);

  const refresh = useCallback(async () => {
    try {
      const response = await api.getComputer(bot.id);
      setInfo(response.computer);
      setError(null);
      return response.computer;
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "无法读取电脑状态");
      return null;
    } finally {
      setLoading(false);
    }
  }, [bot.id]);

  useEffect(() => {
    void refresh();
    return () => {
      if (pollRef.current !== null) window.clearInterval(pollRef.current);
    };
  }, [refresh]);

  useEffect(() => {
    if (!info) return;
    const actionPending =
      info.last_action?.status === "pending" || info.last_action?.status === "running";
    const shouldPoll =
      actionPending || info.status === "starting" || (info.desired_state === "running" && info.status !== "running");
    if (!shouldPoll) {
      if (pollRef.current !== null) {
        window.clearInterval(pollRef.current);
        pollRef.current = null;
      }
      return;
    }
    if (pollRef.current === null) {
      pollRef.current = window.setInterval(() => {
        void refresh();
      }, 2000);
    }
  }, [info, refresh]);

  const start = useCallback(async () => {
    setWorking(true);
    setError(null);
    try {
      await api.startComputer(bot.id);
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "启动失败");
    } finally {
      setWorking(false);
    }
  }, [bot.id, refresh]);

  const stop = useCallback(async () => {
    setWorking(true);
    setError(null);
    try {
      await api.stopComputer(bot.id);
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "停止失败");
    } finally {
      setWorking(false);
    }
  }, [bot.id, refresh]);

  const update = useCallback(async () => {
    setWorking(true);
    setError(null);
    try {
      await api.updateComputer(bot.id);
      toast("已提交更新：会重建容器，工作区文件保留", "success");
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "更新失败");
    } finally {
      setWorking(false);
    }
  }, [bot.id, refresh, toast]);

  const reset = useCallback(async () => {
    const ok = await confirm({
      title: "重置 Bot 的电脑？",
      description: "会清空 Bot 的工作区文件并重建容器，此操作不可恢复。",
      confirmLabel: "重置",
      danger: true,
    });
    if (!ok) return;
    setWorking(true);
    setError(null);
    try {
      await api.resetComputer(bot.id);
      toast("已提交重置，正在清空工作区并重建电脑", "success");
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "重置失败");
    } finally {
      setWorking(false);
    }
  }, [bot.id, confirm, refresh, toast]);

  const loadSnapshots = useCallback(async () => {
    try {
      const result = await api.listComputerSnapshots(bot.id);
      setSnapshots(result.snapshots);
    } catch {
      setSnapshots([]);
    }
  }, [bot.id]);

  const createSnapshot = useCallback(async () => {
    setWorking(true);
    try {
      await api.createComputerSnapshot(bot.id);
      toast("已提交快照，稍后可在快照列表查看", "success");
      window.setTimeout(() => void loadSnapshots(), 2500);
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "创建快照失败", "danger");
    } finally {
      setWorking(false);
    }
  }, [bot.id, loadSnapshots, toast]);

  const restoreSnapshot = useCallback(
    async (snapshotID: string) => {
      const ok = await confirm({
        title: "恢复到该快照？",
        description: "会清空当前工作区并重建电脑，快照之后产生的文件会丢失。",
        confirmLabel: "恢复",
        danger: true,
      });
      if (!ok) return;
      setWorking(true);
      try {
        await api.restoreComputerSnapshot(bot.id, snapshotID);
        toast("已提交恢复，正在重建电脑", "success");
        setSnapshotsOpen(false);
        window.setTimeout(() => void refresh(), 2500);
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "恢复失败", "danger");
      } finally {
        setWorking(false);
      }
    },
    [bot.id, confirm, refresh, toast],
  );

  const deleteSnapshot = useCallback(
    async (snapshotID: string) => {
      try {
        await api.deleteComputerSnapshot(bot.id, snapshotID);
        setSnapshots((prev) => prev.filter((item) => item.id !== snapshotID));
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "删除快照失败", "danger");
      }
    },
    [bot.id, toast],
  );

  const running = info?.status === "running";
  const apiOrigin = process.env.NEXT_PUBLIC_API_ORIGIN ?? "";
  const isLocalPage =
    typeof window !== "undefined" &&
    (window.location.hostname === "localhost" || window.location.hostname === "127.0.0.1");
  const vncSrc = info?.vnc_url
    ? apiOrigin && isLocalPage && info.vnc_url.startsWith("/")
      ? `${apiOrigin}${info.vnc_url}`
      : info.vnc_url
    : "";

  if (typeof document === "undefined") return null;

  return createPortal(
    <div className="fixed inset-0 z-[70] flex flex-col bg-[#171717] text-white">
      <div className="pointer-events-none absolute left-0 right-0 top-0 z-10 flex items-center justify-between p-3">
        <div className={running ? "pointer-events-auto flex items-center gap-2 rounded-full bg-black/55 px-3 py-1.5 backdrop-blur" : "hidden"}>
          <BotAvatar name={bot.name} id={bot.id} size={28} shape={bot.avatar_shape} color={bot.avatar_color} avatar={bot.avatar} />
          <span className="text-[13px] font-medium">{bot.name} 的电脑</span>
          <StatusPill tone={running ? "live" : info?.status === "error" ? "error" : "idle"} pulse={running}>
            {info?.status ?? "loading"}
          </StatusPill>
          <div className="ml-2 flex items-center gap-1">
            <button
              type="button"
              onClick={() => void update()}
              disabled={working}
              className="rounded-full p-1.5 text-white/70 transition hover:bg-white/10 hover:text-white disabled:opacity-40"
              title="更新（保留文件）"
              aria-label="更新"
            >
              <RefreshCw className="h-4 w-4" />
            </button>
            <button
              type="button"
              onClick={() => void reset()}
              disabled={working}
              className="rounded-full p-1.5 text-white/70 transition hover:bg-white/10 hover:text-white disabled:opacity-40"
              title="重置（清空工作区）"
              aria-label="重置"
            >
              <RotateCcw className="h-4 w-4" />
            </button>
            {running && (
              <button
                type="button"
                onClick={() => void stop()}
                disabled={working}
                className="rounded-full p-1.5 text-white/70 transition hover:bg-white/10 hover:text-white disabled:opacity-40"
                title="停止电脑"
                aria-label="停止电脑"
              >
                <Power className="h-4 w-4" />
              </button>
            )}
            <button
              type="button"
              onClick={() => void createSnapshot()}
              disabled={working}
              className="rounded-full p-1.5 text-white/70 transition hover:bg-white/10 hover:text-white disabled:opacity-40"
              title="创建快照"
              aria-label="创建快照"
            >
              <Camera className="h-4 w-4" />
            </button>
            <button
              type="button"
              onClick={() => {
                setSnapshotsOpen((prev) => !prev);
                void loadSnapshots();
              }}
              className="rounded-full p-1.5 text-white/70 transition hover:bg-white/10 hover:text-white"
              title="快照"
              aria-label="快照"
            >
              <History className="h-4 w-4" />
            </button>
            <button
              type="button"
              onClick={() => void refresh()}
              className="rounded-full p-1.5 text-white/70 transition hover:bg-white/10 hover:text-white"
              title="刷新"
              aria-label="刷新"
            >
              <RefreshCw className="h-4 w-4" />
            </button>
          </div>
        </div>
        <button
          type="button"
          onClick={onClose}
          className="pointer-events-auto flex h-10 w-10 items-center justify-center rounded-full bg-black/55 text-white/80 backdrop-blur transition hover:bg-black/70 hover:text-white"
          aria-label="收起"
          title="收起"
        >
          <Shrink className="h-5 w-5" />
        </button>
      </div>

      {snapshotsOpen && (
        <div className="absolute right-3 top-16 z-20 w-[320px] overflow-hidden rounded-xl border border-white/10 bg-black/80 text-white shadow-xl backdrop-blur">
          <div className="flex items-center justify-between border-b border-white/10 px-3 py-2 text-[12px]">
            <span>工作区快照（{snapshots.length}）</span>
            <button type="button" onClick={() => setSnapshotsOpen(false)} className="text-white/60 hover:text-white">关闭</button>
          </div>
          <div className="max-h-[260px] overflow-y-auto p-2">
            {snapshots.length === 0 ? (
              <div className="px-2 py-6 text-center text-[12px] text-white/50">还没有快照</div>
            ) : (
              snapshots.map((item) => (
                <div key={item.id} className="flex items-center gap-2 rounded-lg px-2 py-1.5 text-[12px] hover:bg-white/10">
                  <div className="min-w-0 flex-1">
                    <div className="truncate">{item.label}</div>
                    <div className="text-[10px] text-white/50">
                      {(item.size_bytes / 1024).toFixed(0)} KB
                    </div>
                  </div>
                  <button type="button" onClick={() => void restoreSnapshot(item.id)} className="rounded px-1.5 py-0.5 text-white/70 hover:bg-white/10 hover:text-white">恢复</button>
                  <button type="button" onClick={() => void deleteSnapshot(item.id)} aria-label="删除快照" className="rounded p-0.5 text-white/50 hover:bg-white/10 hover:text-white"><Trash2 className="h-3.5 w-3.5" /></button>
                </div>
              ))
            )}
          </div>
        </div>
      )}

      {loading ? (
        <div className="flex flex-1 items-center justify-center">
          <Loader2 className="sb-spin h-6 w-6 text-white/60" />
        </div>
      ) : running && info?.vnc_url ? (
        <iframe
          title="Bot computer live view"
          src={vncSrc}
          className="h-full w-full flex-1 border-0 bg-black"
          allow="clipboard-read; clipboard-write"
        />
      ) : (
        <div className="flex flex-1 flex-col items-center justify-center px-6 text-center">
          {info?.status === "starting" ? (
            <>
              <Monitor className="h-12 w-12 text-white/25" />
              <h3 className="mt-4 text-[15px] font-medium">电脑正在启动</h3>
              <p className="mt-1.5 max-w-md text-[12px] leading-[18px] text-white/50">
                正在启动 Chromium、Xvfb 和 noVNC，通常需要几秒钟。
              </p>
              <div className="mt-5 flex items-center gap-2 text-[12px] text-white/60">
                <Loader2 className="sb-spin h-4 w-4" />
                正在启动…
              </div>
            </>
          ) : (
            <button
              type="button"
              onClick={() => void start()}
              disabled={working}
              className="text-[15px] font-medium text-white/90 transition hover:text-white disabled:opacity-50"
              title="点击继续"
            >
              你离开期间屏幕已暂停
            </button>
          )}
          {error && (
            <div className="mt-3 rounded-lg bg-red-500/15 px-3 py-2 text-[12px] text-red-300">
              {error}
            </div>
          )}
        </div>
      )}
      {confirmElement}
    </div>,
    document.body,
  );
}
