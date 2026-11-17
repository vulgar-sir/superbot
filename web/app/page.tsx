"use client";

import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import {
  ArrowDown,
  ArrowUpRight,
  ChevronRight,
  CornerDownRight,
  Loader2,
  MessageCircleQuestion,
  MessageSquarePlus,
  PanelLeft,
  Plus,
  RotateCcw,
  Sparkles,
  ShieldAlert,
  Upload,
  X,
} from "lucide-react";

import { api, ApiError, SSE_EVENT_TYPES } from "@/lib/api";
import { botProfilePayload } from "@/lib/bot";
import type {
  AgentThreadDetail,
  Attachment,
  Bot,
  Conversation,
  EventEnvelope,
  Message,
  SidebarData,
  SidebarSection,
  StreamState,
  Team,
  ToolActivity,
  ToolApproval,
  User,
} from "@/lib/types";
import { cn } from "@/lib/format";
import { localizeServerText } from "@/lib/serverText";
import { GrokSidebar } from "@/components/GrokSidebar";
import { NewBotDialog } from "@/components/NewBotDialog";
import { NewGroupDialog } from "@/components/NewGroupDialog";
import { BotInfoDialog } from "@/components/BotInfoDialog";
import { RoutineDialog } from "@/components/RoutineDialog";
import { AgentThreadPanel } from "@/components/AgentThreadPanel";
import { AgentThreadGroup } from "@/components/AgentThreadGroup";
import { BotWorkBanner } from "@/components/BotWorkBanner";
import { buildTranscript, itemMessage } from "@/lib/agentThreads";
import { orderForDisplay, replyRunIDs, replySeenOrdinal, unreadByReply } from "@/lib/displayOrder";
import { SearchDialog } from "@/components/SearchDialog";
import { VoiceCallPanel } from "@/components/VoiceCallPanel";
import { ChatMessage } from "@/components/ChatMessage";
import { StreamingMessage } from "@/components/StreamingMessage";
import { ToolGroup } from "@/components/ToolCard";
import { Composer } from "@/components/Composer";
import { BotDetailPanel } from "@/components/BotDetailPanel";
import { GoalChip, GoalPanel, useGroupGoal } from "@/components/GoalPanel";
import { GroupRoutineList } from "@/components/GroupRoutineList";
import { isRoutineTrigger } from "@/components/SystemNotices";
import { SidePanel, parseSideCommand, useSideChats } from "@/components/SidePanel";
import { SettingsScreen } from "@/app/settings/page";
import { HelpDialog } from "@/components/HelpDialog";
import { ConnectorsDialog } from "@/components/ConnectorsDialog";
import { ComputerPanel } from "@/components/ComputerPanel";
import { BotAvatar } from "@/components/BotAvatar";
import { Logo } from "@/components/Logo";
import { useToast } from "@/components/ui/Toast";
import { Menu } from "@/components/ui/Menu";
import { ShareMenu } from "@/components/ShareMenu";
import { NotificationCenter } from "@/components/NotificationCenter";
import { useLanguage } from "@/components/LanguageProvider";
import { copyText } from "@/lib/clipboard";

function hashString(value: string): number {
  let hash = 5381;
  for (let index = 0; index < value.length; index += 1) {
    hash = ((hash << 5) + hash + value.charCodeAt(index)) >>> 0;
  }
  return hash;
}

function pickSuggestions(pool: string[], seed: string): string[] {
  if (pool.length <= 3) return pool;
  const start = hashString(seed) % pool.length;
  return [0, 1, 2].map((offset) => pool[(start + offset) % pool.length]);
}

function welcomeSuggestions(bot?: Bot, team?: Team, language: "zh" | "en" = "zh"): string[] {
  if (team) {
    const name = team.name;
    const member = team.bots[0]?.bot?.name;
    if (language === "en") {
      const pool = [
        `Ask each member of "${name}" what they are best at.`,
        `Break down today's most important task with "${name}".`,
        `Have "${name}" split up a small task and summarize the result.`,
        member ? `@${member} what do you own in "${name}"?` : `What can "${name}" do for me right now?`,
        `Ask "${name}" to review yesterday and plan today.`,
        `Give today's goal to "${name}" and let it sequence the work.`,
      ];
      return pickSuggestions(pool, team.id || name);
    }
    const pool = [
      `让「${name}」的成员各自介绍一下擅长的方向。`,
      `和「${name}」一起拆解今天最重要的任务。`,
      `让「${name}」分工完成一项小任务，再汇总结果。`,
      member ? `@${member} 先说说你在「${name}」里负责什么。` : `「${name}」现在能帮我做什么？`,
      `让「${name}」复盘昨天的工作，并列出今天的计划。`,
      `把今天的目标交给「${name}」，让它安排执行顺序。`,
    ];
    return pickSuggestions(pool, team.id || name);
  }
  const name = bot?.name?.trim() || "这个 Bot";
  const description = (bot?.description || bot?.title || "").trim();
  if (language === "en") {
    const displayName = bot?.name?.trim() || "this bot";
    const pool = [
      `${displayName}, introduce yourself and what you are best at.`,
      `Ask ${displayName} to inspect the workspace and summarize its structure.`,
      `List the tools ${displayName} can use and what each is best for.`,
      `${displayName}, break down today's most important task and give me a plan.`,
      `Use ${displayName}'s strongest skill to finish a small task for me.`,
      description
        ? `What can ${displayName} do around "${description}"?`
        : `Ask ${displayName} for three tasks we can start right now.`,
      `Set up a routine so ${displayName} reports progress every day.`,
      `Ask ${displayName} to look at the workspace and suggest what to push forward today.`,
    ];
    return pickSuggestions(pool, bot?.id || displayName);
  }
  const pool = [
    `${name}，先介绍一下你能做什么，以及最擅长处理哪类任务。`,
    `让 ${name} 检查当前工作区，并总结目录结构。`,
    `列出 ${name} 能使用的工具，并说明分别适合什么场景。`,
    `${name}，帮我拆解今天最重要的任务，并给出执行计划。`,
    `用 ${name} 最擅长的方式，帮我完成一个小任务。`,
    description
      ? `围绕「${description}」，${name} 能帮我做什么？`
      : `让 ${name} 给我三个现在就能开始的任务建议。`,
    `帮我给 ${name} 设一个例程，让它每天自动汇报进展。`,
    `让 ${name} 先看看工作区，再告诉我今天适合推进什么。`,
  ];
  return pickSuggestions(pool, bot?.id || name);
}

function dateKey(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  return `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}`;
}

