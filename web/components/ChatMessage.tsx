"use client";

import { useEffect, useMemo, useState } from "react";
import {
  Check,
  Copy,
  CornerDownRight,
  FileText,
  Loader2,
  MessagesSquare,
  Pencil,
  Pin,
  Reply,
  SmilePlus,
  Trash2,
  X,
} from "lucide-react";

import { BotAvatar } from "./BotAvatar";
import { Markdown } from "./Markdown";
import { Menu, type MenuItem } from "./ui/Menu";
import { useToast } from "./ui/Toast";
import { api } from "@/lib/api";
import { cn, formatTime } from "@/lib/format";
import { useLanguage } from "./LanguageProvider";
import {
  CheckpointDivider,
  GoalNudgeDivider,
  RoutineTriggerDivider,
  SubagentResultCard,
  isRoutineTrigger,
} from "./SystemNotices";
import { localizeServerText } from "@/lib/serverText";
import type { Attachment, Bot, Message, MessageReaction, Poll } from "@/lib/types";

export function ChatMessage({
  message,
  bot,
  bots = [],
  groupPosition,
  showSender = false,
  parentMessage,
  conversationId,
  interjection,
  onReply,
  onOpenAgentThread,
  onMessageUpdated,
  onMessageDeleted,
}: {
  message: Message;
  bot?: Bot;
  bots?: Bot[];
  groupPosition?: "first" | "middle" | "last" | "single";
  showSender?: boolean;
  parentMessage?: Message;
  conversationId?: string | null;
  interjection?: "pending" | "delivered" | "follow_up";
  onReply?: (message: Message) => void;
  onOpenAgentThread?: (threadID: string, conversationID: string) => void;
  onMessageUpdated?: (message: Message) => void;
  onMessageDeleted?: (messageID: string) => void;
}) {
  const { toast } = useToast();
  const { t } = useLanguage();
  const [menuAnchor, setMenuAnchor] = useState<{ rect: DOMRect } | { x: number; y: number } | null>(null);
  const [showMoreReactions, setShowMoreReactions] = useState(false);
  const [reactions, setReactions] = useState<MessageReaction[]>(message.reactions ?? []);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(message.content);
  const [savingEdit, setSavingEdit] = useState(false);
  const [pollBusy, setPollBusy] = useState<string | null>(null);
  const [xPostedURL, setXPostedURL] = useState<string | null>(null);

  useEffect(() => {
    setReactions(message.reactions ?? []);
  }, [message.reactions]);

  const toggleReaction = async (emoji: string) => {
    const current = reactions.find((item) => item.emoji === emoji);
    const action = current?.reacted ? "remove" : "add";
    const previous = reactions;
    setReactions((prev) => {
      const existing = prev.find((item) => item.emoji === emoji);
      if (existing) {
        return prev
          .map((item) =>
            item.emoji === emoji
              ? { ...item, reacted: !item.reacted, count: item.count + (item.reacted ? -1 : 1) }
              : item,
          )
          .filter((item) => item.count > 0);
      }
      return [...prev, { emoji, count: 1, reacted: true }];
    });
    try {
      const result = await api.toggleMessageReaction(message.id, emoji, action);
      setReactions(result.reactions);
    } catch {
      setReactions(previous);
      toast("表情回应失败", "danger");
    }
  };

  const copyRequestID = async () => {
    try {
      await navigator.clipboard.writeText(message.run_id ?? message.id);
      toast("请求 ID 已复制", "success");
    } catch {
      toast("复制失败", "danger");
    }
  };

  const saveEdit = async () => {
    const content = draft.trim();
    if (!content) return;
    if (content === message.content) {
      setEditing(false);
      return;
    }
    setSavingEdit(true);
    try {
      const result = await api.updateMessage(message.id, content);
      onMessageUpdated?.(result.message);
      setEditing(false);
      toast("消息已更新", "success");
    } catch (err) {
      toast(err instanceof Error ? err.message : "编辑消息失败", "danger");
    } finally {
      setSavingEdit(false);
    }
  };

  const removeMessage = async () => {
    try {
      await api.deleteMessage(message.id);
      onMessageDeleted?.(message.id);
      toast("消息已删除", "success");
    } catch (err) {
      toast(err instanceof Error ? err.message : "删除消息失败", "danger");
    }
  };

  const togglePin = async () => {
    try {
      const result = await api.setMessagePinned(message.id, !message.pinned_at);
      onMessageUpdated?.(result.message);
      toast(result.message.pinned_at ? "已置顶" : "已取消置顶", "success");
    } catch (err) {
      toast(err instanceof Error ? err.message : "置顶失败", "danger");
    }
  };

  const publishX = async () => {
    try {
      const replyTo =
        typeof message.metadata?.reply_to === "string" ? message.metadata.reply_to : undefined;
      const result = await api.publishXPost(message.content, replyTo);
      setXPostedURL(result.url);
      toast("已发布到 X", "success");
    } catch (err) {
      toast(err instanceof Error ? err.message : "发布到 X 失败", "danger");
    }
  };

  const votePoll = async (optionID: string, voted: boolean) => {
    setPollBusy(optionID);
    try {
      const result = await api.votePoll(message.id, optionID, voted ? "remove" : "add");
      onMessageUpdated?.(result.message);
    } catch (err) {
      toast(err instanceof Error ? err.message : "投票失败", "danger");
    } finally {
      setPollBusy(null);
    }
  };

  const peer = useMemo(() => {
    if (message.metadata?.kind !== "agent_thread") return undefined;
    const peerID = String(message.metadata.peer_bot_id ?? "");
    return bots.find((item) => item.id === peerID);
  }, [message.metadata, bots]);

  if (message.sender_type === "system") {
    if (message.metadata?.kind === "context_checkpoint") {
      return <CheckpointDivider message={message} conversationId={conversationId} />;
    }
    if (message.metadata?.kind === "subagent_result") {
      return <SubagentResultCard message={message} />;
    }
    if (message.metadata?.kind === "goal_nudge") {
      return <GoalNudgeDivider message={message} />;
    }
    if (message.metadata?.kind === "agent_thread") {
      const threadID = String(message.metadata.thread_id ?? "");
      const conversationID = String(message.metadata.conversation_id ?? "");
      const peerName = String(message.metadata.peer_bot_name ?? "队友");
      const direction = String(message.metadata.direction ?? "sent");
      // A received reply flagged for injection is handed to the bot that
      // started the thread, which then carries on by itself.
      const handedOff = direction === "received" && message.metadata.inject === "true";
      const handoffBot =
        bots.find((item) => item.id === String(message.metadata.for_bot_id ?? "")) ?? bot;
      return (
        <div className="flex flex-col items-center py-1" data-message-id={message.id}>
          <button
            type="button"
            onClick={() => threadID && onOpenAgentThread?.(threadID, conversationID)}
            className="sb-agent-card max-w-[520px] rounded-full px-3 hover:bg-[var(--sb-hover)]"
          >
            {peer ? (
              <BotAvatar
                name={peer.name}
                id={peer.id}
                size={18}
                shape={peer.avatar_shape}
                color={peer.avatar_color}
                avatar={peer.avatar}
              />
            ) : (
              <MessagesSquare className="h-3.5 w-3.5" />
            )}
            <span>
              {direction === "received" ? "来自" : "已发消息给"} {peerName}
            </span>
            <CornerDownRight className="h-3 w-3 opacity-50" />
          </button>
          {handedOff && (
            <div className="-mt-1 pb-1 text-center text-[11px] leading-[14px] text-[var(--sb-text-3)]">
              {t("chat.agentThread.handoff", { bot: handoffBot?.name ?? t("chat.currentBot") })}
            </div>
          )}
        </div>
      );
    }
    return (
      <div className="sb-system-line" data-message-id={message.id}>
        {message.content}
      </div>
    );
  }

  if (isRoutineTrigger(message)) {
    return <RoutineTriggerDivider message={message} />;
  }

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(message.content);
      toast("已复制", "success");
    } catch {
      toast("复制失败", "danger");
    }
  };

  const actions: MenuItem[] = [
    {
      key: "reply",
      label: t("chat.reply"),
      icon: <Reply className="h-3.5 w-3.5" />,
      onSelect: () => onReply?.(message),
    },
    {
      key: "copy",
      label: t("chat.copy"),
      icon: <Copy className="h-3.5 w-3.5" />,
      onSelect: () => void copy(),
    },
    {
      key: "pin",
      label: message.pinned_at ? t("chat.unpin") : t("chat.pin"),
      icon: <Pin className="h-3.5 w-3.5" />,
      onSelect: () => void togglePin(),
    },
    {
      key: "copy-id",
      label: t("chat.copyRequestId"),
      icon: <FileText className="h-3.5 w-3.5" />,
      onSelect: () => void copyRequestID(),
    },
  ];
  if (message.sender_type === "user") {
    actions.push(
      {
        key: "edit",
        label: t("chat.edit"),
        icon: <Pencil className="h-3.5 w-3.5" />,
        onSelect: () => {
          setDraft(message.content);
          setEditing(true);
        },
      },
      {
        key: "delete",
        label: t("chat.delete"),
        icon: <Trash2 className="h-3.5 w-3.5" />,
        onSelect: () => void removeMessage(),
      },
    );
  }

  const reactionHeader = (
    <div className="w-full">
      <div className="flex items-center gap-0.5">
        {["👍", "👎", "❤️", "😂", "🎉"].map((emoji) => (
          <button
            key={emoji}
            type="button"
            onClick={() => void toggleReaction(emoji)}
            className={`flex h-8 w-8 items-center justify-center rounded-md text-[17px] transition hover:bg-[var(--sb-hover)] ${
              reactions.some((item) => item.emoji === emoji && item.reacted)
                ? "bg-[var(--sb-hover)]"
                : ""
            }`}
            aria-label={`用 ${emoji} 回应`}
          >
            {emoji}
          </button>
        ))}
        <button
          type="button"
          onClick={() => setShowMoreReactions((prev) => !prev)}
          className="flex h-8 w-8 items-center justify-center rounded-md text-[var(--sb-text-2)] transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-text)]"
          aria-label="更多表情符号"
        >
          <SmilePlus className="h-4 w-4" />
        </button>
      </div>
      {showMoreReactions && (
        <div className="mt-1 flex flex-wrap gap-0.5">
          {["😮", "😢", "😡", "🚀", "👀", "🙏", "🔥", "✅"].map((emoji) => (
            <button
              key={emoji}
              type="button"
              onClick={() => void toggleReaction(emoji)}
              className={`flex h-8 w-8 items-center justify-center rounded-md text-[17px] transition hover:bg-[var(--sb-hover)] ${
                reactions.some((item) => item.emoji === emoji && item.reacted)
                  ? "bg-[var(--sb-hover)]"
                  : ""
              }`}
              aria-label={`用 ${emoji} 回应`}
            >
              {emoji}
            </button>
          ))}
        </div>
      )}
    </div>
  );

  if (message.sender_type === "user") {
    return (
      <div
        className="sb-message-row group"
        data-side="right"
        data-message-id={message.id}
        onContextMenu={(event) => {
          event.preventDefault();
          setMenuAnchor({ x: event.clientX, y: event.clientY });
        }}
      >
        <div className="relative sb-bubble-wrap">
          {parentMessage && (
            <div className="mb-1 flex items-center gap-1.5 rounded-lg bg-[var(--sb-hover)] px-2 py-1 text-[11px] text-[var(--sb-text-2)]">
              <CornerDownRight className="h-3 w-3 shrink-0" />
              <span className="truncate">{parentMessage.content}</span>
            </div>
          )}
          {editing ? (
            <div
              className="w-full rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-2 shadow-sm"
              style={{ minWidth: 280 }}
            >
              <textarea
                autoFocus
                value={draft}
                onChange={(event) => setDraft(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Escape") setEditing(false);
                  if (event.key === "Enter" && !event.shiftKey) {
                    event.preventDefault();
                    void saveEdit();
                  }
                }}
                className="field min-h-[72px] w-full resize-none border-0 bg-transparent p-1 text-[13px] outline-none"
                aria-label="编辑消息"
              />
              <div className="mt-1 flex items-center justify-end gap-1.5">
                <button
                  type="button"
                  onClick={() => setEditing(false)}
                  className="flex h-7 items-center gap-1 rounded-lg px-2 text-[11.5px] text-[var(--sb-text-2)] transition hover:bg-[var(--sb-hover)]"
                >
                  <X className="h-3 w-3" />
                  取消
                </button>
                <button
                  type="button"
                  disabled={savingEdit || !draft.trim()}
                  onClick={() => void saveEdit()}
                  className="flex h-7 items-center gap-1 rounded-lg bg-[var(--sb-text)] px-2 text-[11.5px] text-[var(--sb-pane)] transition disabled:opacity-50"
                >
                  {savingEdit ? <Loader2 className="h-3 w-3 animate-spin" /> : <Check className="h-3 w-3" />}
                  保存
                </button>
              </div>
            </div>
          ) : (
            <div className="sb-user-bubble" data-group={groupPosition ?? "single"}>
              {message.content}
              <MessageAttachments attachments={message.attachments} tone="user" />
            </div>
          )}
          {reactions.length > 0 && (
            <div className="mt-1 flex justify-end gap-1">
              {reactions.map((reaction) => (
                <button
                  key={reaction.emoji}
                  type="button"
                  onClick={() => void toggleReaction(reaction.emoji)}
                  className="sb-reaction"
                  aria-label={`${reaction.reacted ? "移除" : "添加"} ${reaction.emoji} 回应`}
                >
                  {reaction.emoji} <span>{reaction.count}</span>
                </button>
              ))}
            </div>
          )}
          {interjection && (
            <div
              className="mt-1 text-right text-[11px] leading-[14px] text-[var(--sb-text-3)]"
              role="status"
              data-interjection={interjection}
            >
              {interjection === "delivered"
                ? t("chat.interjection.delivered")
                : interjection === "follow_up"
                  ? t("chat.interjection.followUp")
                  : t("chat.interjection.pending")}
            </div>
          )}
          <div className="sb-message-time flex items-center justify-end gap-1 text-right">
            {message.pinned_at && <Pin className="h-3 w-3" aria-label={t("chat.pinned")} />}
            {message.edited_at && <span>{t("chat.edited")}</span>}
            {formatTime(message.created_at)}
          </div>
        </div>
        <Menu
          open={menuAnchor !== null}
          anchor={menuAnchor}
          items={actions}
          onClose={() => {
            setMenuAnchor(null);
            setShowMoreReactions(false);
          }}
          header={reactionHeader}
          minWidth={224}
        />
      </div>
    );
  }

  const warning = typeof message.metadata?.warning === "string" ? message.metadata.warning : "";

  return (
    <div
      className="sb-message-row group"
      data-side="left"
      data-message-id={message.id}
      onContextMenu={(event) => {
        event.preventDefault();
        setMenuAnchor({ x: event.clientX, y: event.clientY });
      }}
    >
      <div className={showSender ? "w-full min-w-0 pl-[30px]" : "w-full min-w-0"}>
        {showSender && (
          <div className="sb-sender-row">
            <BotAvatar
              name={bot?.name ?? "Bot"}
              id={message.sender_bot_id ?? message.id}
              size={20}
              shape={bot?.avatar_shape}
              color={bot?.avatar_color}
              avatar={bot?.avatar}
            />
            <span className="font-medium text-[var(--sb-text)]">{bot?.name ?? "Bot"}</span>
            <span>{formatTime(message.created_at)}</span>
          </div>
        )}
        <div className="relative sb-bubble-wrap">
          {parentMessage && (
            <div className="mb-1 flex items-center gap-1.5 rounded-lg bg-[var(--sb-hover)] px-2 py-1 text-[11px] text-[var(--sb-text-2)]">
              <CornerDownRight className="h-3 w-3 shrink-0" />
              <span className="truncate">{parentMessage.content}</span>
            </div>
          )}
          <div className="sb-bubble" data-group={groupPosition ?? "single"}>
            <Markdown content={message.content} />
            <MessageAttachments attachments={message.attachments} tone="bot" />
            {message.poll && (
              <PollCard poll={message.poll} busy={pollBusy} onVote={votePoll} />
            )}
            {message.metadata?.kind === "x_draft" && (
              <div className="mt-2.5 rounded-xl border border-[var(--sb-border)] bg-[var(--sb-bg)] p-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="text-[11px] text-[var(--sb-text-3)]">
                    X 草稿 · {Array.from(message.content).length}/280
                  </span>
                  {xPostedURL ? (
                    <a
                      href={xPostedURL}
                      target="_blank"
                      rel="noreferrer"
                      className="text-[11.5px] text-[var(--sb-success)] underline"
                    >
                      查看帖子
                    </a>
                  ) : (
                    <button
                      type="button"
                      onClick={() => void publishX()}
                      className="h-7 rounded-lg bg-[var(--sb-text)] px-2.5 text-[11.5px] text-[var(--sb-pane)]"
                    >
                      发布到 X
                    </button>
                  )}
                </div>
              </div>
            )}
          </div>
          {warning && (
            <div
              className="mt-1 text-[11px] leading-[14px] text-[var(--sb-text-3)]"
              data-message-warning
            >
              {localizeServerText(warning, t)}
            </div>
          )}
          {reactions.length > 0 && (
            <div className="mt-1 flex gap-1">
              {reactions.map((reaction) => (
                <button
                  key={reaction.emoji}
                  type="button"
                  onClick={() => void toggleReaction(reaction.emoji)}
                  className="sb-reaction"
                  aria-label={`${reaction.reacted ? "移除" : "添加"} ${reaction.emoji} 回应`}
                >
                  {reaction.emoji} <span>{reaction.count}</span>
                </button>
              ))}
            </div>
          )}
          <div className="sb-message-time flex items-center gap-1">
            {message.pinned_at && <Pin className="h-3 w-3" aria-label={t("chat.pinned")} />}
            {message.edited_at && <span>{t("chat.edited")}</span>}
            {formatTime(message.created_at)}
          </div>
        </div>
      </div>
      <Menu
        open={menuAnchor !== null}
        anchor={menuAnchor}
        items={actions}
        onClose={() => {
          setMenuAnchor(null);
          setShowMoreReactions(false);
        }}
        header={reactionHeader}
        minWidth={224}
      />
    </div>
  );
}


