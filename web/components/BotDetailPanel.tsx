"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import {
  Activity as ActivityIcon,
  ArrowLeftRight,
  ChevronDown,
  ChevronRight,
  Clock,
  Download,
  ExternalLink,
  FileText,
  Folder,
  Image as ImageIcon,
  Loader2,
  MemoryStick,
  MessageSquare,
  Monitor,
  MoreHorizontal,
  Pencil,
  Plus,
  Sparkles,
  Trash2,
  Upload,
  Users,
  X,
} from "lucide-react";

import { api, ApiError } from "@/lib/api";
import { botProfilePayload } from "@/lib/bot";
import { useToast } from "./ui/Toast";
import { useConfirm } from "./ui/Confirm";
import { Menu, type MenuItem } from "./ui/Menu";
import { Modal } from "./ui/Modal";
import { BotAvatar } from "./BotAvatar";
import { BotAvatarPicker } from "./BotAvatarPicker";
import { GOAL_SECTION_ID } from "./GoalPanel";
import { useLanguage } from "./LanguageProvider";
import { Markdown } from "./Markdown";
import { cn, timeAgo } from "@/lib/format";
import type {
  Activity,
  Bot,
  BotRoutine,
  Conversation,
  BotFileEntry,
  LibraryItem,
  LibraryItemDetail,
  SubagentTask,
  Team,
} from "@/lib/types";

type Props = {
  bot?: Bot;
  team?: Team;
  bots: Bot[];
  conversation: Conversation | null;
  isPrimary: boolean;
  onClose: () => void;
  onOpenComputer: () => void;
  onOpenRoutines: (bot: Bot) => void;
  onOpenExtensions: (provider?: string) => void;
  onOpenBot: (bot: Bot) => void;
  onOpenInfo: (bot: Bot) => void;
  onOpenActivity: (activity: Activity) => void;
  onOpenTeams: () => void;
  onRefresh: () => Promise<void> | void;
  /** Group goal section, shown above the member list of a group. */
  goalSection?: React.ReactNode;
  /** Bump to switch to the details tab and bring the goal section into view. */
  goalFocusSignal?: number;
};