function dateLabel(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  const now = new Date();
  const startToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const time = date.getTime();
  if (time >= startToday) {
    return `今天 ${date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}`;
  }
  if (time >= startToday - 24 * 3600 * 1000) {
    return `昨天 ${date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}`;
  }
  return date.toLocaleString("zh-CN", {
    month: "long",
    day: "numeric",
    weekday: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function sameSender(a: Message, b: Message): boolean {
  return a.sender_type === b.sender_type && a.sender_bot_id === b.sender_bot_id;
}

function groupPosition(messages: Message[], index: number): "first" | "middle" | "last" | "single" {
  const message = messages[index];
  const prev = messages[index - 1];
  const next = messages[index + 1];
  // Notice rows (system messages, routine triggers) break a run of bubbles.
  const prevSame = prev && prev.sender_type !== "system" && !isRoutineTrigger(prev) && sameSender(prev, message);
  const nextSame = next && next.sender_type !== "system" && !isRoutineTrigger(next) && sameSender(next, message);
  if (prevSame && nextSame) return "middle";
  if (prevSame) return "last";
  if (nextSame) return "first";
  return "single";
}

// The primary bot's request for standing exemptions, read from the approval
// arguments; null for any other approval.
function ruleRequest(
  approval: ToolApproval,
): { summary: string; rules: Array<{ pattern: string; reason: string }> } | null {
  if (approval.tool_name !== "request_permission_rules") return null;
  const args = (approval.args ?? {}) as { summary?: unknown; rules?: unknown };
  const rules = Array.isArray(args.rules)
    ? args.rules
        .map((rule) => {
          const item = (rule ?? {}) as { pattern?: unknown; reason?: unknown };
          return { pattern: String(item.pattern ?? "").trim(), reason: String(item.reason ?? "").trim() };
        })
        .filter((rule) => rule.pattern)
    : [];
  return { summary: typeof args.summary === "string" ? args.summary : "", rules };
}

function isToolApprovalNotice(message: Message): boolean {
  if (message.sender_type !== "system") return false;
  if (!message.metadata || typeof message.metadata.approval_id !== "string") return false;
  return /^工具审批(已通过|被拒绝)/.test(message.content) || /^Tool approval (granted|denied)/.test(message.content);
}

// In-memory status of a user message that was sent while a cycle was running.
type InterjectionState = "pending" | "delivered" | "follow_up";

type CycleRetry = { attempt: number; maxAttempts: number; error: string };

const INTERJECTION_FADE_MS = 4000;

// How many of the latest replies get their tool cards loaded on open.
const RECENT_TOOL_REPLIES = 8;

export default function HomePage() {
  const router = useRouter();
  const { toast } = useToast();
  const { t, language } = useLanguage();

  const [booting, setBooting] = useState(true);
  const [bootError, setBootError] = useState<string | null>(null);
  const [user, setUser] = useState<User | null>(null);
  const [bots, setBots] = useState<Bot[]>([]);
  const [hasProvider, setHasProvider] = useState(true);
  const [teams, setTeams] = useState<Team[]>([]);
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [activeId, setActiveId] = useState<string | null>(null);
  const [conversation, setConversation] = useState<Conversation | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  // Replies sit before the messages that reached the bot too late for them.
  const visibleMessages = useMemo(
    () => orderForDisplay(messages.filter((message) => !isToolApprovalNotice(message))),
    [messages],
  );
  // Bot-to-bot private messages fold into one summary per exchange.
  const transcript = useMemo(() => buildTranscript(visibleMessages), [visibleMessages]);
  const transcriptAnchors = useMemo(() => transcript.map((item) => itemMessage(item)), [transcript]);
  const [streams, setStreams] = useState<Record<string, StreamState>>({});
  const [tools, setTools] = useState<Record<string, ToolActivity[]>>({});
  const [runStatus, setRunStatus] = useState<Record<string, string>>({});
  // For a run that resumed a reply after a tool approval: the paused runs
  // before it, whose tool calls belong to the same reply.
  const [runChains, setRunChains] = useState<Record<string, string[]>>({});
  const [cycleActive, setCycleActive] = useState(false);
  const [live, setLive] = useState(false);
  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const [loadingConversation, setLoadingConversation] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [interjections, setInterjections] = useState<Record<string, InterjectionState>>({});
  const [cycleRetry, setCycleRetry] = useState<CycleRetry | null>(null);
  // Error text of a cycle that gave up; "" when the server sent none.
  const [cycleFailure, setCycleFailure] = useState<string | null>(null);
  const [sidebar, setSidebar] = useState<SidebarData | null>(null);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [infoOpen, setInfoOpen] = useState(true);
  // The side-question drawer takes the info panel's place while open.
  const [sideOpen, setSideOpen] = useState(false);
  const [sideFocusSignal, setSideFocusSignal] = useState(0);
  const [goalFocusSignal, setGoalFocusSignal] = useState(0);
  const [showJump, setShowJump] = useState(false);
  const [newMessages, setNewMessages] = useState(0);
  const [focusMessageID, setFocusMessageID] = useState<string | null>(null);
  const [findOpen, setFindOpen] = useState(false);
  const [findQuery, setFindQuery] = useState("");
  const [findIndex, setFindIndex] = useState(0);
  const [newBotOpen, setNewBotOpen] = useState(false);
  const [newBotKind, setNewBotKind] = useState<"personal" | "team">("personal");
  const [notificationSignal, setNotificationSignal] = useState(0);
  const [creatingBot, setCreatingBot] = useState(false);
  const [newBotError, setNewBotError] = useState<string | null>(null);
  const [newGroupOpen, setNewGroupOpen] = useState(false);
  const [creatingGroup, setCreatingGroup] = useState(false);
  const [newGroupError, setNewGroupError] = useState<string | null>(null);
  const [infoBot, setInfoBot] = useState<Bot | null>(null);
  const [infoTab, setInfoTab] = useState<"profile" | "library" | "memory">("profile");
  const [savingBotProfile, setSavingBotProfile] = useState(false);
  const [botProfileError, setBotProfileError] = useState<string | null>(null);
  const [settingPrimary, setSettingPrimary] = useState(false);
  const [primaryError, setPrimaryError] = useState<string | null>(null);
  const [routineBot, setRoutineBot] = useState<Bot | null>(null);
  const [agentThreadID, setAgentThreadID] = useState<string | null>(null);
  // Set while a bot-to-bot thread is open in the main view (read-only):
  // the bot whose work it is, the chat to return to, and the pair's name.
  const [agentView, setAgentView] = useState<{ botId: string; returnTo: string | null; title: string } | null>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [voiceOpen, setVoiceOpen] = useState(false);
  const [computerOpen, setComputerOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsSection, setSettingsSection] = useState("appearance");
  const [shareAnchor, setShareAnchor] = useState<{ rect: DOMRect } | null>(null);
  const [replyTarget, setReplyTarget] = useState<Message | null>(null);
  const [pendingAttachments, setPendingAttachments] = useState<Attachment[]>([]);
  const [approvals, setApprovals] = useState<ToolApproval[]>([]);
  const [uploadingAttachments, setUploadingAttachments] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);
  const [connectorsOpen, setConnectorsOpen] = useState(false);
  const [connectorsProvider, setConnectorsProvider] = useState<string | null>(null);
  const installPromptRef = useRef<any>(null);

  const goalCtl = useGroupGoal(
    activeId,
    Boolean(conversation?.team_id && conversation.id === activeId),
  );
  const { applyEvent: applyGoalEvent, reload: reloadGoal } = goalCtl;
  const side = useSideChats(activeId);

  const esRef = useRef<EventSource | null>(null);
  const lastSeqRef = useRef(0);
  const activeIdRef = useRef<string | null>(null);
  const preferredConversationRef = useRef<string | null>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const transcriptRef = useRef<HTMLDivElement>(null);
  const stickRef = useRef(true);
  const pendingSendRef = useRef<{ id: string; content: string } | null>(null);
  // Message ids the server reported as delivered to a working bot. The SSE
  // event can beat the send response, so remember them for sendContent.
  const deliveredInterjectionsRef = useRef<Set<string>>(new Set());
  // How far each run had read, from its status events; a stream that starts
  // after the event picks it up from here.
  const runSeenRef = useRef<Record<string, number>>({});
  const runBotRef = useRef<Record<string, string>>({});
  const messagesRef = useRef<Message[]>([]);
  useEffect(() => {
    messagesRef.current = messages;
  }, [messages]);

  // Let "delivered" notes disappear after a moment.
  const fadeDeliveredInterjections = useCallback((ids: string[]) => {
    window.setTimeout(() => {
      setInterjections((prev) => {
        const next = { ...prev };
        let changed = false;
        for (const id of ids) {
          if (next[id] === "delivered") {
            delete next[id];
            changed = true;
          }
        }
        return changed ? next : prev;
      });
    }, INTERJECTION_FADE_MS);
  }, []);

  // A reply that had read an interjection has taken it in, whether it reached
  // the bot mid-turn or was already there when the bot's turn began (the next
  // member of a group, a follow-up cycle).
  const markInterjectionsRead = useCallback(
    (seen: number) => {
      const read = messagesRef.current
        .filter((item) => item.sender_type === "user" && !item.id.startsWith("pending-") && item.ordinal <= seen)
        .map((item) => item.id);
      if (read.length === 0) return;
      const readSet = new Set(read);
      setInterjections((prev) => {
        const next = { ...prev };
        let changed = false;
        for (const [id, state] of Object.entries(prev)) {
          if (state !== "delivered" && readSet.has(id)) {
            next[id] = "delivered";
            changed = true;
          }
        }
        return changed ? next : prev;
      });
      fadeDeliveredInterjections(read.slice(-20));
    },
    [fadeDeliveredInterjections],
  );

  const closeSSE = useCallback(() => {
    esRef.current?.close();
    esRef.current = null;
    setLive(false);
  }, []);

  const refreshActive = useCallback(async () => {
    const id = activeIdRef.current;
    if (!id) return;
    try {
      const data = await api.getConversation(id);
      setConversation(data.conversation);
      setMessages(data.messages);
    } catch (err) {
      if (err instanceof ApiError) setErrorMessage(err.message);
    }
  }, []);

  const refreshSidebar = useCallback(async () => {
    try {
      setSidebar(await api.getSidebar());
    } catch {
      // chat still works
    }
  }, []);

  const refreshBots = useCallback(async () => {
    try {
      const result = await api.listBots();
      setBots(result.bots);
    } catch {
      // ignore
    }
  }, []);

  const toggleSection = useCallback(
    async (section: SidebarSection) => {
      if (!section.id) return;
      setSidebar((prev) =>
        prev
          ? {
              ...prev,
              sections: prev.sections.map((item) =>
                item.id === section.id ? { ...item, collapsed: !item.collapsed } : item,
              ),
            }
          : prev,
      );
      try {
        await api.updateSection(section.id, { collapsed: !section.collapsed });
      } catch {
        void refreshSidebar();
      }
    },
    [refreshSidebar],
  );

  const scrollToBottom = useCallback((behavior: ScrollBehavior = "smooth") => {
    stickRef.current = true;
    setShowJump(false);
    setNewMessages(0);
    bottomRef.current?.scrollIntoView({ behavior, block: "end" });
  }, []);

  const handleEnvelope = useCallback(
    (envelope: EventEnvelope) => {
      if (!envelope || typeof envelope.seq !== "number") return;
      if (envelope.seq <= lastSeqRef.current) return;
      lastSeqRef.current = envelope.seq;
      setConversation((prev) => (prev ? { ...prev, last_event_seq: envelope.seq } : prev));

      const payload = envelope.payload ?? {};
      switch (envelope.type) {
        case "message.created": {
          const raw = payload.message as Message | undefined;
          if (!raw) return;
          const message: Message = {
            ...raw,
            reactions: raw.reactions ?? [],
            attachments: raw.attachments ?? [],
          };
          setMessages((prev) => {
            if (prev.some((item) => item.id === message.id)) return prev;
            const pending = pendingSendRef.current;
            if (
              pending &&
              message.sender_type === "user" &&
              message.content === pending.content
            ) {
              pendingSendRef.current = null;
              return prev.map((item) =>
                item.id === pending.id
                  ? {
                      ...message,
                      attachments: message.attachments ?? item.attachments,
                      reactions: message.reactions ?? item.reactions,
                    }
                  : item,
              );
            }
            return [...prev, message];
          });
          // Checkpoint markers, goal checks and routine triggers are
          // bookkeeping, not something new to read.
          if (
            !stickRef.current &&
            message.metadata?.kind !== "context_checkpoint" &&
            message.metadata?.kind !== "goal_nudge" &&
            !isRoutineTrigger(message)
          ) {
            setNewMessages((prev) => prev + 1);
          }
          if (message.sender_type === "bot") setCycleRetry(null);
          if (message.sender_type === "bot" && message.run_id) {
            const runId = message.run_id;
            // The reply replaces its live stream, and those of the runs that
            // paused for approval on the way to it.
            const finished = replyRunIDs(message);
            setStreams((prev) => {
              if (!finished.some((id) => prev[id])) return prev;
              const next = { ...prev };
              for (const id of finished) delete next[id];
              return next;
            });
            setRunStatus((prev) => ({ ...prev, [runId]: "succeeded" }));
            const seen = replySeenOrdinal(message);
            if (seen > 0) markInterjectionsRead(seen);
          }
          return;
        }
        case "text_delta": {
          const runId = String(payload.run_id ?? "");
          if (!runId) return;
          setStreams((prev) => {
            const current = prev[runId];
            return {
              ...prev,
              [runId]: {
                runId,
                botId: String(payload.bot_id ?? ""),
                text: (current?.text ?? "") + String(payload.text ?? ""),
                reasoning: current?.reasoning ?? "",
                seen: current?.seen ?? runSeenRef.current[runId] ?? Number.MAX_SAFE_INTEGER,
              },
            };
          });
          setCycleActive(true);
          return;
        }
        case "reasoning_delta": {
          const runId = String(payload.run_id ?? "");
          if (!runId) return;
          setStreams((prev) => {
            const current = prev[runId];
            return {
              ...prev,
              [runId]: {
                runId,
                botId: String(payload.bot_id ?? ""),
                text: current?.text ?? "",
                reasoning: (current?.reasoning ?? "") + String(payload.text ?? ""),
                seen: current?.seen ?? runSeenRef.current[runId] ?? Number.MAX_SAFE_INTEGER,
              },
            };
          });
          setCycleActive(true);
          return;
        }
        case "tool_call": {
          const runId = String(payload.run_id ?? "");
          const callId = String(payload.call_id ?? "");
          if (!runId || !callId) return;
          setTools((prev) => {
            const list = prev[runId] ?? [];
            if (list.some((item) => item.callId === callId)) return prev;
            const activity: ToolActivity = {
              callId,
              name: String(payload.name ?? "tool"),
              args: (payload.arguments ?? {}) as Record<string, unknown>,
              status: "running",
            };
            return { ...prev, [runId]: [...list, activity] };
          });
          // A reply that opens with a tool call still gets its place (and its
          // typing dots) under the tool card right away.
          setStreams((prev) =>
            prev[runId]
              ? prev
              : {
                  ...prev,
                  [runId]: {
                    runId,
                    botId: runBotRef.current[runId] ?? "",
                    text: "",
                    reasoning: "",
                    seen: runSeenRef.current[runId] ?? Number.MAX_SAFE_INTEGER,
                  },
                },
          );
          setCycleActive(true);
          return;
        }
        case "tool_approval": {
          const approvalID = String(payload.approval_id ?? "");
          if (!approvalID) return;
          const approval: ToolApproval = {
            id: approvalID,
            conversation_id: String(payload.conversation_id ?? ""),
            run_id: String(payload.run_id ?? ""),
            call_id: String(payload.call_id ?? ""),
            // The event names the tool "name"; reading only tool_name showed
            // every request as "tool" and saved "always allow" as a rule
            // for a tool that does not exist.
            tool_name: String(payload.tool_name ?? payload.name ?? "tool"),
            args: (payload.arguments ?? {}) as Record<string, unknown>,
            status: String(payload.status ?? "pending"),
            created_at: String(payload.created_at ?? new Date().toISOString()),
          };
          setApprovals((prev) =>
            prev.some((item) => item.id === approval.id) ? prev : [...prev, approval],
          );
          // The run stops here; its text stays, without the typing caret.
          if (approval.run_id) {
            setRunStatus((prev) => ({ ...prev, [approval.run_id]: "waiting_approval" }));
          }
          return;
        }
        case "tool_result": {
          const runId = String(payload.run_id ?? "");
          const callId = String(payload.call_id ?? "");
          if (!runId || !callId) return;
          setApprovals((prev) => prev.filter((item) => item.call_id !== callId));
          setTools((prev) => {
            const list = prev[runId] ?? [];
            const next = list.map((item) =>
              item.callId === callId
                ? {
                    ...item,
                    status: payload.is_error ? ("failed" as const) : ("done" as const),
                    result: String(payload.content ?? ""),
                    isError: Boolean(payload.is_error),
                    widgetHtml:
                      typeof payload.widget_html === "string" ? payload.widget_html : undefined,
                  }
                : item,
            );
            return { ...prev, [runId]: next };
          });
          return;
        }
        case "status": {
          const runId = String(payload.run_id ?? "");
          const status = String(payload.status ?? "");
          if (runId) setRunStatus((prev) => ({ ...prev, [runId]: status }));
          if (runId && payload.bot_id) runBotRef.current[runId] = String(payload.bot_id);
          const seenOrdinal = Number(payload.seen_ordinal ?? 0);
          if (runId && Number.isFinite(seenOrdinal) && seenOrdinal > 0) {
            runSeenRef.current[runId] = Math.max(runSeenRef.current[runId] ?? 0, seenOrdinal);
            setStreams((prev) => {
              const current = prev[runId];
              if (!current || current.seen === seenOrdinal) return prev;
              return { ...prev, [runId]: { ...current, seen: seenOrdinal } };
            });
          }
          const resumedFrom = Array.isArray(payload.resumed_from)
            ? payload.resumed_from.map((id) => String(id)).filter(Boolean)
            : [];
          if (runId && resumedFrom.length > 0) {
            // This run carries on the paused runs' reply and streams their
            // text first; their own streams would only repeat it.
            setRunChains((prev) => ({ ...prev, [runId]: resumedFrom }));
            setStreams((prev) => {
              if (!resumedFrom.some((id) => prev[id])) return prev;
              const next = { ...prev };
              for (const id of resumedFrom) delete next[id];
              return next;
            });
          }
          const delivered = Array.isArray(payload.interjection_delivered)
            ? payload.interjection_delivered.map((id) => String(id))
            : [];
          for (const id of delivered) deliveredInterjectionsRef.current.add(id);
          if (status === "running" || delivered.length > 0) {
            setInterjections((prev) => {
              const next = { ...prev };
              let changed = false;
              for (const [id, state] of Object.entries(prev)) {
                if (delivered.includes(id)) {
                  if (state !== "delivered") {
                    next[id] = "delivered";
                    changed = true;
                  }
                } else if (state === "follow_up" && status === "running") {
                  // The follow-up cycle has started; the bot is answering.
                  delete next[id];
                  changed = true;
                }
              }
              return changed ? next : prev;
            });
            if (delivered.length > 0) fadeDeliveredInterjections(delivered);
          }
          if (status === "running") setCycleActive(true);
          if (status === "failed") {
            setErrorMessage(String(payload.error ?? "运行失败"));
            setCycleActive(false);
          }
          if (status === "succeeded" || status === "canceled") setCycleActive(false);
          return;
        }
        case "usage": {
          const cost = Number(payload.cost_micros ?? 0);
          setConversation((prev) =>
            prev ? { ...prev, spent_micros: prev.spent_micros + cost } : prev,
          );
          return;
        }
        case "cycle.updated": {
          const status = String(payload.status ?? "");
          const cycleError = String(payload.error ?? "");
          if (payload.reason === "follow_up") {
            // The previous cycle ended before the interjection reached the
            // bot, so the server queued a cycle of its own to answer it.
            setInterjections((prev) => {
              const next = { ...prev };
              let changed = false;
              for (const [id, state] of Object.entries(prev)) {
                if (state === "pending") {
                  next[id] = "follow_up";
                  changed = true;
                }
              }
              return changed ? next : prev;
            });
            setCycleActive(true);
            return;
          }
          if (status === "queued" && typeof payload.attempt === "number" && cycleError) {
            // A failed attempt is being retried; drop the partial output.
            setCycleRetry({
              attempt: payload.attempt,
              maxAttempts: Number(payload.max_attempts) || 0,
              error: cycleError,
            });
            setCycleFailure(null);
            setErrorMessage(null);
            setStreams({});
            setCycleActive(true);
            return;
          }
          if (status === "running") {
            setInterjections((prev) => {
              const next = { ...prev };
              let changed = false;
              for (const [id, state] of Object.entries(prev)) {
                if (state === "follow_up") {
                  delete next[id];
                  changed = true;
                }
              }
              return changed ? next : prev;
            });
          }
          if (["done", "canceled", "failed", "waiting_user"].includes(status)) {
            setCycleActive(false);
            setCycleRetry(null);
          }
          if (status === "done") {
            // Every reply of the cycle is stored by now; a stream still
            // around belongs to a run that never finished one.
            setStreams({});
          }
          if (status === "failed" || status === "canceled") {
            // Nothing will deliver these any more.
            setInterjections((prev) => {
              const next = { ...prev };
              let changed = false;
              for (const [id, state] of Object.entries(prev)) {
                if (state !== "delivered") {
                  delete next[id];
                  changed = true;
                }
              }
              return changed ? next : prev;
            });
          }
          if (status === "failed") {
            setCycleFailure(cycleError);
            setErrorMessage(null);
          }
          return;
        }
        case "goal.updated": {
          applyGoalEvent(payload.goal);
          return;
        }
        case "resync": {
          setStreams({});
          setTools({});
          void refreshActive();
          void reloadGoal();
          return;
        }
        case "error": {
          setErrorMessage(String(payload.error ?? "运行出错"));
          setCycleActive(false);
          return;
        }
      }
    },
    [refreshActive, fadeDeliveredInterjections, markInterjectionsRead, applyGoalEvent, reloadGoal],
  );

  const connectSSE = useCallback(
    (id: string, after: number) => {
      closeSSE();
      lastSeqRef.current = after;
      const source = new EventSource(`/api/conversations/${id}/stream?after=${after}`);
      const listener = (event: Event) => {
        try {
          handleEnvelope(JSON.parse((event as MessageEvent).data) as EventEnvelope);
        } catch {
          // ignore malformed event
        }
      };
      for (const type of SSE_EVENT_TYPES) {
        source.addEventListener(type, listener as EventListener);
      }
      source.onopen = () => setLive(true);
      source.onerror = () => setLive(false);
      esRef.current = source;
    },
    [closeSSE, handleEnvelope],
  );

  const openConversation = useCallback(
    async (id: string) => {
      setAgentView(null);
      setLoadingConversation(true);
      setErrorMessage(null);
      try {
        const data = await api.getConversation(id);
        activeIdRef.current = id;
        setActiveId(id);
        setConversation(data.conversation);
        setMessages(data.messages);
        setStreams({});
        setTools({});
        setRunStatus({});
        setCycleActive(false);
        setApprovals([]);
        setInterjections({});
        deliveredInterjectionsRef.current.clear();
        setCycleRetry(null);
        setCycleFailure(null);
        setConversations((prev) => prev.map((item) => (item.id === id ? data.conversation : item)));
        // After a reload, show that the last request failed, or that the bot
        // is still on it (stop button, busy composer), as a live page would.
        const lastCycle = data.last_cycle;
        if (lastCycle?.status === "failed") setCycleFailure(lastCycle.error ?? "");
        if (lastCycle && ["queued", "running"].includes(lastCycle.status)) setCycleActive(true);
        void api
          .listToolApprovals(id)
          .then((result) => {
            if (activeIdRef.current === id) setApprovals(result.approvals);
          })
          .catch(() => undefined);
        stickRef.current = true;
        window.setTimeout(() => scrollToBottom("auto"), 30);
        // Tool cards sit above the reply they led to; load them for the
        // latest replies (and the runs that paused for approval before them).
        const recentRuns = data.messages
          .filter((message) => message.sender_type === "bot" && message.run_id)
          .slice(-RECENT_TOOL_REPLIES)
          .flatMap((message) => replyRunIDs(message));
        setRunChains(
          Object.fromEntries(
            data.messages
              .filter((message) => message.sender_type === "bot" && message.run_id)
              .map((message) => [message.run_id as string, replyRunIDs(message).slice(0, -1)])
              .filter(([, chain]) => chain.length > 0),
          ),
        );
        runSeenRef.current = {};
        runBotRef.current = {};
        for (const runId of recentRuns) {
          void api
            .getRun(runId, "tool_result")
            .then((runData) => {
              if (activeIdRef.current !== id) return;
              const results = new Map<string, { content: string; isError: boolean; widgetHtml?: string }>();
              for (const event of runData.events) {
                if (event.type === "tool_result") {
                  results.set(String(event.payload.call_id ?? ""), {
                    content: String(event.payload.content ?? ""),
                    isError: Boolean(event.payload.is_error),
                    widgetHtml:
                      typeof event.payload.widget_html === "string" ? event.payload.widget_html : undefined,
                  });
                }
              }
              const activities: ToolActivity[] = runData.tool_invocations.map((invocation) => {
                const result = results.get(invocation.call_id);
                return {
                  callId: invocation.call_id,
                  name: invocation.name,
                  args: invocation.args,
                  status:
                    invocation.status === "failed"
                      ? ("failed" as const)
                      : invocation.status === "running" || invocation.status === "intent"
                        ? ("running" as const)
                        : ("done" as const),
                  result: result?.content ?? invocation.error,
                  isError: result?.isError ?? invocation.status === "failed",
                  widgetHtml: result?.widgetHtml,
                };
              });
              setTools((prev) => ({ ...prev, [runId]: activities }));
              setRunStatus((prev) => ({ ...prev, [runId]: runData.run.status }));
            })
            .catch(() => undefined);
        }
        connectSSE(id, data.conversation.last_event_seq);
      } catch (err) {
        if (err instanceof ApiError) setErrorMessage(err.message);
        else setErrorMessage("无法加载会话");
      } finally {
        setLoadingConversation(false);
      }
    },
    [connectSSE, scrollToBottom],
  );

  useEffect(() => {
    try {
      const saved = window.localStorage.getItem("superbot-info-open");
      if (saved === "false") setInfoOpen(false);
    } catch {
      // ignore
    }
    if (window.innerWidth < 860) setSidebarCollapsed(true);
    if (window.innerWidth < 1000) setInfoOpen(false);
  }, []);

  useEffect(() => {
    try {
      window.localStorage.setItem("superbot-info-open", String(infoOpen));
    } catch {
      // ignore
    }
  }, [infoOpen]);

  useEffect(() => {
    const handler = (event: Event) => {
      event.preventDefault();
      installPromptRef.current = event;
    };
    window.addEventListener("beforeinstallprompt", handler);
    return () => window.removeEventListener("beforeinstallprompt", handler);
  }, []);

  const refreshedForRef = useRef<string | null>(null);
  useEffect(() => {
    if (!conversation) return;
    const botMissing = conversation.direct_bot_id && !bots.some((bot) => bot.id === conversation.direct_bot_id);
    const teamMissing = conversation.team_id && !teams.some((team) => team.id === conversation.team_id);
    if (!botMissing && !teamMissing) return;
    if (refreshedForRef.current === conversation.id) return;
    refreshedForRef.current = conversation.id;
    if (botMissing) void refreshBots();
    if (teamMissing) {
      void api
        .listTeams()
        .then((result) => setTeams(result.teams))
        .catch(() => undefined);
    }
  }, [conversation, bots, teams, refreshBots]);

  const openSettings = useCallback((section = "appearance") => {
    setSettingsSection(section);
    setSettingsOpen(true);
  }, []);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const me = await api.me();
        const [botsResponse, conversationsResponse, teamsResponse, sidebarResponse, providerResponse] =
          await Promise.all([
            api.listBots(),
            api.listConversations(),
            api.listTeams(),
            api.getSidebar(),
            api.listProviders(),
          ]);
        if (cancelled) return;
        setUser(me.user);
        setHasProvider(
          providerResponse.providers.some(
            (provider) => provider.type === "llm" && provider.enabled,
          ),
        );
        setBots(botsResponse.bots);
        setConversations(conversationsResponse.conversations);
        setTeams(teamsResponse.teams);
        setSidebar(sidebarResponse);
        setBooting(false);
      } catch (err) {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setBootError(err instanceof Error ? err.message : "加载失败");
        setBooting(false);
      }
    })();
    return () => {
      cancelled = true;
      esRef.current?.close();
    };
  }, [router]);

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    preferredConversationRef.current = params.get("conv");
    setFocusMessageID(params.get("message"));
  }, []);

  useEffect(() => {
    if (!focusMessageID) return;
    const target = document.querySelector<HTMLElement>(`[data-message-id="${focusMessageID}"]`);
    if (!target) return;
    target.scrollIntoView({ behavior: "smooth", block: "center" });
    target.animate(
      [
        { outline: "2px solid transparent", outlineOffset: "4px" },
        { outline: "2px solid var(--sb-accent)", outlineOffset: "4px" },
        { outline: "2px solid transparent", outlineOffset: "4px" },
      ],
      { duration: 1600, easing: "ease-out" },
    );
    setFocusMessageID(null);
  }, [messages, activeId, focusMessageID]);

  const findMatches = useMemo(() => {
    const query = findQuery.trim().toLowerCase();
    if (!query) return [] as string[];
    return messages
      .filter((message) => (message.content ?? "").toLowerCase().includes(query))
      .map((message) => message.id);
  }, [messages, findQuery]);

  const gotoMatch = (delta: number) => {
    if (findMatches.length === 0) return;
    const next = (findIndex + delta + findMatches.length) % findMatches.length;
    setFindIndex(next);
    setFocusMessageID(findMatches[next]);
  };

  useEffect(() => {
    setFindIndex(0);
  }, [findQuery]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "f" && activeId) {
        event.preventDefault();
        setFindOpen(true);
      }
      if (event.key === "Escape") setFindOpen(false);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [activeId]);

  useEffect(() => {
    if (booting || !user || activeId) return;
    const preferred = preferredConversationRef.current;
    const primary = user.primary_bot_id;
    const primaryConversation = primary
      ? conversations.find((item) => item.direct_bot_id === primary)
      : undefined;
    const target =
      (preferred && conversations.some((item) => item.id === preferred) ? preferred : null) ||
      primaryConversation?.id ||
      conversations[0]?.id;
    if (target) {
      preferredConversationRef.current = null;
      void openConversation(target);
      return;
    }
    if (primary && bots.length > 0) {
      void api
        .createConversation({ kind: "direct", bot_id: primary })
        .then(async (result) => {
          setConversations((prev) =>
            prev.some((item) => item.id === result.conversation.id)
              ? prev
              : [result.conversation, ...prev],
          );
          await openConversation(result.conversation.id);
        })
        .catch(() => undefined);
    }
  }, [booting, user, activeId, conversations, bots, openConversation]);

  useEffect(() => {
    if (stickRef.current) scrollToBottom(cycleActive ? "auto" : "smooth");
  }, [messages.length, streams, errorMessage, cycleRetry, cycleFailure, cycleActive, scrollToBottom]);

  // Tool activities load after the messages; keep the merged tool summary in
  // view when the reader is already following the bottom of the chat.
  useEffect(() => {
    if (stickRef.current) scrollToBottom("auto");
  }, [tools, scrollToBottom]);

  useEffect(() => {
    const botID = conversation?.direct_bot_id;
    if (!botID) return;
    if (!messages.some((message) => message.sender_type === "bot")) return;
    const timer = window.setTimeout(() => {
      void api
        .markActivitiesRead(botID)
        .then(() => refreshSidebar())
        .catch(() => undefined);
    }, 700);
    return () => window.clearTimeout(timer);
  }, [conversation?.direct_bot_id, messages, refreshSidebar]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const meta = event.metaKey || event.ctrlKey;
      if (!meta) {
        if (event.key === "/" && document.activeElement?.tagName !== "INPUT" && document.activeElement?.tagName !== "TEXTAREA") {
          event.preventDefault();
          document.getElementById("superbot-composer")?.focus();
        }
        return;
      }
      if (event.key.toLowerCase() === "k") {
        event.preventDefault();
        setSearchQuery("");
        setSearchOpen(true);
      } else if (event.key.toLowerCase() === "n") {
        event.preventDefault();
        setNewBotError(null);
        setNewBotOpen(true);
      } else if (event.key === ",") {
        event.preventDefault();
        openSettings();
      } else if (event.key === "\\") {
        event.preventDefault();
        setInfoOpen((prev) => !prev);
      } else if (event.key.toLowerCase() === "b") {
        event.preventDefault();
        setSidebarCollapsed((prev) => !prev);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [router, openSettings]);

  useEffect(() => {
    if (!settingsOpen) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setSettingsOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [settingsOpen]);

  const activeBot = useMemo(
    () =>
      bots.find((bot) => bot.id === conversation?.direct_bot_id) ??
      (agentView ? bots.find((bot) => bot.id === agentView.botId) : undefined),
    [bots, conversation, agentView],
  );
  const isAgentView = conversation?.kind === "agent";
  const activeTeam = useMemo(
    () => teams.find((team) => team.id === conversation?.team_id),
    [teams, conversation],
  );
  const lastBotMessage = useMemo(
    () => [...messages].reverse().find((message) => message.sender_type === "bot"),
    [messages],
  );
  const currentStream = useMemo(() => Object.values(streams).at(-1), [streams]);
  // Where each reply still being written goes: after everything its bot had
  // read, so a message sent meanwhile shows below the reply, not between the
  // question and the answer.
  const streamSlots = useMemo(() => {
    const slots = new Map<number, StreamState[]>();
    for (const stream of Object.values(streams)) {
      let at = transcript.length;
      while (at > 0) {
        const item = transcript[at - 1];
        if (item.kind !== "message" || !unreadByReply(item.message, stream.seen)) break;
        at -= 1;
      }
      slots.set(at, [...(slots.get(at) ?? []), stream]);
    }
    return slots;
  }, [streams, transcript]);
  const toolsOfRuns = useCallback(
    (runIds: string[]) => runIds.flatMap((runId) => tools[runId] ?? []),
    [tools],
  );
  const identityName = isAgentView
    ? agentView?.title ?? conversation?.title ?? "superbot"
    : activeTeam?.name ?? activeBot?.name ?? conversation?.title ?? "superbot";
  const primaryBot = useMemo(
    () => bots.find((bot) => bot.id === user?.primary_bot_id),
    [bots, user?.primary_bot_id],
  );

  const uploadFiles = useCallback(
    async (files: File[]) => {
      if (files.length === 0) return;
      setUploadingAttachments(true);
      try {
        for (const file of files) {
          if (file.size > 10 * 1024 * 1024) {
            toast(`${file.name} 超过 10MB`, "danger");
            continue;
          }
          const attachment = await api.uploadAttachment(file);
          setPendingAttachments((prev) => [...prev, attachment]);
        }
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "附件上传失败", "danger");
      } finally {
        setUploadingAttachments(false);
      }
    },
    [toast],
  );

  const sendContent = useCallback(
    async (raw: string, attachmentsOverride?: Attachment[]) => {
      const attachments = attachmentsOverride ?? pendingAttachments;
      const content = raw.trim() || (attachments.length > 0 ? "附件" : "");
      if (!content || !activeId || sending) {
        (window as any).__sendGuard = { content, activeId, sending };
        return;
      }
      const tempId = `pending-${Date.now()}-${Math.random().toString(16).slice(2, 8)}`;
      const temp: Message = {
        id: tempId,
        ordinal: (messages.at(-1)?.ordinal ?? 0) + 1,
        cycle_id: null,
        sender_type: "user",
        sender_bot_id: null,
        content,
        metadata: {},
        run_id: null,
        parent_message_id: null,
        reactions: [],
        attachments,
        created_at: new Date().toISOString(),
      };
      pendingSendRef.current = { id: tempId, content };
      setMessages((prev) => [...prev, temp]);
      setInput("");
      setSending(true);
      setErrorMessage(null);
      setCycleFailure(null);
      stickRef.current = true;
      scrollToBottom("smooth");
      try {
        const clientMsgId =
          typeof crypto !== "undefined" && "randomUUID" in crypto
            ? crypto.randomUUID()
            : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
        const result = await api.sendMessage(
          activeId,
          content,
          clientMsgId,
          replyTarget?.id ?? null,
          attachments.map((attachment) => attachment.id),
        );
        setReplyTarget(null);
        setPendingAttachments([]);
        setMessages((prev) => {
          if (prev.some((item) => item.id === result.message.id)) {
            return prev
              .map((item) => (item.id === result.message.id ? result.message : item))
              .filter((item) => item.id !== tempId);
          }
          return prev.map((item) => (item.id === tempId ? result.message : item));
        });
        if (pendingSendRef.current?.id === tempId) pendingSendRef.current = null;
        if (result.interjection) {
          const messageID = result.message.id;
          const alreadyDelivered = deliveredInterjectionsRef.current.has(messageID);
          setInterjections((prev) => ({
            ...prev,
            [messageID]: alreadyDelivered ? "delivered" : "pending",
          }));
          if (alreadyDelivered) fadeDeliveredInterjections([messageID]);
        }
        setCycleActive(true);
        setConversations((prev) =>
          prev.map((item) =>
            item.id === activeId ? { ...item, updated_at: new Date().toISOString() } : item,
          ),
        );
      } catch (err) {
        pendingSendRef.current = null;
        setMessages((prev) => prev.filter((item) => item.id !== tempId));
        setInput(content);
        setErrorMessage(err instanceof ApiError ? err.message : "消息发送失败");
        setCycleActive(false);
      } finally {
        setSending(false);
      }
    },
    [
      activeId,
      fadeDeliveredInterjections,
      messages,
      pendingAttachments,
      replyTarget?.id,
      scrollToBottom,
      sending,
    ],
  );

  const openSidePanel = useCallback((focusInput: boolean) => {
    setSideOpen(true);
    // 0 leaves the focus where it is; any other new value moves it to the
    // panel's input.
    setSideFocusSignal((prev) => (focusInput ? prev + 1 : 0));
  }, []);

  const openGoalSection = useCallback(() => {
    setSideOpen(false);
    setInfoOpen(true);
    setGoalFocusSignal((prev) => prev + 1);
  }, []);

  // `/side question` asks on the side instead of sending a chat message.
  const { ask: askSide, pending: sidePending } = side;
  const submitComposer = useCallback(() => {
    const sideQuestion = parseSideCommand(input);
    if (sideQuestion === null) {
      void sendContent(input);
      return;
    }
    if (!activeId) return;
    if (!sideQuestion) {
      setInput("");
      openSidePanel(true);
      return;
    }
    openSidePanel(false);
    if (sidePending !== null) {
      // One at a time; the question stays in the composer.
      toast(t("side.waitPrevious"), "default");
      return;
    }
    setInput("");
    void askSide(sideQuestion);
  }, [activeId, askSide, input, openSidePanel, sendContent, sidePending, t, toast]);

  const insertIntoComposer = useCallback(
    (text: string) => {
      setInput((prev) => (prev.trim() ? `${prev.replace(/\s+$/, "")}\n\n${text}` : text));
      // On narrow windows the drawer covers the composer.
      if (window.innerWidth < 980) setSideOpen(false);
      toast(t("side.inserted"), "success");
      window.requestAnimationFrame(() => {
        document.getElementById("superbot-composer")?.focus();
      });
    },
    [t, toast],
  );

  // After a cycle gave up, "Retry" re-sends the last user message through the
  // normal send path. Messages with attachments are left to the user to
  // re-send, since their files belong to the original message.
  // A routine trigger is not the user's own words, so it is not re-sent as one.
  const retryCandidate = useMemo(() => {
    const last = [...messages].reverse().find((message) => message.sender_type === "user");
    if (!last || last.id.startsWith("pending-") || (last.attachments ?? []).length > 0) return null;
    if (isRoutineTrigger(last)) return null;
    return last;
  }, [messages]);

  const retryLastUserMessage = useCallback(async () => {
    if (!retryCandidate) return;
    const draft = input;
    const draftAttachments = pendingAttachments;
    await sendContent(retryCandidate.content, []);
    // sendContent clears the composer; give back what was being written.
    if (draft) setInput(draft);
    if (draftAttachments.length > 0) setPendingAttachments(draftAttachments);
  }, [input, pendingAttachments, retryCandidate, sendContent]);

  const sendVoice = useCallback(
    async (file: File) => {
      try {
        const attachment = await api.uploadAttachment(file);
        await sendContent(t("chat.voiceMessage"), [attachment]);
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "语音消息发送失败", "danger");
      }
    },
    [sendContent, toast],
  );

  const stop = useCallback(async () => {
    if (!activeId) return;
    try {
      await api.stopConversation(activeId);
      setCycleActive(false);
    } catch (err) {
      setErrorMessage(err instanceof ApiError ? err.message : "停止失败");
    }
  }, [activeId]);

  const createConversation = useCallback(
    async (botId: string) => {
      try {
        const result = await api.createConversation({ kind: "direct", bot_id: botId });
        const list = await api.listConversations();
        setConversations(list.conversations);
        await openConversation(result.conversation.id);
      } catch (err) {
        setErrorMessage(err instanceof ApiError ? err.message : "创建会话失败");
      }
    },
    [openConversation],
  );

  const createTeamConversation = useCallback(
    async (teamId: string) => {
      try {
        const result = await api.createConversation({ kind: "team", team_id: teamId });
        const list = await api.listConversations();
        setConversations(list.conversations);
        await openConversation(result.conversation.id);
      } catch (err) {
        setErrorMessage(err instanceof ApiError ? err.message : "创建团队会话失败");
      }
    },
    [openConversation],
  );

  const createBot = useCallback(
    async (payload: { name: string; avatar_shape: string; avatar_color: string }) => {
      setCreatingBot(true);
      setNewBotError(null);
      try {
        const result = await api.createBot({ ...payload, kind: newBotKind });
        setBots((prev) => [...prev, result.bot]);
        const me = await api.me();
        setUser(me.user);
        setNewBotOpen(false);
        await refreshSidebar();
        if (newBotKind === "personal") {
          await createConversation(result.bot.id);
        }
        setNewBotKind("personal");
      } catch (err) {
        setNewBotError(err instanceof ApiError ? err.message : "创建 Bot 失败");
      } finally {
        setCreatingBot(false);
      }
    },
    [createConversation, refreshSidebar],
  );

  const openBot = useCallback(
    async (bot: Bot) => {
      if (window.innerWidth < 860) setSidebarCollapsed(true);
      void api
        .markActivitiesRead(bot.id)
        .then(() => refreshSidebar())
        .catch(() => undefined);
      await createConversation(bot.id);
    },
    [createConversation, refreshSidebar],
  );

  // A bot's work for another bot happens in their private thread; open
  // that conversation in the main view so its tool calls and live output
  // show, read-only, with a way back.
  const openAgentWork = useCallback(
    async (detail: AgentThreadDetail, focusBotId?: string) => {
      const returnTo = activeIdRef.current;
      setAgentThreadID(null);
      await openConversation(detail.conversation.id);
      setAgentView({
        botId: focusBotId ?? detail.bot_b.id,
        returnTo: returnTo && returnTo !== detail.conversation.id ? returnTo : null,
        title: `${detail.bot_a.name} ↔ ${detail.bot_b.name}`,
      });
    },
    [openConversation],
  );

  const openGroup = useCallback(
    async (team: Team) => {
      if (window.innerWidth < 860) setSidebarCollapsed(true);
      void api
        .setTeamUnread(team.id, false)
        .then(() => refreshSidebar())
        .catch(() => undefined);
      if (team.room_conversation_id) {
        await openConversation(team.room_conversation_id);
        return;
      }
      await createTeamConversation(team.id);
    },
    [createTeamConversation, openConversation],
  );

  const importTemplate = useCallback(
    async (template: Record<string, unknown>) => {
      try {
        const result = await api.importBotTemplate(template);
        setBots((prev) => [...prev, result.bot]);
        const me = await api.me();
        setUser(me.user);
        await refreshSidebar();
        await createConversation(result.bot.id);
        toast("Bot 模板已导入", "success");
      } catch (err) {
        setErrorMessage(err instanceof ApiError ? err.message : "导入模板失败");
      }
    },
    [createConversation, refreshSidebar, toast],
  );

  const createSection = useCallback(
    async (name: string) => {
      await api.createSection(name);
      await refreshSidebar();
    },
    [refreshSidebar],
  );

  const createGroup = useCallback(
    async (payload: { name: string; bot_ids: string[] }) => {
      setCreatingGroup(true);
      setNewGroupError(null);
      try {
        const result = await api.createTeam({
          name: payload.name,
          description: "",
          mode: "group",
          config: { max_rounds: 6 },
          bot_ids: payload.bot_ids,
        });
        setTeams((prev) => [...prev, result.team]);
        setNewGroupOpen(false);
        await refreshSidebar();
        if (result.team.room_conversation_id) {
          await openConversation(result.team.room_conversation_id);
        } else {
          await createTeamConversation(result.team.id);
        }
      } catch (err) {
        setNewGroupError(err instanceof ApiError ? err.message : "创建群聊失败");
      } finally {
        setCreatingGroup(false);
      }
    },
    [createTeamConversation, openConversation, refreshSidebar],
  );

  // Hands the primary role to another personal Bot, from the sidebar menu,
  // the Bot profile dialog or Settings.
  const setPrimaryBot = useCallback(
    async (bot: Bot, from: "dialog" | "menu" = "menu") => {
      setSettingPrimary(true);
      setPrimaryError(null);
      try {
        const result = await api.setPrimaryBot(bot.id);
        setUser(result.user);
        // The open profile stays on the Bot it was opened for; it only needs
        // the fresh copy when that Bot is the one that became primary.
        if (from === "dialog") {
          setInfoBot((current) => (current?.id === result.primary_bot.id ? result.primary_bot : current));
        }
        await refreshSidebar();
        toast(t("sidebar.primarySet", { name: result.primary_bot.name }), "success");
      } catch (err) {
        const message = err instanceof ApiError ? err.message : t("sidebar.primaryFailed");
        if (from === "dialog") setPrimaryError(message);
        else toast(message, "danger");
      } finally {
        setSettingPrimary(false);
      }
    },
    [refreshSidebar, t, toast],
  );

  const saveBotProfile = useCallback(
    async (payload: {
      name: string;
      title: string;
      description: string;
      avatar_shape: string;
      avatar_color: string;
      section_id: string | null;
      pinned: boolean;
      provider_id: string | null;
      model: string;
      params: Record<string, unknown> | null;
      visibility: "owner" | "account";
    }) => {
      if (!infoBot) return;
      setSavingBotProfile(true);
      setBotProfileError(null);
      try {
        const result = await api.updateBotProfile(infoBot.id, { ...botProfilePayload(infoBot), ...payload });
        // Model/provider are not profile columns; persist them through the bot
        // update endpoint so the settings dialog is not a dead end.
        if (payload.provider_id !== undefined || payload.model !== undefined) {
          await api.updateBot(infoBot.id, {
            name: payload.name,
            description: payload.description,
            avatar: infoBot.avatar,
            system_prompt: infoBot.system_prompt,
            provider_id: payload.provider_id,
            model: payload.model,
            params: payload.params ?? infoBot.params,
            capabilities: infoBot.capabilities,
            tool_policy: infoBot.tool_policy,
            memory_enabled: infoBot.memory_enabled,
            computer_profile: infoBot.computer_profile,
          });
        }
        setBots((prev) => prev.map((bot) => (bot.id === result.bot.id ? result.bot : bot)));
        await refreshSidebar();
        setInfoBot(null);
        toast("已保存", "success");
      } catch (err) {
        setBotProfileError(err instanceof ApiError ? err.message : "保存 Bot 资料失败");
      } finally {
        setSavingBotProfile(false);
      }
    },
    [infoBot, refreshSidebar, toast],
  );

  const openInfo = useCallback((bot: Bot, tab: "profile" | "memory" = "profile") => {
    setBotProfileError(null);
    setPrimaryError(null);
    setInfoTab(tab);
    setInfoBot(bot);
  }, []);

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } finally {
      closeSSE();
      router.replace("/login");
    }
  }, [closeSSE, router]);

  const createShareLink = useCallback(async () => {
    if (!activeBot) return;
    try {
      const result = await api.createBotShare(activeBot.id);
      await copyText(`${window.location.origin}/share/${result.token}`);
      toast("分享链接已复制", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "创建分享链接失败", "danger");
    }
  }, [activeBot, toast]);

  const resolveApproval = useCallback(
    async (approval: ToolApproval, decision: "allow" | "deny", always = false) => {
      try {
        if (always) {
          await api.createPermissionRule("allow", approval.tool_name);
        }
        await api.resolveToolApproval(approval.id, decision);
        setApprovals((prev) => prev.filter((item) => item.id !== approval.id));
        toast(decision === "allow" ? "已允许" : "已拒绝", "success");
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "审批操作失败", "danger");
      }
    },
    [toast],
  );

  const replyToMessage = useCallback((message: Message) => {
    setReplyTarget(message);
    window.requestAnimationFrame(() => {
      document.getElementById("superbot-composer")?.focus();
    });
  }, []);

  const copyLink = useCallback(async () => {
    if (!activeId) return;
    try {
      await copyText(`${window.location.origin}/?conv=${activeId}`);
      toast("链接已复制", "success");
    } catch {
      toast("复制失败", "danger");
    }
  }, [activeId, toast]);

  const onTranscriptScroll = useCallback(() => {
    const el = transcriptRef.current;
    if (!el) return;
    const distance = el.scrollHeight - el.scrollTop - el.clientHeight;
    stickRef.current = distance < 90;
    setShowJump(distance > 220);
    if (distance < 90) setNewMessages(0);
  }, []);

  if (booting) {
    return (
      <div className="flex h-screen flex-col items-center justify-center gap-4 bg-[var(--sb-bg)]">
        <Logo />
        <Loader2 className="sb-spin h-4 w-4 text-[var(--sb-text-3)]" />
      </div>
    );
  }

  if (!user) {
    return (
      <div className="flex h-screen items-center justify-center bg-[var(--sb-bg)] p-6">
        <div className="sb-card max-w-md p-6 text-center">
          <div className="text-[15px] font-medium">无法连接 superbot</div>
          <p className="mt-2 text-[12px] text-[var(--sb-text-2)]">
            {bootError ?? "请检查 API 是否已启动。"}
          </p>
          <button
            type="button"
            onClick={() => router.replace("/login")}
            className="sb-btn mt-4"
            data-variant="primary"
          >
            去登录
          </button>
        </div>
      </div>
    );
  }

  const isEmptyAccount = bots.length === 0;
  const lastBotMessageID = lastBotMessage?.id;

  // The replies still being written that go before transcript item `at`
  // (or after the last one), each under the tool calls it has made so far.
  const renderStreams = (at: number) => {
    const list = streamSlots.get(at);
    if (!list) return null;
    const prev = transcript[at - 1];
    const now = new Date().toISOString();
    const showDate = !prev || dateKey(itemMessage(prev).created_at) !== dateKey(now);
    return list.map((stream, index) => {
      const streamTools = toolsOfRuns([...(runChains[stream.runId] ?? []), stream.runId]);
      return (
        <Fragment key={`stream-${stream.runId}`}>
          {showDate && index === 0 && <div className="sb-date-sep">{dateLabel(now)}</div>}
          {streamTools.length > 0 && (
            <div className="mb-2 space-y-1.5">
              <ToolGroup tools={streamTools} />
            </div>
          )}
          <StreamingMessage
            stream={stream}
            bot={bots.find((bot) => bot.id === stream.botId) ?? activeBot}
            status={runStatus[stream.runId]}
            showSender={Boolean(activeTeam)}
          />
        </Fragment>
      );
    });
  };

  return (
    <div className="sb-shell">
      <GrokSidebar
        user={user}
        data={sidebar}
        activeBotId={activeBot?.id ?? null}
        activeTeamId={activeTeam?.id ?? null}
        primaryBotId={user.primary_bot_id}
        collapsed={sidebarCollapsed}
        onOpenBot={(bot) => void openBot(bot)}
        onOpenInfo={(bot) => openInfo(bot)}
        onSetPrimary={(bot) => setPrimaryBot(bot)}
        onOpenGroup={(team) => void openGroup(team)}
        onNewBot={() => {
          setNewBotError(null);
          setNewBotKind("personal");
          setNewBotOpen(true);
        }}
        onNewTeamBot={() => {
          setNewBotError(null);
          setNewBotKind("team");
          setNewBotOpen(true);
        }}
        onNewGroup={() => {
          setNewGroupError(null);
          setNewGroupOpen(true);
        }}
        onOpenSettings={openSettings}
        onOpenNotifications={() => setNotificationSignal((value) => value + 1)}
        onLogout={logout}
        onToggleSection={(section) => void toggleSection(section)}
        onCreateSection={createSection}
        onOpenSearch={(query) => {
          setSearchQuery(query);
          setSearchOpen(true);
        }}
        onImportTemplate={importTemplate}
        onRefresh={async () => {
          await Promise.all([refreshSidebar(), refreshBots()]);
        }}
        onOpenExtensions={() => {
          setConnectorsProvider(null);
          setConnectorsOpen(true);
        }}
        onOpenHelp={() => setHelpOpen(true)}
        onInstallApp={() => {
          const prompt = installPromptRef.current;
          if (prompt) {
            void prompt.prompt();
            installPromptRef.current = null;
          } else {
            toast("请在浏览器菜单中选择「安装应用」", "default");
          }
        }}
      />

      {!sidebarCollapsed && (
        <button
          type="button"
          aria-label="关闭侧栏"
          className="fixed inset-0 z-30 bg-black/25 md:hidden"
          onClick={() => setSidebarCollapsed(true)}
        />
      )}

      <main className={cn("sb-main", (infoOpen || sideOpen) && "sb-info-open")}>
        <header className="sb-header">
          <div className="flex items-center pl-1">
            <button
              type="button"
              className={cn("sb-icon-btn", !sidebarCollapsed && "md:hidden")}
              data-variant="plain"
              onClick={() => setSidebarCollapsed((prev) => !prev)}
              aria-label="切换侧栏"
              title="切换侧栏（⌘B）"
            >
              <PanelLeft className="h-[17px] w-[17px]" />
            </button>
            {!live && (
              <span
                className="ml-2 hidden h-1.5 w-1.5 rounded-full bg-[var(--sb-warn)] md:block"
                title="正在重连"
              />
            )}
          </div>

          <button
            type="button"
            className="sb-header-identity min-w-0"
            onClick={() => {
              if (!activeId) return;
              if (sideOpen) {
                setSideOpen(false);
                setInfoOpen(true);
                return;
              }
              setInfoOpen((prev) => !prev);
            }}
            title="查看对话详情"
          >
            {activeTeam ? (
              <span className="flex -space-x-2">
                {activeTeam.bots.slice(0, 2).map((entry) => (
                  <BotAvatar
                    key={entry.bot.id}
                    name={entry.bot.name}
                    id={entry.bot.id}
                    size={26}
                    shape={entry.bot.avatar_shape}
                    color={entry.bot.avatar_color}
                    avatar={entry.bot.avatar}
                    className="ring-2 ring-[var(--sb-pane)]"
                  />
                ))}
              </span>
            ) : activeBot ? (
              <BotAvatar
                name={activeBot.name}
                id={activeBot.id}
                size={26}
                shape={activeBot.avatar_shape}
                color={activeBot.avatar_color}
                avatar={activeBot.avatar}
              />
            ) : (
              <span className="flex h-[26px] w-[26px] items-center justify-center rounded-full bg-[var(--sb-hover)] text-[var(--sb-text-2)]">
                <Sparkles className="h-3.5 w-3.5" />
              </span>
            )}
            <span className="min-w-0 truncate text-[15px] font-medium leading-[20px]">{identityName}</span>
            {(!infoOpen || sideOpen) && <ChevronRight className="h-3.5 w-3.5 shrink-0 text-[var(--sb-text-3)]" />}
          </button>

          <div className="sb-header-actions flex items-center justify-end gap-2 pr-0.5">
            {activeTeam && <GoalChip goal={goalCtl.data?.goal} onClick={openGoalSection} />}
            <button
              type="button"
              className="sb-icon-btn relative"
              onClick={() => {
                if (sideOpen) setSideOpen(false);
                else openSidePanel(true);
              }}
              disabled={!activeId}
              aria-label={t("side.title")}
              aria-pressed={sideOpen}
              title={t("side.title")}
              data-side-toggle
            >
              <MessageCircleQuestion className="h-[16px] w-[16px]" />
              {side.items.length > 0 && (
                <span
                  className="absolute -right-1 -top-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-[var(--sb-primary)] px-1 text-[10px] leading-none tabular-nums text-[var(--sb-primary-contrast)]"
                  aria-label={t("side.count", { n: side.items.length })}
                >
                  {side.items.length}
                </span>
              )}
            </button>
            <button
              type="button"
              className="sb-icon-btn"
              onClick={(event) =>
                setShareAnchor({ rect: event.currentTarget.getBoundingClientRect() })
              }
              disabled={!activeId}
              aria-label="分享"
              title="分享"
            >
              <Upload className="h-[16px] w-[16px]" />
            </button>
          </div>
        </header>
        <NotificationCenter
          hideTrigger
          openSignal={notificationSignal}
          onOpenConversation={(conversationID, messageID) => {
            if (messageID) setFocusMessageID(messageID);
            void openConversation(conversationID);
          }}
        />

        {findOpen && activeId && (
          <div className="sb-find-bar" role="search" aria-label="在会话中查找">
            <input
              autoFocus
              value={findQuery}
              onChange={(event) => setFindQuery(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") gotoMatch(event.shiftKey ? -1 : 1);
              }}
              placeholder="在会话中查找"
              className="h-7 w-[150px] bg-transparent text-[12px] outline-none placeholder:text-[var(--sb-text-4)]"
            />
            <span className="text-[11px] tabular-nums text-[var(--sb-text-3)]">
              {findMatches.length === 0 ? "0/0" : `${findIndex + 1}/${findMatches.length}`}
            </span>
            <button type="button" onClick={() => gotoMatch(-1)} aria-label="上一个" className="sb-icon-btn" data-size="sm" data-variant="plain">↑</button>
            <button type="button" onClick={() => gotoMatch(1)} aria-label="下一个" className="sb-icon-btn" data-size="sm" data-variant="plain">↓</button>
            <button type="button" onClick={() => setFindOpen(false)} aria-label="关闭查找" className="sb-icon-btn" data-size="sm" data-variant="plain">✕</button>
          </div>
        )}

        {activeId ? (
          <>
            <section
              className="sb-transcript relative"
              ref={transcriptRef}
              onScroll={onTranscriptScroll}
            >
              <div className="sb-transcript-inner mx-auto w-full max-w-[820px]">
                {!loadingConversation && !isAgentView && conversation?.direct_bot_id && activeBot && (
                  <BotWorkBanner
                    bot={activeBot}
                    refreshKey={messages.length}
                    onView={(detail) => void openAgentWork(detail, activeBot.id)}
                  />
                )}
                {loadingConversation && (
                  <div className="space-y-3 py-8">
                    <div className="sb-skeleton h-16 w-[62%]" />
                    <div className="sb-skeleton ml-auto h-10 w-[40%]" />
                    <div className="sb-skeleton h-24 w-[70%]" />
                  </div>
                )}
                {!loadingConversation && messages.length === 0 && !currentStream && (
                  <EmptyRoom
                    bot={activeBot}
                    team={activeTeam}
                    onPick={(text) => {
                      setInput(text);
                      document.getElementById("superbot-composer")?.focus();
                    }}
                  />
                )}
                {transcript.map((item, index) => {
                  const prevItem = transcript[index - 1];
                  const first = itemMessage(item, "first");
                  const showDate = !prevItem || dateKey(itemMessage(prevItem).created_at) !== dateKey(first.created_at);
                  const streamsHere = renderStreams(index);
                  if (item.kind === "agents") {
                    return (
                      <Fragment key={item.key}>
                        {streamsHere}
                        {showDate && <div className="sb-date-sep">{dateLabel(first.created_at)}</div>}
                        <AgentThreadGroup
                          threads={item.threads}
                          bots={bots}
                          bot={activeBot}
                          onOpenAgentThread={(threadID) => setAgentThreadID(threadID)}
                        />
                      </Fragment>
                    );
                  }
                  const message = item.message;
                  const isTeam = Boolean(activeTeam);
                  const position = groupPosition(transcriptAnchors, index);
                  const senderVisible =
                    isTeam && message.sender_type === "bot" && (position === "first" || position === "single");
                  // The tool calls a reply made sit right above it.
                  const replyTools = message.sender_type === "bot" ? toolsOfRuns(replyRunIDs(message)) : [];
                  return (
                    <Fragment key={message.id}>
                      {streamsHere}
                      {showDate && <div className="sb-date-sep">{dateLabel(message.created_at)}</div>}
                      {replyTools.length > 0 && (
                        <div className="mb-2 space-y-1.5">
                          <ToolGroup tools={replyTools} />
                        </div>
                      )}
                      <ChatMessage
                        message={message}
                        bot={
                          message.sender_bot_id
                            ? bots.find((bot) => bot.id === message.sender_bot_id)
                            : activeBot
                        }
                        bots={bots}
                        groupPosition={position}
                        showSender={senderVisible}
                        parentMessage={
                          message.parent_message_id
                            ? visibleMessages.find((item) => item.id === message.parent_message_id)
                            : undefined
                        }
                        conversationId={activeId}
                        interjection={interjections[message.id]}
                        onReply={replyToMessage}
                        onOpenAgentThread={(threadID) => setAgentThreadID(threadID)}
                        onMessageUpdated={(updated) =>
                          setMessages((prev) =>
                            prev.map((item) => (item.id === updated.id ? updated : item)),
                          )
                        }
                        onMessageDeleted={(messageID) =>
                          setMessages((prev) => prev.filter((item) => item.id !== messageID))
                        }
                      />
                    </Fragment>
                  );
                })}
                {renderStreams(transcript.length)}
                {cycleRetry && <CycleRetryRow retry={cycleRetry} />}
                {cycleFailure !== null && (
                  <CycleFailureRow
                    error={cycleFailure}
                    onRetry={retryCandidate && !sending ? () => void retryLastUserMessage() : undefined}
                    onDismiss={() => setCycleFailure(null)}
                  />
                )}
                {errorMessage && (
                  <div className="mx-auto mt-2 w-full rounded-xl bg-[var(--sb-danger-soft)] px-3.5 py-2.5 text-[12px] leading-[18px] text-[var(--sb-danger)]">
                    {errorMessage}
                  </div>
                )}
                <div ref={bottomRef} className="h-2" />
              </div>
            </section>

            {newMessages > 0 ? (
              <div className="absolute bottom-[88px] left-1/2 z-20 flex h-9 -translate-x-1/2 items-center rounded-full bg-[var(--sb-primary)] text-[var(--sb-primary-contrast)] shadow-[var(--sb-shadow-popup)]">
                <button
                  type="button"
                  onClick={() => {
                    scrollToBottom("smooth");
                    setNewMessages(0);
                  }}
                  className="flex h-full items-center gap-1.5 pl-3 pr-2 text-[13px]"
                >
                  <ArrowDown className="h-4 w-4" />
                  {newMessages} 条新消息
                </button>
                <button
                  type="button"
                  onClick={() => setNewMessages(0)}
                  className="flex h-full items-center pr-2.5 pl-1 opacity-70 transition hover:opacity-100"
                  aria-label="忽略新消息"
                >
                  <X className="h-3.5 w-3.5" />
                </button>
              </div>
            ) : showJump ? (
              <button
                type="button"
                onClick={() => scrollToBottom("smooth")}
                className="sb-icon-btn absolute bottom-[88px] left-1/2 z-20 -translate-x-1/2 shadow-[var(--sb-shadow-popup)]"
                aria-label="回到底部"
                title="回到底部"
              >
                <ArrowDown className="h-4 w-4" />
              </button>
            ) : null}

            <div className="sb-composer-wrap">
              <div className="mx-auto w-full max-w-[820px]">
                {approvals.map((approval) => (
                  <div
                    key={approval.id}
                    className="mb-2 rounded-xl border border-[var(--sb-warn)]/30 bg-[var(--sb-warn-soft)] p-3"
                  >
                    <div className="flex items-center gap-2 text-[12px] font-medium text-[var(--sb-warn)]">
                      <ShieldAlert className="h-3.5 w-3.5" />
                      {ruleRequest(approval)
                        ? "主 Bot 申请一批免审批规则"
                        : `需要你的批准：${approval.tool_name}`}
                    </div>
                    {(() => {
                      const request = ruleRequest(approval);
                      if (!request) {
                        return (
                          <pre className="mt-2 max-h-28 overflow-auto rounded-lg bg-[var(--sb-pane)] p-2 font-[var(--sb-mono)] text-[11px] leading-[17px] text-[var(--sb-text-2)]">
                            {JSON.stringify(approval.args ?? {}, null, 2)}
                          </pre>
                        );
                      }
                      return (
                        <div className="mt-2 rounded-lg bg-[var(--sb-pane)] p-2.5 text-[12px] leading-[18px]">
                          {request.summary && (
                            <div className="mb-1.5 text-[var(--sb-text-2)]">{request.summary}</div>
                          )}
                          <ul className="max-h-40 space-y-1 overflow-auto">
                            {request.rules.map((rule) => (
                              <li key={rule.pattern} className="flex flex-wrap items-baseline gap-x-2">
                                <code className="font-[var(--sb-mono)] text-[11px] text-[var(--sb-text)]">
                                  {rule.pattern}
                                </code>
                                <span className="text-[var(--sb-text-3)]">{rule.reason}</span>
                              </li>
                            ))}
                          </ul>
                          <div className="mt-1.5 text-[11px] text-[var(--sb-text-3)]">
                            批准后对账号下所有 Bot 生效，可在「设置 → 权限」随时删除。
                          </div>
                        </div>
                      );
                    })()}
                    <div className="mt-2 flex justify-end gap-2">
                      <button
                        type="button"
                        className="sb-btn"
                        data-variant="secondary"
                        data-size="sm"
                        onClick={() => void resolveApproval(approval, "deny")}
                      >
                        拒绝
                      </button>
                      {ruleRequest(approval) ? (
                        <button
                          type="button"
                          className="sb-btn"
                          data-variant="primary"
                          data-size="sm"
                          onClick={() => void resolveApproval(approval, "allow")}
                        >
                          批准这批规则
                        </button>
                      ) : (
                        <>
                          <button
                            type="button"
                            className="sb-btn"
                            data-variant="secondary"
                            data-size="sm"
                            onClick={() => void resolveApproval(approval, "allow")}
                          >
                            允许一次
                          </button>
                          <button
                            type="button"
                            className="sb-btn"
                            data-variant="primary"
                            data-size="sm"
                            onClick={() => void resolveApproval(approval, "allow", true)}
                          >
                            始终允许
                          </button>
                        </>
                      )}
                    </div>
                  </div>
                ))}
                {replyTarget && (
                  <div className="mb-2 flex h-9 items-center gap-2 rounded-xl bg-[var(--sb-hover)] px-3">
                    <CornerDownRight className="h-3.5 w-3.5 shrink-0 text-[var(--sb-text-3)]" />
                    <div className="min-w-0 flex-1 truncate text-[13px] text-[var(--sb-text-2)]">
                      {replyTarget.content}
                    </div>
                    <button
                      type="button"
                      onClick={() => setReplyTarget(null)}
                      className="flex h-6 w-6 shrink-0 items-center justify-center rounded text-[var(--sb-text-3)] transition hover:bg-[var(--sb-active)] hover:text-[var(--sb-text)]"
                      aria-label="取消回复"
                    >
                      <X className="h-3.5 w-3.5" />
                    </button>
                  </div>
                )}
                {isAgentView ? (
                  <div className="flex min-h-12 items-center justify-between gap-3 rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] px-4 py-2 text-[13px] text-[var(--sb-text-2)]">
                    <span>
                      这是 {agentView?.title ?? conversation?.title} 之间的私信，只读；结果会自动交回给发起方。
                    </span>
                    {agentView?.returnTo && (
                      <button
                        type="button"
                        className="sb-btn shrink-0"
                        data-variant="secondary"
                        data-size="sm"
                        onClick={() => void openConversation(agentView.returnTo as string)}
                      >
                        返回
                      </button>
                    )}
                  </div>
                ) : (
                  <Composer
                    value={input}
                    onChange={setInput}
                    onSend={submitComposer}
                    onSideQuestion={() => openSidePanel(true)}
                    onStop={() => void stop()}
                    busy={cycleActive}
                    placeholder={language === "en" ? `${t("page.messageTo")} ${identityName}` : `给 ${identityName} 发消息`}
                    onOpenVoice={() => setVoiceOpen(true)}
                    mentionBots={activeTeam?.bots.map((entry) => entry.bot) ?? []}
                    attachments={pendingAttachments}
                    uploadingAttachments={uploadingAttachments}
                    onUploadFiles={(files) => void uploadFiles(files)}
                    onSendVoice={sendVoice}
                    onRemoveAttachment={(id) =>
                      setPendingAttachments((prev) => prev.filter((item) => item.id !== id))
                    }
                  />
                )}
              </div>
            </div>
          </>
        ) : (
          <section className="flex flex-1 items-center justify-center overflow-y-auto px-6">
            <div className="max-w-md pb-20 text-center">
              <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-[var(--sb-hover)]">
                {isEmptyAccount ? (
                  <MessageSquarePlus className="h-6 w-6 text-[var(--sb-text-2)]" />
                ) : (
                  <Sparkles className="h-6 w-6 text-[var(--sb-text-2)]" />
                )}
              </div>
              <h2 className="mt-5 text-[22px] font-semibold tracking-[-0.02em]">
                {isEmptyAccount && !hasProvider
                  ? "连接一个模型"
                  : isEmptyAccount
                    ? "创建你的主 Bot"
                    : "开始一段对话"}
              </h2>
              <p className="mx-auto mt-2 max-w-sm text-[13px] leading-[20px] text-[var(--sb-text-2)]">
                {isEmptyAccount && !hasProvider
                  ? "先添加一个 LLM Provider 和模型；主 Bot 会用这个模型工作。"
                  : isEmptyAccount
                    ? "主 Bot 是你的默认助手，也是创建其他 Bot 和群聊的入口。给它一个名字和头像即可开始。"
                    : "从侧栏选择一个 Bot，或者创建一个新的 AI 同事。"}
              </p>
              <div className="mt-6 flex items-center justify-center gap-2">
                {isEmptyAccount && !hasProvider ? (
                  <button
                    type="button"
                    onClick={() => openSettings("providers")}
                    className="sb-btn"
                    data-variant="primary"
                    data-size="lg"
                  >
                    <Sparkles className="h-4 w-4" />
                    配置 Provider
                  </button>
                ) : (
                  <button
                    type="button"
                    onClick={() => {
                      setNewBotError(null);
                      setNewBotOpen(true);
                    }}
                    className="sb-btn"
                    data-variant="primary"
                    data-size="lg"
                  >
                    <Plus className="h-4 w-4" />
                    新建 Bot
                  </button>
                )}
                {primaryBot && (
                  <button
                    type="button"
                    onClick={() => void openBot(primaryBot)}
                    className="sb-btn"
                    data-variant="outline"
                    data-size="lg"
                  >
                    打开主 Bot
                  </button>
                )}
              </div>
            </div>
          </section>
        )}
      </main>

      {sideOpen && activeId && (
        <SidePanel
          ctl={side}
          focusSignal={sideFocusSignal}
          onClose={() => setSideOpen(false)}
          onInsert={insertIntoComposer}
        />
      )}

      {infoOpen && activeId && !sideOpen && (
        <BotDetailPanel
          bot={activeBot}
          team={activeTeam}
          bots={bots}
          conversation={conversation}
          isPrimary={Boolean(activeBot && user.primary_bot_id === activeBot.id)}
          onClose={() => setInfoOpen(false)}
          onOpenComputer={() => setComputerOpen(true)}
          onOpenRoutines={(bot) => setRoutineBot(bot)}
          onOpenExtensions={(provider) => {
            setConnectorsProvider(provider ?? null);
            setConnectorsOpen(true);
          }}
          onOpenBot={(bot) => void openBot(bot)}
          onOpenInfo={(bot) => openInfo(bot)}
          onOpenActivity={(activity) => {
            void api
              .markActivitiesRead(activity.bot_id)
              .then(() => refreshSidebar())
              .catch(() => undefined);
            if (activity.agent_thread_id) {
              setAgentThreadID(activity.agent_thread_id);
              return;
            }
            if (activity.conversation_id) {
              void openConversation(activity.conversation_id);
            }
          }}
          onOpenTeams={() => router.push("/teams")}
          onRefresh={async () => {
            await Promise.all([refreshSidebar(), refreshBots()]);
          }}
          goalSection={activeTeam ? <GoalPanel ctl={goalCtl} bots={bots} /> : undefined}
          goalFocusSignal={goalFocusSignal}
          routinesSection={
            activeTeam ? <GroupRoutineList teamId={activeTeam.id} teamName={activeTeam.name} /> : undefined
          }
        />
      )}

      {infoBot && (
        <BotInfoDialog
          key={`${infoBot.id}-${infoTab}`}
          bot={infoBot}
          initialTab={infoTab}
          sections={sidebar?.sections ?? []}
          saving={savingBotProfile}
          error={botProfileError}
          isPrimary={user.primary_bot_id === infoBot.id}
          settingPrimary={settingPrimary}
          primaryError={primaryError}
          onSetPrimary={() => void setPrimaryBot(infoBot, "dialog")}
          primaryCandidates={bots.filter((bot) => bot.kind !== "team" && bot.id !== infoBot.id)}
          onReplacePrimary={(bot) => void setPrimaryBot(bot, "dialog")}
          onClose={() => {
            setInfoBot(null);
            setPrimaryError(null);
          }}
          onSave={(payload) => void saveBotProfile(payload)}
        />
      )}

      {routineBot && (
        <RoutineDialog
          bot={routineBot}
          onClose={() => {
            setRoutineBot(null);
            void refreshSidebar();
          }}
        />
      )}

      <NewBotDialog
        open={newBotOpen}
        team={newBotKind === "team"}
        creating={creatingBot}
        error={newBotError}
        onClose={() => setNewBotOpen(false)}
        onCreate={(payload) => void createBot(payload)}
      />

      <NewGroupDialog
        open={newGroupOpen}
        bots={bots}
        primaryBotId={user.primary_bot_id}
        creating={creatingGroup}
        error={newGroupError}
        onClose={() => setNewGroupOpen(false)}
        onCreate={(payload) => void createGroup(payload)}
      />

      {voiceOpen && (
        <VoiceCallPanel
          name={identityName}
          botID={activeTeam?.bots[0]?.bot.id ?? activeBot?.id ?? "voice"}
          latestBotText={lastBotMessage?.content ?? ""}
          onSend={sendContent}
          onClose={() => setVoiceOpen(false)}
          onOpenSettings={() => setInfoOpen(true)}
        />
      )}

      {agentThreadID && (
        <AgentThreadPanel
          threadID={agentThreadID}
          onClose={() => setAgentThreadID(null)}
          onOpenLive={(detail) => void openAgentWork(detail)}
        />
      )}

      <SearchDialog
        open={searchOpen}
        initialQuery={searchQuery}
        bots={bots}
        onClose={() => setSearchOpen(false)}
        onOpenBot={(bot) => void openBot(bot)}
        onOpenMessage={(conversationID, messageID) => {
          setFocusMessageID(messageID);
          void openConversation(conversationID);
        }}
        onNavigate={(href) => {
          if (href === "/settings") openSettings();
          else router.push(href);
        }}
      />

      {computerOpen && activeBot && (
        <ComputerPanel bot={activeBot} onClose={() => setComputerOpen(false)} />
      )}

      <ShareMenu
        open={shareAnchor !== null}
        anchor={shareAnchor}
        bot={activeBot ?? null}
        onClose={() => setShareAnchor(null)}
        onCreate={() => void createShareLink()}
        onCopyLink={() => void copyLink()}
      />

      <HelpDialog open={helpOpen} onClose={() => setHelpOpen(false)} />

      <ConnectorsDialog
        open={connectorsOpen}
        initialProvider={connectorsProvider}
        onClose={() => {
          setConnectorsOpen(false);
          setConnectorsProvider(null);
        }}
        onOpenBots={() => {
          setConnectorsOpen(false);
          router.push("/bots");
        }}
      />

      {settingsOpen && (
        <div
          className="sb-overlay !p-4 md:!p-6"
          onMouseDown={(event) => {
            if (event.target === event.currentTarget) setSettingsOpen(false);
          }}
        >
          <div
            className="relative z-10 h-[670px] max-h-[88vh] w-full max-w-[900px] overflow-hidden rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] shadow-[var(--sb-shadow-modal)]"
            style={{ animation: "sb-modal-in 0.18s cubic-bezier(0.16,1,0.3,1) both" }}
          >
            <SettingsScreen
              embedded
              initialSection={settingsSection}
              onClose={() => setSettingsOpen(false)}
              onPrimaryBotChange={(next) => {
                setUser(next);
                void refreshSidebar();
              }}
            />
          </div>
        </div>
      )}
    </div>
  );
}