function PollCard({
  poll,
  busy,
  onVote,
}: {
  poll: Poll;
  busy: string | null;
  onVote: (optionID: string, voted: boolean) => void;
}) {
  const { t } = useLanguage();
  return (
    <div className="mt-2.5 rounded-xl border border-[var(--sb-border)] bg-[var(--sb-bg)] p-2.5">
      <div className="mb-2 text-[11px] text-[var(--sb-text-3)]">
        {poll.multiple ? t("poll.multiple") : t("poll.single")} · {poll.total_votes} {t("poll.votes")}
      </div>
      <div className="space-y-1.5">
        {poll.options.map((option) => {
          const percent = poll.total_votes > 0 ? Math.round((option.votes / poll.total_votes) * 100) : 0;
          return (
            <button
              key={option.id}
              type="button"
              disabled={busy !== null}
              onClick={() => onVote(option.id, option.voted)}
              className={cn(
                "relative flex w-full items-center gap-2 overflow-hidden rounded-lg border px-2.5 py-2 text-left text-[12.5px] transition",
                option.voted
                  ? "border-[var(--sb-success)]"
                  : "border-[var(--sb-border)] hover:border-[var(--sb-border-strong)]",
              )}
            >
              <span
                className="absolute inset-y-0 left-0 bg-[var(--sb-success)]/10 transition-all"
                style={{ width: `${percent}%` }}
              />
              <span
                className={cn(
                  "relative flex h-4 w-4 shrink-0 items-center justify-center rounded-full border",
                  option.voted
                    ? "border-[var(--sb-success)] bg-[var(--sb-success)] text-white"
                    : "border-[var(--sb-border-strong)]",
                )}
              >
                {option.voted && <Check className="h-2.5 w-2.5" />}
              </span>
              <span className="relative min-w-0 flex-1 truncate">{option.text}</span>
              <span className="relative shrink-0 text-[11px] text-[var(--sb-text-3)]">
                {option.votes} · {percent}%
              </span>
              {busy === option.id && <Loader2 className="relative h-3 w-3 shrink-0 animate-spin" />}
            </button>
          );
        })}
      </div>
    </div>
  );
}