export function BotDetailPanel({
  bot,
  team,
  bots,
  conversation,
  isPrimary,
  onClose,
  onOpenComputer,
  onOpenRoutines,
  onOpenExtensions,
  onOpenBot,
  onOpenInfo,
  onOpenActivity,
  onOpenTeams,
  onRefresh,
  goalSection,
  goalFocusSignal = 0,
}: Props) {
  const { t } = useLanguage();
  const { toast } = useToast();
  const { confirm, confirmElement } = useConfirm();
  const [tab, setTab] = useState<"detail" | "library" | "files" | "computer">("detail");
  const [avatarOpen, setAvatarOpen] = useState(false);
  const [avatarSaving, setAvatarSaving] = useState(false);

  useEffect(() => {
    setAvatarOpen(false);
  }, [bot?.id]);
  const [width, setWidth] = useState(353);
  const [routines, setRoutines] = useState<BotRoutine[]>([]);
  const [routinesLoading, setRoutinesLoading] = useState(false);
  const [items, setItems] = useState<LibraryItem[]>([]);
  const [libraryLoading, setLibraryLoading] = useState(false);
  const [activities, setActivities] = useState<Activity[]>([]);
  const [activitiesLoading, setActivitiesLoading] = useState(false);
  const [subagents, setSubagents] = useState<SubagentTask[]>([]);
  const [detail, setDetail] = useState<LibraryItemDetail | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [libraryUploading, setLibraryUploading] = useState(false);
  const [files, setFiles] = useState<BotFileEntry[]>([]);
  const [filePath, setFilePath] = useState("");
  const [filesLoading, setFilesLoading] = useState(false);
  const libraryFileRef = useRef<HTMLInputElement>(null);
  const [itemMenu, setItemMenu] = useState<{ item: LibraryItem; rect: DOMRect } | null>(null);
  const [renaming, setRenaming] = useState(false);
  const [name, setName] = useState(bot?.name ?? "");
  const [savingName, setSavingName] = useState(false);
  const [voiceName, setVoiceName] = useState("");
  const [voiceRate, setVoiceRate] = useState("1");
  const [voiceLanguage, setVoiceLanguage] = useState("auto");
  const [voices, setVoices] = useState<SpeechSynthesisVoice[]>([]);
  const [voiceMenu, setVoiceMenu] = useState<{
    kind: "voice" | "rate" | "language";
    rect: DOMRect;
  } | null>(null);
  const resizingRef = useRef(false);

  useEffect(() => {
    setTab("detail");
    setName(bot?.name ?? "");
    setRenaming(false);
  }, [bot?.id, bot?.name]);

  useEffect(() => {
    setItems([]);
    setActivities([]);
    setDetail(null);
  }, [bot?.id, team?.id]);

  useEffect(() => {
    if (!goalFocusSignal) return;
    setTab("detail");
    const frame = window.requestAnimationFrame(() => {
      document.getElementById(GOAL_SECTION_ID)?.scrollIntoView({ behavior: "smooth", block: "start" });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [goalFocusSignal]);

  useEffect(() => {
    try {
      const saved = window.localStorage.getItem("superbot-info-width");
      if (saved) setWidth(Math.min(480, Math.max(280, Number(saved) || 353)));
    } catch {
      // ignore
    }
  }, []);

  useEffect(() => {
    try {
      setVoiceName(window.localStorage.getItem("superbot-voice") ?? "");
      setVoiceRate(window.localStorage.getItem("superbot-voice-rate") ?? "1");
      setVoiceLanguage(window.localStorage.getItem("superbot-voice-language") ?? "auto");
    } catch {
      // ignore
    }
    if (typeof window === "undefined" || !window.speechSynthesis) return;
    const load = () => setVoices(window.speechSynthesis.getVoices());
    load();
    window.speechSynthesis.addEventListener("voiceschanged", load);
    return () => window.speechSynthesis.removeEventListener("voiceschanged", load);
  }, []);

  const persistVoice = (key: string, value: string) => {
    try {
      window.localStorage.setItem(key, value);
    } catch {
      // ignore
    }
  };

  useEffect(() => {
    const onMove = (event: MouseEvent) => {
      if (!resizingRef.current) return;
      const next = Math.min(480, Math.max(280, window.innerWidth - event.clientX));
      setWidth(next);
    };
    const onUp = () => {
      if (!resizingRef.current) return;
      resizingRef.current = false;
      document.body.style.cursor = "";
      setWidth((current) => {
        try {
          window.localStorage.setItem("superbot-info-width", String(current));
        } catch {
          // ignore
        }
        return current;
      });
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    return () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    };
  }, []);

  const loadRoutines = useCallback(async () => {
    if (!bot) return;
    setRoutinesLoading(true);
    try {
      const result = await api.listBotRoutines(bot.id);
      setRoutines(result.routines);
    } catch {
      // keep quiet; panel still useful
    } finally {
      setRoutinesLoading(false);
    }
  }, [bot]);

  const loadLibrary = useCallback(async () => {
    if (team) {
      setLibraryLoading(true);
      try {
        // A group chat has no library of its own; merge the real libraries of
        // its member Bots so the panel reflects what the team has saved.
        const results = await Promise.allSettled(
          team.bots.map((entry) => api.listBotLibrary(entry.bot.id)),
        );
        const merged: LibraryItem[] = [];
        let failed = 0;
        for (const result of results) {
          if (result.status === "fulfilled") merged.push(...result.value.items);
          else failed += 1;
        }
        if (failed > 0) {
          toast(
            failed === results.length ? "加载群资料库失败" : "部分成员的资料库加载失败",
            "danger",
          );
        }
        const seen = new Set<string>();
        const deduped = merged.filter((item) => {
          if (seen.has(item.id)) return false;
          seen.add(item.id);
          return true;
        });
        deduped.sort(
          (a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
        );
        setItems(deduped);
      } finally {
        setLibraryLoading(false);
      }
      return;
    }
    if (!bot) {
      setItems([]);
      return;
    }
    setLibraryLoading(true);
    try {
      const result = await api.listBotLibrary(bot.id);
      setItems(result.items);
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "加载资料库失败", "danger");
    } finally {
      setLibraryLoading(false);
    }
  }, [bot, team, toast]);

  const loadActivities = useCallback(async () => {
    if (!bot) {
      setActivities([]);
      return;
    }
    setActivitiesLoading(true);
    try {
      const result = await api.listActivities(bot.id);
      setActivities(result.activities);
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "加载动态失败", "danger");
    } finally {
      setActivitiesLoading(false);
    }
  }, [bot, toast]);

  const loadSubagents = useCallback(async () => {
    if (!conversation) {
      setSubagents([]);
      return;
    }
    try {
      const result = await api.listSubagents(conversation.id);
      setSubagents(result.subagents);
    } catch {
      // subagents are optional; do not interrupt the info pane
    }
  }, [conversation]);

  useEffect(() => {
    if (!conversation || tab !== "detail") return;
    void loadSubagents();
    const timer = window.setInterval(() => void loadSubagents(), 15_000);
    return () => window.clearInterval(timer);
  }, [conversation, tab, loadSubagents]);

  useEffect(() => {
    if (!bot && !team) return;
    void loadRoutines();
    void loadLibrary();
  }, [bot, team, loadRoutines, loadLibrary]);

  useEffect(() => {
    if (!bot || tab !== "detail") return;
    void loadActivities();
  }, [bot, tab, loadActivities]);

  const groupedItems = useMemo(() => {
    const groups: Array<{ label: string; items: LibraryItem[] }> = [];
    const today = new Date();
    const startOfToday = new Date(today.getFullYear(), today.getMonth(), today.getDate()).getTime();
    for (const item of items) {
      const time = new Date(item.created_at).getTime();
      const label =
        time >= startOfToday
          ? "今天"
          : time >= startOfToday - 24 * 3600 * 1000
            ? "昨天"
            : time >= startOfToday - 7 * 24 * 3600 * 1000
              ? "本周"
              : "更早";
      let group = groups.find((entry) => entry.label === label);
      if (!group) {
        group = { label, items: [] };
        groups.push(group);
      }
      group.items.push(item);
    }
    return groups;
  }, [items]);

  const botNameByID = useMemo(() => {
    const map = new Map<string, string>();
    for (const entry of team?.bots ?? []) map.set(entry.bot.id, entry.bot.name);
    if (bot) map.set(bot.id, bot.name);
    return map;
  }, [bot, team]);

  const saveAvatar = useCallback(
    async (shape: string, color: string) => {
      if (!bot) return;
      setAvatarSaving(true);
      try {
        await api.updateBotProfile(bot.id, {
          ...botProfilePayload(bot),
          avatar_shape: shape,
          avatar_color: color,
        });
        await onRefresh();
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "保存头像失败", "danger");
      } finally {
        setAvatarSaving(false);
      }
    },
    [bot, onRefresh, toast],
  );

  const saveName = useCallback(async () => {
    if (!bot) return;
    const trimmed = name.trim();
    if (!trimmed || trimmed === bot.name) {
      setRenaming(false);
      setName(bot.name);
      return;
    }
    setSavingName(true);
    try {
      await api.updateBotProfile(bot.id, { ...botProfilePayload(bot), name: trimmed });
      await onRefresh();
      setRenaming(false);
      toast("已更新名称", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "保存失败", "danger");
    } finally {
      setSavingName(false);
    }
  }, [bot, name, onRefresh, toast]);

  const toggleRoutine = useCallback(async (routine: BotRoutine) => {
    const running = routine.enabled && !routine.paused;
    try {
      const result = await api.updateBotRoutine(
        routine.id,
        running ? { paused: true } : { enabled: true, paused: false },
      );
      setRoutines((prev) => prev.map((item) => (item.id === routine.id ? result.routine : item)));
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "更新例程失败", "danger");
    }
  }, [toast]);

  const removeItem = useCallback(
    async (item: LibraryItem) => {
      const ok = await confirm({
        title: `删除「${item.title}」？`,
        description: "删除后无法恢复。",
        confirmLabel: "删除",
        danger: true,
      });
      if (!ok) return;
      try {
        await api.deleteLibraryItem(item.id);
        setItems((prev) => prev.filter((entry) => entry.id !== item.id));
        toast("已删除", "success");
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "删除失败", "danger");
      }
    },
    [confirm, toast],
  );

  const openItem = useCallback(async (item: LibraryItem) => {
    setDetailLoading(true);
    try {
      setDetail(await api.getLibraryItem(item.id));
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "加载资料失败", "danger");
    } finally {
      setDetailLoading(false);
    }
  }, [toast]);

  useEffect(() => {
    if (tab !== "files" || !bot) return;
    let cancelled = false;
    setFilesLoading(true);
    api
      .listBotFiles(bot.id, filePath)
      .then((result) => {
        if (!cancelled) setFiles(result.files);
      })
      .catch(() => undefined)
      .finally(() => {
        if (!cancelled) setFilesLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [tab, bot, filePath]);

  const uploadLibraryFile = async (file: File) => {
    const target = bot ?? team?.bots[0]?.bot;
    if (!target) return;
    setLibraryUploading(true);
    try {
      const item = await api.uploadLibraryFile(target.id, file);
      setItems((prev) => [item, ...prev]);
      toast("文件已加入资料库", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : t("detail.uploadFailed"), "danger");
    } finally {
      setLibraryUploading(false);
    }
  };

  const identityName = team?.name ?? bot?.name ?? t("detail.notSelected");
  const identityBots = team ? team.bots.map((entry) => entry.bot) : bot ? [bot] : [];

  return (
    <aside className="sb-info" style={{ width, minWidth: width }} aria-label={t("detail.tabDetails")}>
      <div
        className="sb-resize-handle"
        onMouseDown={(event) => {
          event.preventDefault();
          resizingRef.current = true;
          document.body.style.cursor = "col-resize";
        }}
        aria-hidden
      />
      <div className="sb-info-header">
        <button type="button" className="sb-icon-btn" onClick={onClose} aria-label={t("detail.close")}>
          <X className="h-4 w-4" />
        </button>
      </div>
      {bot && (
        <BotAvatarPicker
          bot={bot}
          open={avatarOpen}
          saving={avatarSaving}
          onClose={() => setAvatarOpen(false)}
          onChange={(shape, color) => void saveAvatar(shape, color)}
          onAvatarChanged={() => onRefresh()}
        />
      )}

      <div className="sb-info-scroll no-scrollbar">
        <div className="flex flex-col items-center pt-2">
          {team ? (
            <button
              type="button"
              className="group/avatar relative flex -space-x-3"
              onClick={onOpenTeams}
              aria-label={t("detail.manageGroupAria")}
            >
              {identityBots.slice(0, 3).map((member) => (
                <BotAvatar
                  key={member.id}
                  name={member.name}
                  id={member.id}
                  size={56}
                  shape={member.avatar_shape}
                  color={member.avatar_color}
                  avatar={member.avatar}
                  className="ring-4 ring-[var(--sb-bg)]"
                />
              ))}
              <span className="pointer-events-none absolute inset-0 flex items-center justify-center rounded-full bg-black/40 text-white opacity-0 transition group-hover/avatar:opacity-100">
                <Users className="h-5 w-5" />
              </span>
            </button>
          ) : bot ? (
            <button
              type="button"
              className="group/avatar relative"
              onClick={() => setAvatarOpen((prev) => !prev)}
              aria-label={t("detail.editAvatar")}
            >
              <BotAvatar
                name={bot.name}
                id={bot.id}
                size={80}
                shape={bot.avatar_shape}
                color={bot.avatar_color}
                avatar={bot.avatar}
              />
              <span className="pointer-events-none absolute inset-0 flex items-center justify-center rounded-full bg-black/40 text-white opacity-0 transition group-hover/avatar:opacity-100">
                <Pencil className="h-5 w-5" />
              </span>
            </button>
          ) : (
            <div className="flex h-16 w-16 items-center justify-center rounded-2xl bg-[var(--sb-hover)] text-[var(--sb-text-3)]">
              <Users className="h-6 w-6" />
            </div>
          )}

          <div className="group/name mt-3 flex w-full items-center justify-center gap-1 px-4">
            {renaming && bot ? (
              <form
                className="flex w-full max-w-[240px] items-center gap-1"
                onSubmit={(event) => {
                  event.preventDefault();
                  void saveName();
                }}
              >
                <input
                  autoFocus
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  onBlur={() => void saveName()}
                  onKeyDown={(event) => {
                    if (event.key === "Escape") {
                      setRenaming(false);
                      setName(bot.name);
                    }
                  }}
                  className="h-8 min-w-0 flex-1 rounded-md border border-[var(--sb-border)] bg-[var(--sb-pane)] px-2 text-center text-[17px] font-medium outline-none focus:border-[var(--sb-accent)]"
                />
                {savingName && <Loader2 className="sb-spin h-4 w-4 text-[var(--sb-text-3)]" />}
              </form>
            ) : (
              <>
                <button
                  type="button"
                  onClick={() => bot && setRenaming(true)}
                  className="max-w-full truncate rounded-md px-2 py-0.5 text-[20px] font-medium leading-[28px] text-[var(--sb-text)] transition hover:bg-[var(--sb-hover)]"
                  title={bot ? t("detail.renameHint") : identityName}
                >
                  {identityName}
                </button>
                {bot && (
                  <button
                    type="button"
                    onClick={() => onOpenInfo(bot)}
                    className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-[var(--sb-text-4)] opacity-0 transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-text)] focus:opacity-100 group-hover/name:opacity-100"
                    aria-label={t("detail.editProfile")}
                    title={t("detail.editProfile")}
                  >
                    <Pencil className="h-3.5 w-3.5" />
                  </button>
                )}
              </>
            )}
          </div>
          {bot?.title && (
            <div className="mt-0.5 text-[12px] leading-[16px] text-[var(--sb-text-3)]">{bot.title}</div>
          )}

          <div className="sb-tabs mt-10">
            <button
              type="button"
              className="sb-tab"
              data-selected={tab === "detail" ? "true" : undefined}
              onClick={() => setTab("detail")}
            >
              {team ? t("detail.tabDetails") : t("detail.tabActivity")}
            </button>
            <button
              type="button"
              className="sb-tab"
              data-selected={tab === "library" ? "true" : undefined}
              onClick={() => setTab("library")}
            >
              {t("detail.tabLibrary")}
            </button>
            {!team && (
              <button
                type="button"
                className="sb-tab"
                data-selected={tab === "files" ? "true" : undefined}
                onClick={() => setTab("files")}
              >
                {t("detail.tabFiles")}
              </button>
            )}
            {team && (
              <button
                type="button"
                className="sb-tab"
                data-selected={tab === "computer" ? "true" : undefined}
                onClick={() => setTab("computer")}
              >
                电脑
              </button>
            )}
          </div>
        </div>

        {tab === "detail" ? (
          <div className="pb-4 pt-6">
            {team ? (
              <>
                {goalSection}
                <div className="sb-info-label">{t("detail.members")}</div>
                <div className="sb-card overflow-hidden">
                  {team.bots.map((entry, index) => (
                    <button
                      key={entry.bot.id}
                      type="button"
                      className={cn("sb-card-row", index > 0 && "border-t border-[var(--sb-border)]")}
                      onClick={() => onOpenBot(entry.bot)}
                    >
                      <BotAvatar
                        name={entry.bot.name}
                        id={entry.bot.id}
                        size={32}
                        shape={entry.bot.avatar_shape}
                        color={entry.bot.avatar_color}
                        avatar={entry.bot.avatar}
                      />
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-[14px] leading-[20px]">{entry.bot.name}</div>
                        <div className="truncate text-[12px] leading-[16px] text-[var(--sb-text-2)]">
                          {entry.bot.title || entry.bot.description || entry.role || t("detail.aiColleague")}
                        </div>
                      </div>
                      <ChevronRight className="h-4 w-4 text-[var(--sb-text-4)]" />
                    </button>
                  ))}
                </div>
                <div className="sb-info-label">{t("detail.group")}</div>
                <div className="sb-card">
                  <div className="sb-info-row">
                    <Users className="h-4 w-4 text-[var(--sb-text-2)]" />
                    <div className="flex-1">
                      <div className="sb-info-row-title">{team.name}</div>
                      <div className="sb-info-row-subtitle">
                        {team.mode === "group" ? t("detail.groupMode") : team.mode} · {team.bots.length} 个 Bot
                      </div>
                    </div>
                  </div>
                  {team.description && (
                    <div className="border-t border-[var(--sb-border)] px-4 py-3 text-[12px] leading-[18px] text-[var(--sb-text-2)]">
                      {team.description}
                    </div>
                  )}
                  <button
                    type="button"
                    onClick={onOpenTeams}
                    className="flex w-full items-center gap-2 border-t border-[var(--sb-border)] px-4 py-2.5 text-left text-[13px] transition hover:bg-[var(--sb-hover)]"
                  >
                    <span className="flex-1">{t("detail.manageGroup")}</span>
                    <ChevronRight className="h-4 w-4 text-[var(--sb-text-4)]" />
                  </button>
                </div>
              </>
            ) : bot ? (
              <>
                <div className="sb-info-label flex items-center justify-between">
                  <span>{t("detail.recentActivity")}</span>
                  {activities.length > 0 && (
                    <span className="text-[11px] font-normal text-[var(--sb-text-4)]">
                      {t("detail.activityCount", { n: activities.length })}
                    </span>
                  )}
                </div>
                <div className="sb-card overflow-hidden">
                  {activitiesLoading ? (
                    <div className="flex items-center justify-center py-5">
                      <Loader2 className="sb-spin h-4 w-4 text-[var(--sb-text-3)]" />
                    </div>
                  ) : activities.length === 0 ? (
                    <div className="flex items-center gap-3 px-4 py-3.5">
                      <ActivityIcon className="h-4 w-4 shrink-0 text-[var(--sb-text-3)]" />
                      <div className="text-[12px] leading-[18px] text-[var(--sb-text-2)]">
                        {t("detail.activityEmpty")}
                      </div>
                    </div>
                  ) : (
                    <>
                      {activities.slice(0, 6).map((activity, index) => (
                        <button
                          key={activity.id}
                          type="button"
                          className={cn(
                            "sb-card-row w-full text-left",
                            index > 0 && "border-t border-[var(--sb-border)]",
                          )}
                          onClick={() => {
                            setActivities((prev) =>
                              prev.map((item) =>
                                item.bot_id === activity.bot_id ? { ...item, read: true } : item,
                              ),
                            );
                            onOpenActivity(activity);
                          }}
                        >
                          <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-[var(--sb-hover)] text-[var(--sb-text-2)]">
                            {activity.kind === "message" ? (
                              <MessageSquare className="h-3.5 w-3.5" />
                            ) : (
                              <ArrowLeftRight className="h-3.5 w-3.5" />
                            )}
                          </span>
                          <span className="min-w-0 flex-1">
                            <span className="line-clamp-2 text-[13px] leading-[18px] text-[var(--sb-text)]">
                              {activity.summary}
                            </span>
                            <span className="mt-0.5 block text-[11px] leading-[15px] text-[var(--sb-text-3)]">
                              {activity.kind === "agent_message"
                                ? t("detail.kindAgentMessage")
                                : activity.kind === "agent_reply"
                                  ? t("detail.kindAgentReply")
                                  : activity.kind === "routine"
                                    ? t("detail.kindRoutine")
                                    : t("detail.kindReply")}{" · "}
                              {timeAgo(activity.created_at)}
                            </span>
                          </span>
                          {!activity.read && (
                            <span className="sb-unread-dot shrink-0" aria-label={t("detail.unreadActivity", { n: 0 }).replace("0 ", "")} />
                          )}
                        </button>
                      ))}
                      {activities.length > 6 && (
                        <Link
                          href="/activity"
                          className="flex w-full items-center gap-2 border-t border-[var(--sb-border)] px-4 py-2.5 text-[13px] text-[var(--sb-text-2)] transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-text)]"
                        >
                          <span className="flex-1">{t("detail.viewAllActivity")}</span>
                          <ChevronRight className="h-4 w-4 text-[var(--sb-text-4)]" />
                        </Link>
                      )}
                    </>
                  )}
                </div>

                <div className="sb-info-label flex items-center justify-between">
                  <span>{t("detail.upcoming")}</span>
                  <button
                    type="button"
                    onClick={() => onOpenRoutines(bot)}
                    className="flex h-5 w-5 items-center justify-center rounded text-[var(--sb-text-4)] transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-text)]"
                    aria-label={t("detail.newRoutine")}
                  >
                    <Plus className="h-3.5 w-3.5" />
                  </button>
                </div>
                <div className="sb-card overflow-hidden">
                  {routinesLoading ? (
                    <div className="flex items-center justify-center py-5">
                      <Loader2 className="sb-spin h-4 w-4 text-[var(--sb-text-3)]" />
                    </div>
                  ) : routines.length === 0 ? (
                    <button
                      type="button"
                      className="sb-card-row text-left text-[var(--sb-text-2)]"
                      onClick={() => onOpenRoutines(bot)}
                    >
                      <Clock className="h-4 w-4" />
                      <span className="flex-1 text-[12px]">{t("detail.routineEmpty")}</span>
                      <Plus className="h-3.5 w-3.5 text-[var(--sb-text-4)]" />
                    </button>
                  ) : (
                    routines.slice(0, 4).map((routine, index) => (
                      <div
                        key={routine.id}
                        className={cn("sb-card-row", index > 0 && "border-t border-[var(--sb-border)]")}
                      >
                        <Clock className="h-4 w-4 shrink-0 text-[var(--sb-text-2)]" />
                        <div className="min-w-0 flex-1">
                          <div className="truncate text-[14px] leading-[20px]">{routine.name}</div>
                          <div className="truncate text-[12px] leading-[16px] text-[var(--sb-text-2)]">
                            {formatSchedule(routine.schedule, routine.timezone)}
                            {!routine.enabled
                              ? t("detail.routineDisabled")
                              : routine.paused
                                ? t("detail.routinePaused")
                                : routine.next_run_at
                                  ? ` · ${formatNext(routine.next_run_at, routine.timezone)}`
                                  : t("detail.routinePending")}
                          </div>
                        </div>
                        <button
                          type="button"
                          className="sb-switch"
                          data-on={routine.enabled && !routine.paused ? "true" : undefined}
                          aria-label={
                            routine.enabled && !routine.paused ? t("detail.pauseRoutine") : t("detail.resumeRoutine")
                          }
                          onClick={() => void toggleRoutine(routine)}
                        />
                      </div>
                    ))
                  )}
                </div>

                {subagents.length > 0 && (
                  <>
                    <div className="sb-info-label">{t("detail.subagents")}</div>
                    <div className="sb-card overflow-hidden">
                      {subagents.slice(0, 6).map((task, index) => (
                        <div
                          key={task.id}
                          className={`flex items-center gap-2 px-3 py-2.5 ${
                            index > 0 ? "border-t border-[var(--sb-border)]" : ""
                          }`}
                          style={{ paddingLeft: `${12 + Math.min(task.depth, 2) * 14}px` }}
                        >
                          <span className="min-w-0 flex-1">
                            <span className="block truncate text-[13px]">
                              {task.depth > 0 && <span className="mr-1 text-[var(--sb-text-4)]">↳</span>}
                              {task.objective}
                            </span>
                            <span className="mt-0.5 block text-[11px] text-[var(--sb-text-3)]">
                              {task.status === "queued"
                                ? t("detail.statusQueued")
                                : task.status === "running"
                                  ? t("detail.statusRunning")
                                  : task.status === "succeeded"
                                    ? t("detail.statusDone")
                                    : task.status === "canceled"
                                      ? t("detail.statusCanceled")
                                      : t("detail.statusFailed")}
                              {task.result_summary ? ` · ${task.result_summary.slice(0, 60)}` : ""}
                            </span>
                          </span>
                          {(task.status === "queued" || task.status === "running") && (
                            <button
                              type="button"
                              className="shrink-0 text-[11px] text-[var(--sb-text-3)] transition hover:text-[var(--sb-danger)]"
                              onClick={async () => {
                                try {
                                  await api.stopSubagent(task.id);
                                  await loadSubagents();
                                } catch (err) {
                                  toast(err instanceof ApiError ? err.message : "停止子代理失败", "danger");
                                }
                              }}
                            >
                              停止
                            </button>
                          )}
                        </div>
                      ))}
                    </div>
                  </>
                )}

                <div className="sb-info-label">{t("detail.suggestions")}</div>
                <div className="sb-card overflow-hidden">
                  <button type="button" className="sb-card-row" onClick={() => onOpenExtensions("slack")}>
                    <span className="flex h-5 w-5 items-center justify-center rounded bg-[#611f69] text-[12px] font-semibold text-white">
                      #
                    </span>
                    <span className="flex-1 text-[14px]">{t("detail.addToSlack")}</span>
                    <ChevronRight className="h-4 w-4 text-[var(--sb-text-4)]" />
                  </button>
                  {routines.length === 0 && (
                    <button
                      type="button"
                      className="sb-card-row border-t border-[var(--sb-border)]"
                      onClick={() => bot && onOpenRoutines(bot)}
                    >
                      <span className="flex h-5 w-5 items-center justify-center rounded-full bg-[var(--sb-hover)] text-[var(--sb-text-2)]">
                        <Plus className="h-3.5 w-3.5" />
                      </span>
                      <span className="flex-1 text-[14px]">{t("detail.createRoutine")}</span>
                      <ChevronRight className="h-4 w-4 text-[var(--sb-text-4)]" />
                    </button>
                  )}
                </div>

                <div className="sb-info-label">{t("detail.computer")}</div>
                <button type="button" className="sb-card sb-card-row w-full" onClick={onOpenComputer}>
                  <div className="min-w-0 flex-1">
                    <div className="text-[14px] leading-[20px]">{t("detail.botComputer")}</div>
                    <div className="text-[12px] leading-[16px] text-[var(--sb-text-2)]">
                      {bot.computer_profile === "none" ? t("detail.notEnabled") : t("detail.sleeping")}
                    </div>
                  </div>
                  <span className="flex h-11 w-[68px] items-center justify-center overflow-hidden rounded-md bg-gradient-to-br from-[#d9d9d9] to-[#8f8f8f] text-white">
                    <Monitor className="h-4 w-4" />
                  </span>
                </button>

                <div className="sb-info-label">{t("detail.voiceChat")}</div>
                <div className="sb-card overflow-hidden">
                  <button
                    type="button"
                    className="sb-card-row"
                    onClick={(event) =>
                      setVoiceMenu({
                        kind: "voice",
                        rect: event.currentTarget.getBoundingClientRect(),
                      })
                    }
                  >
                    <span className="flex-1 text-[14px]">{t("detail.voice")}</span>
                    <span className="max-w-[130px] truncate text-[12px] text-[var(--sb-text-2)]">
                      {voiceName || t("detail.browserDefault")}
                    </span>
                    <ChevronDown className="h-3.5 w-3.5 text-[var(--sb-text-4)]" />
                  </button>
                  <button
                    type="button"
                    className="sb-card-row border-t border-[var(--sb-border)]"
                    onClick={(event) =>
                      setVoiceMenu({
                        kind: "rate",
                        rect: event.currentTarget.getBoundingClientRect(),
                      })
                    }
                  >
                    <span className="flex-1 text-[14px]">{t("detail.speed")}</span>
                    <span className="text-[12px] text-[var(--sb-text-2)]">{voiceRate}x</span>
                    <ChevronDown className="h-3.5 w-3.5 text-[var(--sb-text-4)]" />
                  </button>
                  <button
                    type="button"
                    className="sb-card-row border-t border-[var(--sb-border)]"
                    onClick={(event) =>
                      setVoiceMenu({
                        kind: "language",
                        rect: event.currentTarget.getBoundingClientRect(),
                      })
                    }
                  >
                    <span className="flex-1 text-[14px]">{t("detail.language")}</span>
                    <span className="text-[12px] text-[var(--sb-text-2)]">
                      {voiceLanguage === "zh"
                        ? t("detail.chinese")
                        : voiceLanguage === "en"
                          ? "English"
                          : "Auto-detect"}
                    </span>
                    <ChevronDown className="h-3.5 w-3.5 text-[var(--sb-text-4)]" />
                  </button>
                </div>

              </>
            ) : (
              <div className="sb-empty">
                <Sparkles className="h-5 w-5" />
                <div className="sb-empty-title">{t("detail.selectBot")}</div>
                <div className="sb-empty-desc">{t("detail.selectBotDesc")}</div>
              </div>
            )}
          </div>
        ) : tab === "files" ? (
          <div className="pb-4 pt-4">
            {filePath && (
              <button
                type="button"
                onClick={() => setFilePath(filePath.split("/").slice(0, -1).join("/"))}
                className="mb-2 text-[11.5px] text-[var(--sb-text-2)] hover:text-[var(--sb-text)]"
              >
                ← {t("detail.back")}
              </button>
            )}
            {filesLoading ? (
              <div className="flex justify-center py-10"><Loader2 className="h-4 w-4 animate-spin" /></div>
            ) : files.length === 0 ? (
              <div className="sb-empty">
                <Folder className="h-5 w-5" />
                <div className="sb-empty-title">{t("detail.filesEmpty")}</div>
                <div className="sb-empty-desc">{t("detail.filesEmptyDesc")}</div>
              </div>
            ) : (
              <div className="sb-card overflow-hidden">
                {files.map((file, index) => (
                  <button
                    key={file.path}
                    type="button"
                    className={cn("sb-card-row w-full text-left", index > 0 && "border-t border-[var(--sb-border)]")}
                    onClick={() => {
                      if (file.is_dir) {
                        setFilePath(file.path);
                        return;
                      }
                      window.open(api.botFileURL(bot!.id, file.path), "_blank", "noopener,noreferrer");
                    }}
                  >
                    {file.is_dir ? (
                      <Folder className="h-4 w-4 shrink-0 text-[var(--sb-text-2)]" />
                    ) : (
                      <FileText className="h-4 w-4 shrink-0 text-[var(--sb-text-3)]" />
                    )}
                    <span className="min-w-0 flex-1 truncate text-[12.5px]">{file.name}</span>
                    <span className="shrink-0 text-[10.5px] text-[var(--sb-text-4)]">
                      {file.is_dir ? "" : `${Math.max(1, Math.round(file.size / 1024))} KB`}
                    </span>
                  </button>
                ))}
              </div>
            )}
          </div>
        ) : tab === "computer" ? (
          <div className="pb-4">
            {team ? (
              <div className="sb-empty">
                <Monitor className="h-5 w-5" />
                <div className="sb-empty-title">{t("detail.groupNoComputer")}</div>
                <div className="sb-empty-desc">{t("detail.groupNoComputerDesc")}</div>
              </div>
            ) : bot ? (
              <>
                <div className="sb-info-label">{t("detail.botComputer")}</div>
                <div className="sb-card p-4">
                  <div className="flex items-center gap-3">
                    <span className="flex h-10 w-10 items-center justify-center rounded-lg bg-[var(--sb-hover)] text-[var(--sb-text-2)]">
                      <Monitor className="h-4 w-4" />
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="text-[14px] leading-[20px]">{t("detail.botComputer")}</div>
                      <div className="text-[12px] leading-[16px] text-[var(--sb-text-2)]">
                        {bot.computer_profile === "none"
                          ? t("detail.notEnabled")
                          : bot.computer_profile === "desktop"
                            ? "桌面会话 · 持久保留"
                            : "浏览器会话 · 持久保留"}
                      </div>
                    </div>
                  </div>
                  <div className="mt-3 flex h-40 items-center justify-center overflow-hidden rounded-lg bg-gradient-to-br from-[#d9d9d9] via-[#bdbdbd] to-[#8f8f8f] text-white">
                    <Monitor className="h-8 w-8" />
                  </div>
                  <button
                    type="button"
                    className="sb-btn mt-3 w-full"
                    data-variant="primary"
                    onClick={onOpenComputer}
                  >
                    {t("detail.openComputer")}
                  </button>
                  <p className="mt-2 text-[11px] leading-[16px] text-[var(--sb-text-3)]">
                    {t("detail.computerDesc")}
                  </p>
                </div>
              </>
            ) : (
              <div className="sb-empty">
                <Monitor className="h-5 w-5" />
                <div className="sb-empty-title">{t("detail.selectBotShort")}</div>
                <div className="sb-empty-desc">{t("detail.selectBotShortDesc")}</div>
              </div>
            )}
          </div>
        ) : (
          <div className="pb-4">
            <div className="flex items-center justify-between gap-2 px-1 pt-3">
              <span className="text-[11px] text-[var(--sb-text-3)]">{t("detail.mediaFiles")}</span>
              <button
                type="button"
                disabled={libraryUploading || (!bot && !team)}
                onClick={() => libraryFileRef.current?.click()}
                className="sb-btn"
                data-variant="secondary"
                data-size="sm"
              >
                {libraryUploading ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <Upload className="h-3.5 w-3.5" />
                )}
                {t("detail.uploadFile")}
              </button>
              <input
                ref={libraryFileRef}
                type="file"
                className="hidden"
                onChange={(event) => {
                  const file = event.target.files?.[0];
                  event.target.value = "";
                  if (file) void uploadLibraryFile(file);
                }}
              />
            </div>
            {libraryLoading ? (
              <div className="mt-4 space-y-2">
                {[0, 1, 2].map((item) => (
                  <div key={item} className="sb-skeleton h-12 w-full" />
                ))}
              </div>
            ) : groupedItems.length === 0 ? (
              <div className="sb-empty">
                <FileText className="h-5 w-5" />
                <div className="sb-empty-title">
                  {team ? t("detail.libraryEmptyGroup") : t("detail.libraryEmpty")}
                </div>
                <div className="sb-empty-desc">
                  {team
                    ? t("detail.libraryEmptyGroupDesc")
                    : t("detail.libraryEmptyDesc")}
                </div>
              </div>
            ) : (
              groupedItems.map((group) => (
                <div key={group.label}>
                  <div className="sb-info-label">{group.label}</div>
                  <div className="sb-card overflow-hidden">
                    {group.items.map((item, index) => (
                      <div
                        key={item.id}
                        className={cn(
                          "flex items-center gap-2 px-3 py-2.5 transition hover:bg-[var(--sb-hover)]",
                          index > 0 && "border-t border-[var(--sb-border)]",
                        )}
                        onContextMenu={(event) => {
                          event.preventDefault();
                          setItemMenu({ item, rect: new DOMRect(event.clientX, event.clientY, 0, 0) });
                        }}
                      >
                        <button
                          type="button"
                          onClick={() => void openItem(item)}
                          className="flex min-w-0 flex-1 items-center gap-3 text-left"
                        >
                          <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-[var(--sb-hover)] text-[var(--sb-text-2)]">
                            {item.kind === "media" || item.kind === "file" ? (
                              <ImageIcon className="h-4 w-4" />
                            ) : (
                              <FileText className="h-4 w-4" />
                            )}
                          </span>
                          <span className="min-w-0 flex-1">
                            <span className="block truncate text-[13px] leading-[18px] text-[var(--sb-text)]">
                              {item.title}
                            </span>
                            <span className="block text-[11px] leading-[15px] text-[var(--sb-text-3)]">
                              {team && `${botNameByID.get(item.bot_id) ?? t("detail.memberBot")} · `}
                              {item.kind === "page" ? t("detail.kindPage") : item.kind === "document" ? t("detail.kindDocument") : item.kind === "media" ? t("detail.kindMedia") : t("detail.kindFile")}
                              {" · "}
                              {timeAgo(item.created_at)}
                            </span>
                          </span>
                        </button>
                        <button
                          type="button"
                          className="flex h-6 w-6 items-center justify-center rounded text-[var(--sb-text-4)] transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-text)]"
                          onClick={(event) => setItemMenu({ item, rect: event.currentTarget.getBoundingClientRect() })}
                          aria-label="更多操作"
                        >
                          <MoreHorizontal className="h-3.5 w-3.5" />
                        </button>
                      </div>
                    ))}
                  </div>
                </div>
              ))
            )}
          </div>
        )}
      </div>

      <Menu
        open={voiceMenu !== null}
        anchor={voiceMenu ? { rect: voiceMenu.rect } : null}
        items={
          voiceMenu?.kind === "voice"
            ? [
                {
                  key: "default",
                  label: t("detail.browserDefault"),
                  disabled: !voiceName,
                  onSelect: () => {
                    setVoiceName("");
                    persistVoice("superbot-voice", "");
                  },
                },
                ...voices.slice(0, 14).map((voice) => ({
                  key: voice.name,
                  label: `${voice.name}${voice.lang ? ` · ${voice.lang}` : ""}`,
                  disabled: voiceName === voice.name,
                  onSelect: () => {
                    setVoiceName(voice.name);
                    persistVoice("superbot-voice", voice.name);
                  },
                })),
              ]
            : voiceMenu?.kind === "rate"
              ? ["0.75", "1", "1.25", "1.5"].map((rate) => ({
                  key: rate,
                  label: `${rate}x`,
                  disabled: voiceRate === rate,
                  onSelect: () => {
                    setVoiceRate(rate);
                    persistVoice("superbot-voice-rate", rate);
                  },
                }))
              : [
                  {
                    key: "auto",
                    label: "Auto-detect",
                    disabled: voiceLanguage === "auto",
                    onSelect: () => {
                      setVoiceLanguage("auto");
                      persistVoice("superbot-voice-language", "auto");
                    },
                  },
                  {
                    key: "zh",
                    label: t("detail.chinese"),
                    disabled: voiceLanguage === "zh",
                    onSelect: () => {
                      setVoiceLanguage("zh");
                      persistVoice("superbot-voice-language", "zh");
                    },
                  },
                  {
                    key: "en",
                    label: "English",
                    disabled: voiceLanguage === "en",
                    onSelect: () => {
                      setVoiceLanguage("en");
                      persistVoice("superbot-voice-language", "en");
                    },
                  },
                ]
        }
        onClose={() => setVoiceMenu(null)}
        align="right"
        minWidth={200}
      />

      <Menu
        open={itemMenu !== null}
        anchor={itemMenu ? { rect: itemMenu.rect } : null}
        items={
          itemMenu
            ? [
                {
                  key: "open",
                  label: "打开",
                  icon: <ExternalLink className="h-3.5 w-3.5" />,
                  onSelect: () => void openItem(itemMenu.item),
                },
                {
                  key: "delete",
                  label: "删除",
                  icon: <Trash2 className="h-3.5 w-3.5" />,
                  danger: true,
                  onSelect: () => void removeItem(itemMenu.item),
                },
              ]
            : []
        }
        onClose={() => setItemMenu(null)}
        align="right"
      />

      <Modal
        open={detail !== null || detailLoading}
        onClose={() => setDetail(null)}
        title={detail?.item.title ?? "加载中…"}
        subtitle={detail?.item.description || undefined}
        size="lg"
      >
        {detailLoading || !detail ? (
          <div className="flex items-center justify-center py-16">
            <Loader2 className="sb-spin h-5 w-5 text-[var(--sb-text-3)]" />
          </div>
        ) : (
          <LibraryPreview detail={detail} />
        )}
      </Modal>
      {confirmElement}
    </aside>
  );
}