function CycleRetryRow({ retry }: { retry: CycleRetry }) {
  const { t } = useLanguage();
  const [open, setOpen] = useState(false);
  const label =
    retry.maxAttempts > 0
      ? t("chat.cycle.retrying", {
          attempt: Math.min(retry.attempt + 1, retry.maxAttempts),
          max: retry.maxAttempts,
        })
      : t("chat.cycle.retryingNoCount");
  return (
    <div className="py-1" role="status" data-cycle-status="retrying">
      <button
        type="button"
        className="sb-agent-card"
        aria-expanded={open}
        title={retry.error}
        onClick={() => setOpen((prev) => !prev)}
      >
        <Loader2 className="sb-spin h-3.5 w-3.5 shrink-0" />
        <span>{label}</span>
        <ChevronRight className={cn("h-3 w-3 opacity-50 transition", open && "rotate-90")} />
      </button>
      {open && (
        <div className="mx-auto max-w-[520px] whitespace-pre-wrap break-words rounded-lg bg-[var(--sb-hover)] px-2.5 py-1.5 text-[11px] leading-[16px] text-[var(--sb-text-2)]">
          {retry.error}
        </div>
      )}
    </div>
  );
}

function CycleFailureRow({
  error,
  onRetry,
  onDismiss,
}: {
  error: string;
  onRetry?: () => void;
  onDismiss: () => void;
}) {
  const { t } = useLanguage();
  return (
    <div
      className="mx-auto mt-2 flex w-full items-start gap-2 rounded-xl bg-[var(--sb-danger-soft)] px-3.5 py-2.5 text-[12px] leading-[18px] text-[var(--sb-danger)]"
      role="alert"
      data-cycle-status="failed"
    >
      <div className="min-w-0 flex-1 whitespace-pre-wrap break-words">
        <div className="font-medium">{t("chat.cycle.failed")}</div>
        {error && <div>{localizeServerText(error, t)}</div>}
      </div>
      {onRetry && (
        <button
          type="button"
          onClick={onRetry}
          className="sb-btn shrink-0"
          data-variant="secondary"
          data-size="sm"
        >
          <RotateCcw className="h-3 w-3" />
          {t("chat.cycle.retry")}
        </button>
      )}
      <button
        type="button"
        onClick={onDismiss}
        className="flex h-6 w-6 shrink-0 items-center justify-center rounded transition hover:bg-[var(--sb-danger-soft)]"
        aria-label={t("chat.cycle.dismiss")}
        title={t("chat.cycle.dismiss")}
      >
        <X className="h-3.5 w-3.5" />
      </button>
    </div>
  );
}