function MessageAttachments({
  attachments,
  tone,
}: {
  attachments?: Attachment[];
  tone: "user" | "bot";
}) {
  if (!attachments || attachments.length === 0) return null;
  return (
    <div className="mt-1.5 flex flex-wrap gap-1.5">
      {attachments.map((attachment) =>
        attachment.mime_type.startsWith("audio/") ? (
          <audio
            key={attachment.id}
            controls
            preload="metadata"
            src={attachment.url}
            className="max-w-full"
          />
        ) : attachment.mime_type.startsWith("image/") ? (
          <a
            key={attachment.id}
            href={attachment.url}
            target="_blank"
            rel="noreferrer"
            className="block overflow-hidden rounded-lg"
          >
            <img
              src={attachment.url}
              alt={attachment.filename}
              className="max-h-60 max-w-full rounded-lg object-cover"
            />
          </a>
        ) : (
          <a
            key={attachment.id}
            href={attachment.url}
            target="_blank"
            rel="noreferrer"
            className={
              tone === "user"
                ? "flex items-center gap-1.5 rounded-lg bg-white/15 px-2 py-1.5 text-[12px] text-white/90"
                : "flex items-center gap-1.5 rounded-lg bg-[var(--sb-hover)] px-2 py-1.5 text-[12px] text-[var(--sb-text-2)]"
            }
          >
            <FileText className="h-3.5 w-3.5 shrink-0" />
            <span className="max-w-[180px] truncate">{attachment.filename}</span>
            <span className={tone === "user" ? "text-white/60" : "text-[var(--sb-text-4)]"}>
              {formatBytes(attachment.size_bytes)}
            </span>
          </a>
        ),
      )}
    </div>
  );
}

function formatBytes(value: number): string {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MB`;
}