function LibraryPreview({ detail }: { detail: LibraryItemDetail }) {
  const { item, content } = detail;
  const url = item.file_url;
  const mime = item.mime_type || "";
  if (url && mime.startsWith("image/")) {
    return (
      <a href={url} target="_blank" rel="noreferrer" className="block">
        <img src={url} alt={item.title} className="max-h-[62vh] w-full rounded-lg object-contain" />
      </a>
    );
  }
  if (url && mime.startsWith("audio/")) {
    return <audio controls preload="metadata" src={url} className="w-full" />;
  }
  if (url && mime.startsWith("video/")) {
    return <video controls src={url} className="max-h-[62vh] w-full rounded-lg bg-black" />;
  }
  if (url && mime === "application/pdf") {
    return <iframe title={item.title} src={url} className="h-[62vh] w-full rounded-lg border border-[var(--sb-border)]" />;
  }
  const extension = (item.title.includes(".") ? item.title.split(".").pop() : "")?.toLowerCase() ?? "";
  const officePreview =
    extension === "docx" ||
    extension === "xlsx" ||
    extension === "pptx" ||
    mime.includes("wordprocessingml") ||
    mime.includes("spreadsheetml") ||
    mime.includes("presentationml");
  const textPreview =
    ["txt", "csv", "tsv", "json", "log", "md", "py", "go", "js", "ts", "tsx", "jsx", "sh", "css", "yaml", "yml", "sql", "rs", "java", "c", "cpp", "html", "htm"].includes(
      extension,
    ) || mime.startsWith("text/");
  if (url && (officePreview || textPreview)) {
    return (
      <iframe
        title={item.title}
        src={`/api/library/${item.id}/preview`}
        className="h-[62vh] w-full rounded-lg border border-[var(--sb-border)] bg-white"
      />
    );
  }
  if (content) return <Markdown content={content} />;
  if (url) {
    return (
      <a href={url} download={item.title} className="sb-btn" data-variant="secondary">
        <Download className="h-3.5 w-3.5" />
        下载文件
      </a>
    );
  }
  return <div className="py-10 text-center text-[13px] text-[var(--sb-text-2)]">这个资料没有可预览的内容。</div>;
}

function formatSchedule(schedule: string, timezone?: string): string {
  const trimmed = schedule.trim();
  if (trimmed.startsWith("@every ")) return `每 ${trimmed.slice(7)}`;
  if (trimmed.startsWith("once:")) {
    const at = formatNext(trimmed.slice(5), timezone);
    return at ? `一次性 · ${at}` : "一次性";
  }
  return trimmed;
}

function formatNext(value: string, timezone?: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const options: Intl.DateTimeFormatOptions = {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  };
  if (timezone) {
    try {
      return date.toLocaleString("zh-CN", { ...options, timeZone: timezone });
    } catch {
      // fall back to the browser timezone when the routine timezone is invalid
    }
  }
  return date.toLocaleString("zh-CN", options);
}

function activityKindLabel(kind: string): string {
  if (kind === "agent_message") return "队友私信";
  if (kind === "agent_reply") return "队友回复";
  if (kind === "routine") return "例程";
  return "回复";
}