function EmptyRoom({
  bot,
  team,
  onPick,
}: {
  bot?: Bot;
  team?: Team;
  onPick: (text: string) => void;
}) {
  const { t, language } = useLanguage();
  const title = team?.name ?? bot?.name ?? t("page.newConversation");
  const description =
    team?.description ?? bot?.description ?? t("page.defaultDesc");
  const members = team?.bots.map((entry) => entry.bot) ?? [];
  const suggestions = welcomeSuggestions(bot, team, language);
  return (
    <div className="animate-[sb-message-in_0.22s_ease] px-1 pb-4 pt-10 text-center">
      <div className="mx-auto flex justify-center">
        {team ? (
          <div className="flex -space-x-3">
            {members.slice(0, 4).map((member) => (
              <BotAvatar
                key={member.id}
                name={member.name}
                id={member.id}
                size={54}
                shape={member.avatar_shape}
                color={member.avatar_color}
                avatar={member.avatar}
                className="ring-4 ring-[var(--sb-bg)]"
              />
            ))}
          </div>
        ) : (
          <BotAvatar
            name={bot?.name ?? "Bot"}
            id={bot?.id ?? "unknown"}
            size={60}
            shape={bot?.avatar_shape}
            color={bot?.avatar_color}
            avatar={bot?.avatar}
          />
        )}
      </div>
      <h2 className="mt-4 text-[20px] font-semibold tracking-[-0.02em]">{title}</h2>
      <p className="mx-auto mt-1.5 max-w-md text-[13px] leading-[20px] text-[var(--sb-text-2)]">
        {description}
      </p>
      {team && members.length > 0 && (
        <div className="mt-3 flex flex-wrap items-center justify-center gap-x-3 gap-y-1 text-[12px] text-[var(--sb-text-3)]">
          {members.map((member) => (
            <span key={member.id} className="inline-flex items-center gap-1">
              <BotAvatar name={member.name} id={member.id} size={16} shape={member.avatar_shape} color={member.avatar_color} avatar={member.avatar} />
              {member.name}
            </span>
          ))}
        </div>
      )}
      <div className="mx-auto mt-7 flex max-w-[520px] flex-col gap-1">
        {suggestions.map((text) => (
          <button
            key={text}
            type="button"
            onClick={() => onPick(text)}
            className="group flex items-center gap-3 rounded-xl px-3 py-2.5 text-left transition hover:bg-[var(--sb-hover)]"
          >
            <ArrowUpRight className="h-4 w-4 shrink-0 text-[var(--sb-text-4)] transition group-hover:text-[var(--sb-text)]" />
            <span className="text-[13px] leading-[20px] text-[var(--sb-text-2)] group-hover:text-[var(--sb-text)]">
              {text}
            </span>
          </button>
        ))}
      </div>
    </div>
  );
}
