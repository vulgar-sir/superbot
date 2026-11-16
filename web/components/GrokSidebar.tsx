"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import {
  Bot as BotIcon,
  ArrowDown,
  ArrowUp,
  Bell,
  ArrowLeftRight,
  Check,
  CircleHelp,
  Copy,
  ChevronDown,
  ChevronRight,
  EyeOff,
  FolderInput,
  FolderPlus,
  Gauge,
  Loader2,
  LogOut,
  Mail,
  Pencil,
  Pin,
  PinOff,
  Plug,
  Plus,
  Search,
  Settings,
  Smartphone,
  Trash2,
  Upload,
  UserRound,
  Users,
} from "lucide-react";

import { api, ApiError } from "@/lib/api";
import { botProfilePayload } from "@/lib/bot";
import { BotAvatar } from "./BotAvatar";
import { Menu, useContextMenu, type MenuItem } from "./ui/Menu";
import { Modal } from "./ui/Modal";
import { useConfirm } from "./ui/Confirm";
import { useToast } from "./ui/Toast";
import { useLanguage } from "./LanguageProvider";
import { cn } from "@/lib/format";
import type { Account, Bot, SidebarData, SidebarSection, Team, User } from "@/lib/types";
import { copyText as writeClipboard } from "@/lib/clipboard";

type Props = {
  user: User;
  data: SidebarData | null;
  activeBotId?: string | null;
  activeTeamId?: string | null;
  primaryBotId?: string | null;
  collapsed?: boolean;
  onOpenBot: (bot: Bot) => void;
  onOpenInfo: (bot: Bot) => void;
  onOpenGroup: (team: Team) => void;
  onNewBot: () => void;
  onNewTeamBot: () => void;
  onNewGroup: () => void;
  onOpenSettings: (section?: string) => void;
  onOpenNotifications: () => void;
  onLogout: () => void;
  onToggleSection: (section: SidebarSection) => void;
  onCreateSection: (name: string) => Promise<void> | void;
  onOpenSearch: (query: string) => void;
  onImportTemplate: (template: Record<string, unknown>) => Promise<void> | void;
  onRefresh: () => Promise<void> | void;
  onOpenExtensions: () => void;
  onOpenHelp: () => void;
  onInstallApp: () => void;
};

export function GrokSidebar({
  user,
  data,
  activeBotId,
  activeTeamId,
  primaryBotId,
  collapsed,
  onOpenBot,
  onOpenInfo,
  onOpenGroup,
  onNewBot,
  onNewTeamBot,
  onNewGroup,
  onOpenSettings,
  onOpenNotifications,
  onLogout,
  onToggleSection,
  onCreateSection,
  onOpenSearch,
  onImportTemplate,
  onRefresh,
  onOpenExtensions,
  onOpenHelp,
  onInstallApp,
}: Props) {
  const { toast } = useToast();
  const { t } = useLanguage();
  const { confirm, confirmElement } = useConfirm();
  const [accountAnchor, setAccountAnchor] = useState<{ rect: DOMRect } | null>(null);
  const [unreadAnchor, setUnreadAnchor] = useState<{ rect: DOMRect } | null>(null);
  const [accountOpen, setAccountOpen] = useState(false);
  const [usagePercent, setUsagePercent] = useState<number | null>(null);
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [newAccountName, setNewAccountName] = useState("");
  const [accountBusy, setAccountBusy] = useState(false);
  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteLink, setInviteLink] = useState("");
  const [accountMembers, setAccountMembers] = useState<Array<{ id: string; email: string; display_name: string; role: string }>>([]);
  const [addingSection, setAddingSection] = useState(false);
  const [sectionName, setSectionName] = useState("");
  const [sectionSaving, setSectionSaving] = useState(false);
  const [editingSectionId, setEditingSectionId] = useState<string | null>(null);
  const [editingName, setEditingName] = useState("");
  const [draggingBotId, setDraggingBotId] = useState<string | null>(null);
  const [draggingGroupId, setDraggingGroupId] = useState<string | null>(null);
  const [draggingSectionId, setDraggingSectionId] = useState<string | null>(null);
  const [dropBotId, setDropBotId] = useState<string | null>(null);
  const [dropSectionId, setDropSectionId] = useState<string | null>(null);
  const importInputRef = useRef<HTMLInputElement>(null);
  const botContext = useContextMenu();
  const sectionContext = useContextMenu();
  const [contextBot, setContextBot] = useState<Bot | null>(null);
  const [contextSection, setContextSection] = useState<SidebarSection | null>(null);
  const [groupMenu, setGroupMenu] = useState<Team | null>(null);
  const [groupMenuAnchor, setGroupMenuAnchor] = useState<{ rect: DOMRect } | { x: number; y: number } | null>(null);
  const [emptyAnchor, setEmptyAnchor] = useState<{ x: number; y: number } | null>(null);
  const [renamingBotId, setRenamingBotId] = useState<string | null>(null);
  const [renameBotValue, setRenameBotValue] = useState("");
  const [renamingGroupId, setRenamingGroupId] = useState<string | null>(null);
  const [renameGroupValue, setRenameGroupValue] = useState("");

  const hiddenEntries = useMemo(() => {
    if (!data) return [] as Array<{ type: "bot" | "group"; id: string; name: string }>;
    const entries: Array<{ type: "bot" | "group"; id: string; name: string }> = [];
    for (const section of [data.unassigned, ...data.sections]) {
      for (const bot of section.bots) {
        if (bot.hidden) entries.push({ type: "bot", id: bot.id, name: bot.name });
      }
      for (const group of section.groups) {
        if (group.hidden) entries.push({ type: "group", id: group.id, name: group.name });
      }
    }
    return entries;
  }, [data]);

  const markUnreadBot = useCallback(
    async (bot: Bot) => {
      try {
        await api.markActivitiesUnread(bot.id);
        await onRefresh();
        toast("已标为未读", "success");
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "操作失败", "danger");
      }
    },
    [onRefresh, toast],
  );

  const markUnreadGroup = useCallback(
    async (group: Team) => {
      try {
        await api.setTeamUnread(group.id, true);
        await onRefresh();
        toast("已标为未读", "success");
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "操作失败", "danger");
      }
    },
    [onRefresh, toast],
  );

  const hideBot = useCallback(
    async (bot: Bot) => {
      try {
        await api.setBotHidden(bot.id, true);
        await onRefresh();
        toast(`${bot.name} 已从侧栏隐藏`, "success");
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "隐藏失败", "danger");
      }
    },
    [onRefresh, toast],
  );

  const hideGroup = useCallback(
    async (group: Team) => {
      try {
        await api.setTeamHidden(group.id, true);
        await onRefresh();
        toast(`${group.name} 已从侧栏隐藏`, "success");
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "隐藏失败", "danger");
      }
    },
    [onRefresh, toast],
  );

  const showHidden = useCallback(async () => {
    try {
      await Promise.all(
        hiddenEntries.map((entry) =>
          entry.type === "bot"
            ? api.setBotHidden(entry.id, false)
            : api.setTeamHidden(entry.id, false),
        ),
      );
      await onRefresh();
      toast("已显示隐藏的项", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "操作失败", "danger");
    }
  }, [hiddenEntries, onRefresh, toast]);

  const sections = useMemo(() => {
    if (!data) return [];
    const withoutPrimary = (section: SidebarSection): SidebarSection => ({
      ...section,
      bots: section.bots.filter((bot) => bot.id !== primaryBotId && !bot.hidden),
      groups: section.groups.filter((group) => !group.hidden),
    });
    const all = [...data.sections]
      .sort((a, b) => a.position - b.position)
      .map(withoutPrimary);
    const unassigned = withoutPrimary(data.unassigned);
    if (unassigned.bots.length > 0 || unassigned.groups.length > 0 || draggingBotId || draggingGroupId) {
      all.push({ ...unassigned, id: undefined, name: t("nav.ungrouped") });
    }
    return all;
  }, [data, primaryBotId, draggingBotId, draggingGroupId, t]);

  const primaryBot = useMemo(() => {
    if (!data || !primaryBotId) return null;
    for (const section of [data.unassigned, ...data.sections]) {
      const match = section.bots.find((bot) => bot.id === primaryBotId);
      if (match) return match;
    }
    return null;
  }, [data, primaryBotId]);

  const unreadTotal = useMemo(() => {
    if (!data) return 0;
    let total = 0;
    for (const section of [data.unassigned, ...data.sections]) {
      for (const bot of section.bots) {
        if (!bot.hidden && ((bot.unread_count ?? 0) > 0 || bot.marked_unread)) total += 1;
      }
      for (const group of section.groups) {
        if (group.hidden) continue;
        if (group.marked_unread || group.bots.some((entry) => (entry.bot.unread_count ?? 0) > 0)) {
          total += 1;
        }
      }
    }
    return total;
  }, [data]);

  const markAllRead = useCallback(async () => {
    try {
      await api.markActivitiesRead();
      await onRefresh();
      toast("已全部标记为已读", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "操作失败", "danger");
    }
  }, [onRefresh, toast]);

  const unreadItems = useMemo(() => {
    const result: MenuItem[] = [];
    if (!data) return result;
    for (const section of [data.unassigned, ...data.sections]) {
      for (const bot of section.bots) {
        if (bot.hidden || ((bot.unread_count ?? 0) <= 0 && !bot.marked_unread)) continue;
        result.push({
          key: `unread-bot-${bot.id}`,
          label: bot.name,
          description: bot.unread_count ? `${bot.unread_count} 条` : "未读",
          onSelect: () => {
            onOpenBot(bot);
            void api.markActivitiesRead(bot.id).then(onRefresh).catch(() => undefined);
          },
        });
      }
      for (const group of section.groups) {
        if (group.hidden) continue;
        const count = group.bots.reduce((sum, entry) => sum + (entry.bot.unread_count ?? 0), 0);
        if (!group.marked_unread && count === 0) continue;
        result.push({
          key: `unread-group-${group.id}`,
          label: group.name,
          description: count ? `${count} 条` : "未读",
          onSelect: () => onOpenGroup(group),
        });
      }
    }
    if (result.length > 0) {
      result.push({ key: "sep-all", separator: true });
      result.push({
        key: "mark-all",
        label: t("sidebar.markAllRead"),
        onSelect: () => void markAllRead(),
      });
    }
    return result;
  }, [data, markAllRead, onOpenBot, onOpenGroup, onRefresh]);

  const patchBot = useCallback(
    async (bot: Bot, patch: { pinned?: boolean; section_id?: string | null; position?: number }) => {
      try {
        await api.updateBotProfile(bot.id, { ...botProfilePayload(bot), ...patch });
        await onRefresh();
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "更新失败", "danger");
      }
    },
    [onRefresh, toast],
  );

  const moveBotToSection = useCallback(
    async (bot: Bot, sectionId: string | null) => {
      try {
        await api.updateBotProfile(bot.id, {
          ...botProfilePayload(bot),
          section_id: sectionId,
          position: 0,
        });
        await onRefresh();
        toast("已移动", "success");
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "移动失败", "danger");
      }
    },
    [onRefresh, toast],
  );

  const reorder = useCallback(
    async (dragged: Bot, targetSectionId: string | null, targetBot: Bot | null) => {
      if (!data) return;
      const allSections = [...data.sections, { ...data.unassigned, id: undefined }];
      const targetSection = allSections.find(
        (section) => (section.id ?? null) === targetSectionId,
      );
      if (!targetSection) return;
      const sourceSection = allSections.find((section) =>
        section.bots.some((bot) => bot.id === dragged.id),
      );
      const targetList = targetSection.bots.filter((bot) => bot.id !== dragged.id);
      let insertAt = targetBot
        ? targetList.findIndex((bot) => bot.id === targetBot.id)
        : targetList.length;
      if (insertAt < 0) insertAt = targetList.length;
      targetList.splice(insertAt, 0, dragged);
      try {
        const updates: Array<Promise<unknown>> = [];
        targetList.forEach((bot, index) => {
          updates.push(
            api.updateBotProfile(bot.id, {
              ...botProfilePayload(bot),
              section_id: targetSectionId,
              position: index,
            }),
          );
        });
        if (sourceSection && sourceSection.id !== targetSectionId) {
          sourceSection.bots
            .filter((bot) => bot.id !== dragged.id)
            .forEach((bot, index) => {
              updates.push(
                api.updateBotProfile(bot.id, {
                  ...botProfilePayload(bot),
                  section_id: sourceSection.id ?? null,
                  position: index,
                }),
              );
            });
        }
        await Promise.all(updates);
        await onRefresh();
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "排序失败", "danger");
      }
    },
    [data, onRefresh, toast],
  );

  const deleteBot = useCallback(
    async (bot: Bot) => {
      if (primaryBotId === bot.id) {
        await confirm({
          title: `主 Bot「${bot.name}」不能直接删除`,
          description:
            "请先在另一个 Bot 上右键 →「编辑 Bot 资料」→「设为主 Bot」，然后再删除它。",
          confirmLabel: "知道了",
        });
        return;
      }
      const ok = await confirm({
        title: `删除 ${bot.name}？`,
        description: "该 Bot 的私聊、资料库、例程与记忆会一并删除。",
        confirmLabel: "删除",
        danger: true,
      });
      if (!ok) return;
      try {
        await api.deleteBot(bot.id);
        await onRefresh();
        toast("已删除", "success");
      } catch (err) {
        if (err instanceof ApiError && err.code === "primary_bot_required") {
          toast(
            "它是主 Bot：先在另一个 Bot 的「编辑 Bot 资料」里设为主 Bot，再删除",
            "danger",
          );
          return;
        }
        toast(err instanceof ApiError ? err.message : "删除失败", "danger");
      }
    },
    [confirm, onRefresh, primaryBotId, toast],
  );

  const copyText = useCallback(
    async (text: string, label: string) => {
      try {
        await writeClipboard(text);
        toast(label, "success");
      } catch {
        toast("复制失败", "danger");
      }
    },
    [toast],
  );

  const copyBotConversationID = useCallback(
    async (bot: Bot) => {
      try {
        const list = await api.listConversations();
        const conversation = list.conversations.find((item) => item.direct_bot_id === bot.id);
        await copyText(conversation?.id ?? bot.id, "对话 ID 已复制");
      } catch {
        await copyText(bot.id, "对话 ID 已复制");
      }
    },
    [copyText],
  );

  const copyGroupConversationID = useCallback(
    async (group: Team) => {
      await copyText(group.room_conversation_id ?? group.id, "对话 ID 已复制");
    },
    [copyText],
  );

  const submitBotRename = useCallback(
    async (bot: Bot) => {
      const name = renameBotValue.trim();
      setRenamingBotId(null);
      if (!name || name === bot.name) return;
      try {
        await api.updateBotProfile(bot.id, { ...botProfilePayload(bot), name });
        await onRefresh();
        toast("已重命名", "success");
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "重命名失败", "danger");
      }
    },
    [onRefresh, renameBotValue, toast],
  );

  const submitGroupRename = useCallback(
    async (group: Team) => {
      const name = renameGroupValue.trim();
      setRenamingGroupId(null);
      if (!name || name === group.name) return;
      try {
        await api.updateTeam(group.id, {
          name,
          description: group.description,
          mode: group.mode,
          config: group.config,
        });
        await onRefresh();
        toast("已重命名", "success");
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "重命名失败", "danger");
      }
    },
    [onRefresh, renameGroupValue, toast],
  );

  const deleteGroup = useCallback(
    async (group: Team) => {
      const ok = await confirm({
        title: `删除群聊「${group.name}」？`,
        description: "该群聊的会话与房间记录也会被删除，此操作不可恢复。",
        confirmLabel: "删除",
        danger: true,
      });
      if (!ok) return;
      try {
        await api.deleteTeam(group.id);
        await onRefresh();
        toast("已删除", "success");
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "删除失败", "danger");
      }
    },
    [confirm, onRefresh, toast],
  );

  const moveSection = useCallback(
    async (section: SidebarSection, direction: "up" | "down") => {
      if (!section.id) return;
      const named = sections
        .filter((item) => item.id)
        .sort((a, b) => a.position - b.position);
      const index = named.findIndex((item) => item.id === section.id);
      const target = direction === "up" ? index - 1 : index + 1;
      if (index < 0 || target < 0 || target >= named.length) return;
      const other = named[target];
      try {
        await Promise.all([
          api.updateSection(section.id, { position: other.position }),
          api.updateSection(other.id!, { position: section.position }),
        ]);
        await onRefresh();
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "调整分组顺序失败", "danger");
      }
    },
    [onRefresh, sections, toast],
  );

  const botMenuItems = useCallback(
    (bot: Bot): MenuItem[] => {
      const moveTargets = [
        { id: null as string | null, name: t("nav.ungrouped") },
        ...sections.filter((section) => section.id).map((section) => ({
          id: section.id as string,
          name: section.name,
        })),
      ];
      return [
        {
          key: "pin",
          label: bot.pinned ? t("sidebar.unpin") : t("sidebar.pin"),
          icon: bot.pinned ? <PinOff className="h-3.5 w-3.5" /> : <Pin className="h-3.5 w-3.5" />,
          onSelect: () => void patchBot(bot, { pinned: !bot.pinned }),
        },
        {
          key: "move",
          label: t("sidebar.moveTo"),
          icon: <FolderInput className="h-3.5 w-3.5" />,
          children: moveTargets.map((target) => ({
            key: `move-${target.id ?? "none"}`,
            label: target.name,
            disabled: (bot.section_id ?? null) === target.id,
            icon:
              (bot.section_id ?? null) === target.id ? (
                <Check className="h-3.5 w-3.5" />
              ) : (
                <span className="block h-3.5 w-3.5" />
              ),
            onSelect: () => void moveBotToSection(bot, target.id),
          })),
        },
        {
          key: "unread",
          label: t("sidebar.markUnread"),
          icon: <Mail className="h-3.5 w-3.5" />,
          onSelect: () => void markUnreadBot(bot),
        },
        { key: "sep-unread", separator: true },
        {
          key: "rename",
          label: t("sidebar.renameBot"),
          icon: <Pencil className="h-3.5 w-3.5" />,
          onSelect: () => {
            setRenamingBotId(bot.id);
            setRenameBotValue(bot.name);
          },
        },
        {
          key: "copy-id",
          label: t("sidebar.copyConversationId"),
          icon: <Copy className="h-3.5 w-3.5" />,
          onSelect: () => void copyBotConversationID(bot),
        },
        { key: "sep-copy", separator: true },
        {
          key: "hide",
          label: t("sidebar.hide"),
          icon: <EyeOff className="h-3.5 w-3.5" />,
          onSelect: () => void hideBot(bot),
        },
        {
          key: "delete",
          label: t("common.delete"),
          icon: <Trash2 className="h-3.5 w-3.5" />,
          danger: true,
          onSelect: () => void deleteBot(bot),
        },
      ];
    },
    [
      copyBotConversationID,
      deleteBot,
      hideBot,
      markUnreadBot,
      moveBotToSection,
      onOpenInfo,
      patchBot,
      sections,
      t,
    ],
  );

  const groupMenuItems = useCallback(
    (group: Team): MenuItem[] => {
      const moveTargets = [
        { id: null as string | null, name: t("nav.ungrouped") },
        ...sections.filter((section) => section.id).map((section) => ({
          id: section.id as string,
          name: section.name,
        })),
      ];
      return [
        {
          key: "pin",
          label: group.pinned ? t("sidebar.unpin") : t("sidebar.pin"),
          icon: group.pinned ? <PinOff className="h-3.5 w-3.5" /> : <Pin className="h-3.5 w-3.5" />,
          onSelect: () => {
            void api
              .updateTeamPlacement(group.id, {
                section_id: group.section_id,
                position: group.position,
                pinned: !group.pinned,
              })
              .then(onRefresh)
              .then(() => toast(group.pinned ? "已取消置顶" : "已置顶", "success"))
              .catch((err) =>
                toast(err instanceof ApiError ? err.message : "更新群聊失败", "danger"),
              );
          },
        },
        {
          key: "move",
          label: t("sidebar.moveTo"),
          icon: <FolderInput className="h-3.5 w-3.5" />,
          children: moveTargets.map((target) => ({
            key: `move-${target.id ?? "none"}`,
            label: target.name,
            disabled: (group.section_id ?? null) === target.id,
            icon:
              (group.section_id ?? null) === target.id ? (
                <Check className="h-3.5 w-3.5" />
              ) : (
                <span className="block h-3.5 w-3.5" />
              ),
            onSelect: () => {
              void api
                .updateTeamPlacement(group.id, {
                  section_id: target.id,
                  position: 0,
                  pinned: group.pinned,
                })
                .then(onRefresh)
                .then(() => toast("群聊已移动", "success"))
                .catch((err) =>
                  toast(err instanceof ApiError ? err.message : "移动群聊失败", "danger"),
                );
            },
          })),
        },
        {
          key: "unread",
          label: t("sidebar.markUnread"),
          icon: <Mail className="h-3.5 w-3.5" />,
          onSelect: () => void markUnreadGroup(group),
        },
        { key: "sep-unread", separator: true },
        {
          key: "rename",
          label: t("sidebar.renameGroup"),
          icon: <Pencil className="h-3.5 w-3.5" />,
          onSelect: () => {
            setRenamingGroupId(group.id);
            setRenameGroupValue(group.name);
          },
        },
        {
          key: "copy-id",
          label: t("sidebar.copyConversationId"),
          icon: <Copy className="h-3.5 w-3.5" />,
          onSelect: () => void copyGroupConversationID(group),
        },
        { key: "sep-copy", separator: true },
        {
          key: "hide",
          label: t("sidebar.hide"),
          icon: <EyeOff className="h-3.5 w-3.5" />,
          onSelect: () => void hideGroup(group),
        },
        {
          key: "delete",
          label: t("common.delete"),
          icon: <Trash2 className="h-3.5 w-3.5" />,
          danger: true,
          onSelect: () => void deleteGroup(group),
        },
      ];
    },
    [
      copyGroupConversationID,
      deleteGroup,
      hideGroup,
      markUnreadGroup,
      onRefresh,
      sections,
      toast,
      t,
    ],
  );

  const renameSection = useCallback(
    async (section: SidebarSection) => {
      if (!section.id || !editingName.trim()) return;
      try {
        await api.updateSection(section.id, { name: editingName.trim() });
        setEditingSectionId(null);
        await onRefresh();
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "重命名失败", "danger");
      }
    },
    [editingName, onRefresh, toast],
  );

  const deleteSection = useCallback(
    async (section: SidebarSection) => {
      if (!section.id) return;
      const ok = await confirm({
        title: `删除分组「${section.name}」？`,
        description: "分组内的 Bot 会移到「未分组」，不会被删除。",
        confirmLabel: "删除分组",
        danger: true,
      });
      if (!ok) return;
      try {
        await api.deleteSection(section.id);
        await onRefresh();
        toast("分组已删除", "success");
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "删除分组失败", "danger");
      }
    },
    [confirm, onRefresh, toast],
  );

  const sectionMenuItems = useCallback(
    (section: SidebarSection): MenuItem[] => {
      if (!section.id) return [];
      const named = sections
        .filter((item) => item.id)
        .sort((a, b) => a.position - b.position);
      const index = named.findIndex((item) => item.id === section.id);
      return [
        {
          key: "rename",
          label: t("sidebar.rename"),
          icon: <Pencil className="h-3.5 w-3.5" />,
          onSelect: () => {
            setEditingSectionId(section.id ?? null);
            setEditingName(section.name);
          },
        },
        {
          key: "up",
          label: t("sidebar.moveUp"),
          disabled: index <= 0,
          icon: <ArrowUp className="h-3.5 w-3.5" />,
          onSelect: () => void moveSection(section, "up"),
        },
        {
          key: "down",
          label: t("sidebar.moveDown"),
          disabled: index < 0 || index >= named.length - 1,
          icon: <ArrowDown className="h-3.5 w-3.5" />,
          onSelect: () => void moveSection(section, "down"),
        },
        { key: "sep", separator: true },
        {
          key: "delete",
          label: t("common.delete"),
          icon: <Trash2 className="h-3.5 w-3.5" />,
          danger: true,
          onSelect: () => void deleteSection(section),
        },
      ];
    },
    [deleteSection, moveSection, sections],
  );

  useEffect(() => {
    let cancelled = false;
    api.getUsage()
      .then((usage) => {
        if (cancelled) return;
        const budget = usage.weekly_budget_micros;
        setUsagePercent(budget > 0 ? Math.min(100, Math.round((usage.weekly_spent_micros / budget) * 100)) : null);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, []);

  const accountMenuItems: MenuItem[] = [
    {
      key: "usage",
      label: t("sidebar.selfHostedPlan"),
      description: usagePercent === null ? t("sidebar.budgetUnset") : t("sidebar.budgetUsed", { n: usagePercent }),
      icon: <Gauge className="h-4 w-4" />,
      onSelect: () => onOpenSettings("usage"),
    },
    {
      key: "install",
      label: t("sidebar.mobileApp"),
      icon: <Smartphone className="h-4 w-4" />,
      onSelect: onInstallApp,
    },
    {
      key: "help",
      label: t("sidebar.support"),
      icon: <CircleHelp className="h-4 w-4" />,
      children: [
        {
          key: "shortcuts",
          label: t("sidebar.shortcuts"),
          onSelect: onOpenHelp,
        },
        {
          key: "notifications",
          label: t("sidebar.notifications"),
          onSelect: onOpenNotifications,
        },
      ],
    },
    {
      key: "settings",
      label: t("nav.settings"),
      icon: <Settings className="h-4 w-4" />,
      shortcut: "⌘,",
      onSelect: () => onOpenSettings(),
    },
    { key: "sep1", separator: true },
    {
      key: "switch",
      label: t("sidebar.switchAccount"),
      icon: <ArrowLeftRight className="h-4 w-4" />,
      onSelect: () => {
        setAccountOpen(true);
        setInviteLink("");
        setInviteEmail("");
        void api
          .listAccounts()
          .then(async (result) => {
            setAccounts(result.accounts);
            const active = result.accounts.find((account) => account.active);
            if (active) {
              const members = await api.listAccountMembers(active.id).catch(() => ({ members: [] }));
              setAccountMembers(members.members);
            }
          })
          .catch(() => undefined);
      },
    },
    {
      key: "logout",
      label: t("sidebar.logout"),
      icon: <LogOut className="h-4 w-4" />,
      onSelect: onLogout,
    },
  ];

  const emptyMenuItems: MenuItem[] = [
    {
      key: "new-bot",
      label: t("sidebar.newBot"),
      icon: <Plus className="h-3.5 w-3.5" />,
      onSelect: onNewBot,
    },
    {
      key: "new-team-bot",
      label: t("sidebar.newTeamBot"),
      icon: <BotIcon className="h-3.5 w-3.5" />,
      onSelect: onNewTeamBot,
    },
    {
      key: "new-group",
      label: t("sidebar.newGroup"),
      icon: <Users className="h-3.5 w-3.5" />,
      onSelect: onNewGroup,
    },
    {
      key: "new-section",
      label: t("sidebar.newSection"),
      icon: <FolderPlus className="h-3.5 w-3.5" />,
      onSelect: () => setAddingSection(true),
    },
    { key: "sep", separator: true },
    {
      key: "import",
      label: t("sidebar.importTemplate"),
      icon: <Upload className="h-3.5 w-3.5" />,
      onSelect: () => importInputRef.current?.click(),
    },
    ...(hiddenEntries.length > 0
      ? [
          { key: "sep-hidden", separator: true },
          {
            key: "show-hidden",
            label: t("sidebar.showHidden", { n: hiddenEntries.length }),
            icon: <EyeOff className="h-3.5 w-3.5" />,
            onSelect: showHidden,
          },
        ]
      : []),
  ];


  return (
    <>
      <aside className="sb-sidebar" data-collapsed={collapsed ? "true" : undefined} aria-label="Bot">
        <div className="sb-sidebar-top">
          <button
            type="button"
            className="sb-icon-btn"
            aria-label={t("sidebar.search")}
            title={t("sidebar.searchShortcut")}
            onClick={() => onOpenSearch("")}
          >
            <Search className="h-[17px] w-[17px]" />
          </button>
          <button
            type="button"
            className="sb-icon-btn"
            aria-label={t("sidebar.new")}
            title={t("sidebar.newBot")}
            onClick={onNewBot}
          >
            <Plus className="h-[18px] w-[18px]" />
          </button>
        </div>

        <div
          className="sb-sidebar-scroll no-scrollbar"
          onContextMenu={(event) => {
            event.preventDefault();
            setEmptyAnchor({ x: event.clientX, y: event.clientY });
          }}
        >
          {!data && (
            <div className="mx-1 mt-4 space-y-2">
              <div className="sb-skeleton mx-auto h-[88px] w-[95px]" />
              {[0, 1, 2, 3].map((item) => (
                <div key={item} className="flex items-center gap-2.5 px-2 py-2">
                  <div className="sb-skeleton h-9 w-9 rounded-[10px]" />
                  <div className="flex-1 space-y-1.5">
                    <div className="sb-skeleton h-3 w-2/3" />
                    <div className="sb-skeleton h-2.5 w-5/6" />
                  </div>
                </div>
              ))}
            </div>
          )}

          {data && (
            <>
              {primaryBot ? (
                <button
                  type="button"
                  className="sb-primary-card"
                  data-active={activeBotId === primaryBot.id ? "true" : undefined}
                  onClick={() => onOpenBot(primaryBot)}
                  onContextMenu={(event) => {
                    setContextBot(primaryBot);
                    botContext.openAt(event);
                  }}
                  title={`${primaryBot.name} · ${t("sidebar.primaryBot")}`}
                >
                  <div className="relative">
                    <BotAvatar
                      name={primaryBot.name}
                      id={primaryBot.id}
                      size={80}
                      shape={primaryBot.avatar_shape}
                      color={primaryBot.avatar_color}
                      avatar={primaryBot.avatar}
                    />
                    <span className="absolute -bottom-0.5 -right-0.5 flex h-5 w-5 items-center justify-center rounded-full border-2 border-[var(--sb-sidebar)] bg-[#f5a623] text-[10px] leading-none text-white shadow-sm">
                      ★
                    </span>
                  </div>
                  <span className="w-full truncate text-center text-[15px] font-medium leading-[18px]">
                    {primaryBot.name}
                  </span>
                </button>
              ) : (
                <button
                  type="button"
                  className="sb-primary-card"
                  onClick={onNewBot}
                  title={t("sidebar.createPrimary")}
                >
                  <span className="flex h-14 w-14 items-center justify-center rounded-full border border-dashed border-[var(--sb-border-strong)] text-[var(--sb-text-3)]">
                    <Plus className="h-5 w-5" />
                  </span>
                  <span className="text-[12px] leading-[16px] text-[var(--sb-text-2)]">{t("sidebar.createPrimary")}</span>
                </button>
              )}

              <div className="mt-2 space-y-0.5 pb-2">
                {sections.map((section) => {
                  const isCollapsed = section.collapsed;
                  return (
                    <div
                      key={section.id ?? section.name}
                      onDragOver={(event) => {
                        if (draggingBotId || draggingGroupId) {
                          event.preventDefault();
                          setDropSectionId(section.id ?? null);
                        }
                      }}
                      onDragLeave={() => setDropSectionId(null)}
                      onDrop={(event) => {
                        if (!data) return;
                        if (draggingBotId) {
                          event.preventDefault();
                          const dragged = [
                            ...data.unassigned.bots,
                            ...data.sections.flatMap((s) => s.bots),
                          ].find((bot) => bot.id === draggingBotId);
                          setDropSectionId(null);
                          setDraggingBotId(null);
                          if (dragged) void reorder(dragged, section.id ?? null, null);
                          return;
                        }
                        if (draggingGroupId) {
                          event.preventDefault();
                          const dragged = [
                            ...data.unassigned.groups,
                            ...data.sections.flatMap((s) => s.groups),
                          ].find((group) => group.id === draggingGroupId);
                          setDropSectionId(null);
                          setDraggingGroupId(null);
                          if (!dragged) return;
                          void api
                            .updateTeamPlacement(dragged.id, {
                              section_id: section.id ?? null,
                              position: 0,
                              pinned: dragged.pinned,
                            })
                            .then(onRefresh)
                            .then(() => toast("群聊已移动", "success"))
                            .catch((err) =>
                              toast(err instanceof ApiError ? err.message : "移动群聊失败", "danger"),
                            );
                        }
                      }}
                      className={cn(
                        "rounded-lg transition-colors",
                        dropSectionId === (section.id ?? null) && draggingBotId && "bg-[var(--sb-accent-soft)]",
                      )}
                    >
                      <div
                        draggable={Boolean(section.id)}
                        onDragStart={(event) => {
                          if (!section.id) return;
                          event.dataTransfer.effectAllowed = "move";
                          setDraggingSectionId(section.id);
                        }}
                        onDragEnd={() => setDraggingSectionId(null)}
                        onDragOver={(event) => {
                          if (draggingSectionId && section.id && draggingSectionId !== section.id) {
                            event.preventDefault();
                          }
                        }}
                        onDrop={(event) => {
                          if (!draggingSectionId || !section.id || draggingSectionId === section.id) return;
                          event.preventDefault();
                          event.stopPropagation();
                          const all = sections.filter((item) => item.id);
                          const fromIndex = all.findIndex((item) => item.id === draggingSectionId);
                          const toIndex = all.findIndex((item) => item.id === section.id);
                          if (fromIndex < 0 || toIndex < 0) return;
                          const ordered = [...all];
                          const [moved] = ordered.splice(fromIndex, 1);
                          ordered.splice(toIndex, 0, moved);
                          setDraggingSectionId(null);
                          void Promise.all(
                            ordered.map((item, index) =>
                              api.updateSection(item.id!, { position: index }),
                            ),
                          ).then(onRefresh);
                        }}
                        className="group/section"
                      >
                        {editingSectionId === section.id && section.id ? (
                          <form
                            className="flex items-center gap-1 px-1 py-1"
                            onSubmit={(event) => {
                              event.preventDefault();
                              void renameSection(section);
                            }}
                          >
                            <input
                              autoFocus
                              value={editingName}
                              onChange={(event) => setEditingName(event.target.value)}
                              onKeyDown={(event) => {
                                if (event.key === "Escape") setEditingSectionId(null);
                              }}
                              className="h-7 min-w-0 flex-1 rounded-md border border-[var(--sb-border)] bg-[var(--sb-pane)] px-2 text-[13px] outline-none focus:border-[var(--sb-accent)]"
                            />
                            <button type="submit" className="sb-icon-btn" data-size="sm" data-variant="plain" aria-label={t("sidebar.save")}>
                              <Check className="h-3.5 w-3.5" />
                            </button>
                          </form>
                        ) : (
                          <button
                            type="button"
                            className="sb-section-header"
                            onClick={() => section.id && onToggleSection(section)}
                            onContextMenu={(event) => {
                              if (!section.id) return;
                              setContextSection(section);
                              sectionContext.openAt(event);
                            }}
                          >
                            {isCollapsed ? (
                              <ChevronRight className="h-3.5 w-3.5 shrink-0 text-[var(--sb-text-3)] opacity-0 transition group-hover/section:opacity-100" />
                            ) : (
                              <ChevronDown className="h-3.5 w-3.5 shrink-0 text-[var(--sb-text-3)] opacity-0 transition group-hover/section:opacity-100" />
                            )}
                            <span className="truncate font-medium">{section.name}</span>
                          </button>
                        )}
                      </div>

                      <div
                        style={{
                          display: "grid",
                          gridTemplateRows: isCollapsed ? "0fr" : "1fr",
                          transition: "grid-template-rows 0.2s cubic-bezier(0.22,1,0.36,1)",
                        }}
                      >
                        <div className="overflow-hidden">
                          <div className="space-y-0.5 pt-0.5">
                            {section.groups.map((group) => (
                              <GroupRow
                                key={group.id}
                                group={group}
                                active={activeTeamId === group.id}
                                dragging={draggingGroupId === group.id}
                                unread={
                                  group.marked_unread ||
                                  group.bots.some((entry) => (entry.bot.unread_count ?? 0) > 0)
                                }
                                renaming={renamingGroupId === group.id}
                                renameValue={renameGroupValue}
                                onDragStart={(event) => {
                                  event.dataTransfer.effectAllowed = "move";
                                  setDraggingGroupId(group.id);
                                }}
                                onDragEnd={() => setDraggingGroupId(null)}
                                onClick={() => onOpenGroup(group)}
                                onContextMenu={(event) => {
                                  event.preventDefault();
                                  event.stopPropagation();
                                  setGroupMenu(group);
                                  setGroupMenuAnchor({ x: event.clientX, y: event.clientY });
                                }}
                                onRenameChange={setRenameGroupValue}
                                onRenameSubmit={() => void submitGroupRename(group)}
                                onRenameCancel={() => setRenamingGroupId(null)}
                              />
                            ))}
                            {section.bots.map((bot) => (
                              <div
                                key={bot.id}
                                draggable
                                onDragStart={(event) => {
                                  event.dataTransfer.effectAllowed = "move";
                                  event.dataTransfer.setData("text/plain", bot.id);
                                  setDraggingBotId(bot.id);
                                }}
                                onDragEnd={() => {
                                  setDraggingBotId(null);
                                  setDropBotId(null);
                                  setDropSectionId(null);
                                }}
                                onDragOver={(event) => {
                                  if (!draggingBotId || draggingBotId === bot.id) return;
                                  event.preventDefault();
                                  event.stopPropagation();
                                  setDropBotId(bot.id);
                                }}
                                onDragLeave={() => setDropBotId((prev) => (prev === bot.id ? null : prev))}
                                onDrop={(event) => {
                                  if (!draggingBotId || !data) return;
                                  event.preventDefault();
                                  event.stopPropagation();
                                  const dragged = [
                                    ...data.unassigned.bots,
                                    ...data.sections.flatMap((s) => s.bots),
                                  ].find((item) => item.id === draggingBotId);
                                  setDropBotId(null);
                                  setDraggingBotId(null);
                                  setDropSectionId(null);
                                  if (dragged) void reorder(dragged, section.id ?? null, bot);
                                }}
                                style={{
                                  opacity: draggingBotId === bot.id ? 0.35 : 1,
                                  transition: "opacity .12s ease",
                                }}
                              >
                                <div
                                  className={cn(
                                    "h-0.5 rounded-full transition-colors",
                                    dropBotId === bot.id ? "bg-[var(--sb-accent)]" : "bg-transparent",
                                  )}
                                />
                                <BotRow
                                  bot={bot}
                                  active={activeBotId === bot.id}
                                  renaming={renamingBotId === bot.id}
                                  renameValue={renameBotValue}
                                  onClick={() => onOpenBot(bot)}
                                  onContextMenu={(event) => {
                                    setContextBot(bot);
                                    botContext.openAt(event);
                                  }}
                                  onRenameChange={setRenameBotValue}
                                  onRenameSubmit={() => void submitBotRename(bot)}
                                  onRenameCancel={() => setRenamingBotId(null)}
                                />
                              </div>
                            ))}
                            {section.bots.length === 0 && section.groups.length === 0 && (
                              <div className="px-2 py-2 text-[12px] text-[var(--sb-text-4)]">
                                {t("sidebar.dropBot")}
                              </div>
                            )}
                          </div>
                        </div>
                      </div>
                    </div>
                  );
                })}

                {addingSection ? (
                  <form
                    className="mx-1 mt-1 flex items-center gap-1.5"
                    onSubmit={async (event) => {
                      event.preventDefault();
                      const name = sectionName.trim();
                      if (!name || sectionSaving) return;
                      setSectionSaving(true);
                      try {
                        await onCreateSection(name);
                        setSectionName("");
                        setAddingSection(false);
                      } finally {
                        setSectionSaving(false);
                      }
                    }}
                  >
                    <input
                      autoFocus
                      value={sectionName}
                      onChange={(event) => setSectionName(event.target.value)}
                      onKeyDown={(event) => {
                        if (event.key === "Escape") {
                          setAddingSection(false);
                          setSectionName("");
                        }
                      }}
                      placeholder={t("sidebar.sectionName")}
                      className="h-8 min-w-0 flex-1 rounded-md border border-[var(--sb-border)] bg-[var(--sb-pane)] px-2 text-[12px] outline-none focus:border-[var(--sb-accent)]"
                    />
                    <button
                      type="submit"
                      disabled={sectionSaving || !sectionName.trim()}
                      className="sb-icon-btn"
                      data-size="sm"
                      aria-label={t("sidebar.createSection")}
                    >
                      {sectionSaving ? <Loader2 className="sb-spin h-3.5 w-3.5" /> : <Check className="h-3.5 w-3.5" />}
                    </button>
                  </form>
                ) : null}
              </div>
            </>
          )}
        </div>

        <div className="relative px-3 pb-2">
          {unreadTotal > 0 && (
            <div className="pointer-events-none absolute -top-10 left-0 right-0 flex justify-center">
              <button
                type="button"
                onClick={(event) =>
                  setUnreadAnchor({
                    rect: (event.currentTarget as HTMLElement).getBoundingClientRect(),
                  })
                }
                className="sb-unread-pill pointer-events-auto"
              >
                <ArrowDown className="h-3.5 w-3.5" />
                {t("sidebar.moreUnread")}
              </button>
            </div>
          )}
        </div>

        <div className="sb-sidebar-footer">
          <button
            type="button"
            onClick={(event) =>
              setAccountAnchor({ rect: (event.currentTarget as HTMLElement).getBoundingClientRect() })
            }
            className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-[var(--sb-hover)] text-[var(--sb-text-2)] transition hover:bg-[var(--sb-active)]"
            aria-label={t("sidebar.accountMenu")}
            title={user.display_name || user.email}
          >
            <UserRound className="h-[18px] w-[18px]" />
          </button>
          <button type="button" onClick={onOpenExtensions} className="sb-connect-pill">
            <Plug className="h-3.5 w-3.5" />
            {t("nav.connectors")}
            <span className="flex items-center gap-0.5 opacity-70">
              <span className="h-3 w-3 rounded-[3px] bg-[#10ab61]" />
              <span className="h-3 w-3 rounded-[3px] bg-[#1680f3]" />
              <span className="h-3 w-3 rounded-[3px] bg-[#ee620d]" />
            </span>
          </button>
        </div>

        <input
          ref={importInputRef}
          type="file"
          accept="application/json,.json"
          className="hidden"
          onChange={async (event) => {
            const file = event.target.files?.[0];
            event.target.value = "";
            if (!file) return;
            try {
              const parsed = JSON.parse(await file.text()) as Record<string, unknown>;
              await onImportTemplate(parsed);
            } catch {
              toast("模板解析失败", "danger");
            } finally {
            }
          }}
        />
      </aside>

      <Menu
        open={unreadAnchor !== null}
        anchor={unreadAnchor}
        items={unreadItems}
        onClose={() => setUnreadAnchor(null)}
        align="left"
        minWidth={240}
      />

      <Modal
        open={accountOpen}
        onClose={() => setAccountOpen(false)}
        title={t("sidebar.switchAccount")}
        subtitle={t("sidebar.switchAccountHint")}
        size="sm"
        footer={
          <button type="button" className="sb-btn" data-variant="secondary" onClick={() => setAccountOpen(false)}>
            关闭
          </button>
        }
      >
        <div className="space-y-1.5">
          {accounts.map((account) => (
            <button
              key={account.id}
              type="button"
              disabled={accountBusy}
              onClick={async () => {
                if (account.active) {
                  setAccountOpen(false);
                  return;
                }
                setAccountBusy(true);
                try {
                  await api.switchAccount(account.id);
                  window.location.reload();
                } catch (err) {
                  toast(err instanceof ApiError ? err.message : "切换账户失败", "danger");
                  setAccountBusy(false);
                }
              }}
              className="flex w-full items-center gap-3 rounded-xl border border-[var(--sb-border)] px-3 py-2.5 text-left transition hover:bg-[var(--sb-hover)]"
            >
              <span className="flex h-8 w-8 items-center justify-center rounded-full bg-[var(--sb-hover)] text-[12px]">
                {account.name.slice(0, 1).toUpperCase()}
              </span>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-[13px]">{account.name}</span>
                <span className="block text-[11px] text-[var(--sb-text-3)]">{account.role}</span>
              </span>
              {account.active && <Check className="h-4 w-4 text-[var(--sb-success)]" />}
            </button>
          ))}
        </div>
        <div className="mt-4 border-t border-[var(--sb-border)] pt-3">
          <div className="mb-2 flex items-center justify-between text-[12px] text-[var(--sb-text-2)]">
            <span>{t("sidebar.members")}</span>
            <span>{accountMembers.length}</span>
          </div>
          <div className="space-y-1">
            {accountMembers.map((member) => (
              <div key={member.id} className="flex items-center gap-2 rounded-lg bg-[var(--sb-hover)] px-2.5 py-1.5 text-[12px]">
                <span className="min-w-0 flex-1 truncate">{member.display_name || member.email}</span>
                <span className="text-[10px] text-[var(--sb-text-3)]">{member.role}</span>
              </div>
            ))}
          </div>
          <div className="mt-2 flex items-center gap-2">
            <input
              className="field h-9 flex-1"
              value={inviteEmail}
              onChange={(event) => setInviteEmail(event.target.value)}
              placeholder={t("sidebar.inviteEmail")}
            />
            <button
              type="button"
              disabled={accountBusy || !inviteEmail.trim()}
              onClick={async () => {
                const active = accounts.find((account) => account.active);
                if (!active) return;
                setAccountBusy(true);
                try {
                  const result = await api.createAccountInvitation(active.id, inviteEmail.trim());
                  const link = `${window.location.origin}${result.url}`;
                  setInviteLink(link);
                  await writeClipboard(link).catch(() => undefined);
                  toast("邀请链接已复制", "success");
                  setInviteEmail("");
                } catch (err) {
                  toast(err instanceof ApiError ? err.message : "创建邀请失败", "danger");
                } finally {
                  setAccountBusy(false);
                }
              }}
              className="sb-btn"
              data-variant="secondary"
              data-size="sm"
            >
              {t("sidebar.invite")}
            </button>
          </div>
          {inviteLink && (
            <input className="field mt-2 w-full text-[11px]" readOnly value={inviteLink} onFocus={(event) => event.currentTarget.select()} />
          )}
        </div>

        <div className="mt-4 border-t border-[var(--sb-border)] pt-3">
          <div className="mb-2 text-[12px] text-[var(--sb-text-2)]">{t("sidebar.createAccount")}</div>
          <div className="flex items-center gap-2">
            <input
              className="field h-9 flex-1"
              value={newAccountName}
              onChange={(event) => setNewAccountName(event.target.value)}
              placeholder={t("sidebar.accountName")}
            />
            <button
              type="button"
              disabled={accountBusy || !newAccountName.trim()}
              onClick={async () => {
                setAccountBusy(true);
                try {
                  const created = await api.createAccount(newAccountName.trim());
                  await api.switchAccount(created.account.id);
                  window.location.reload();
                } catch (err) {
                  toast(err instanceof ApiError ? err.message : "创建账户失败", "danger");
                  setAccountBusy(false);
                }
              }}
              className="sb-btn"
              data-variant="primary"
              data-size="sm"
            >
              {t("sidebar.createAndSwitch")}
            </button>
          </div>
        </div>
      </Modal>

      <Menu
        open={accountAnchor !== null}
        anchor={accountAnchor}
        items={accountMenuItems}
        onClose={() => setAccountAnchor(null)}
        align="left"
        minWidth={253}
      />

      <Menu
        open={botContext.isOpen}
        anchor={botContext.anchor}
        items={contextBot ? botMenuItems(contextBot) : []}
        onClose={() => {
          botContext.close();
          setContextBot(null);
        }}
        minWidth={240}
      />

      <Menu
        open={sectionContext.isOpen}
        anchor={sectionContext.anchor}
        items={contextSection ? sectionMenuItems(contextSection) : []}
        onClose={() => {
          sectionContext.close();
          setContextSection(null);
        }}
        minWidth={180}
      />

      <Menu
        open={groupMenuAnchor !== null}
        anchor={groupMenuAnchor}
        items={groupMenu ? groupMenuItems(groupMenu) : []}
        onClose={() => {
          setGroupMenuAnchor(null);
          setGroupMenu(null);
        }}
        minWidth={200}
      />

      <Menu
        open={emptyAnchor !== null}
        anchor={emptyAnchor}
        items={emptyMenuItems}
        onClose={() => setEmptyAnchor(null)}
        minWidth={200}
      />

      {confirmElement}
    </>
  );
}

function BotRow({
  bot,
  active,
  renaming,
  renameValue,
  onClick,
  onContextMenu,
  onRenameChange,
  onRenameSubmit,
  onRenameCancel,
}: {
  bot: Bot;
  active: boolean;
  renaming: boolean;
  renameValue: string;
  onClick: () => void;
  onContextMenu: (event: React.MouseEvent) => void;
  onRenameChange: (value: string) => void;
  onRenameSubmit: () => void;
  onRenameCancel: () => void;
}) {
  const { t } = useLanguage();
  const submittedRef = useRef(false);
  useEffect(() => {
    submittedRef.current = false;
  }, [renaming]);
  const subtitle =
    bot.latest_activity?.summary || bot.title || bot.description || bot.model || t("sidebar.aiColleague");
  const unread = ((bot.unread_count ?? 0) > 0 || bot.marked_unread) ? 1 : 0;
  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onClick}
      onContextMenu={onContextMenu}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onClick();
        }
      }}
      className="sb-row group/row"
      data-active={active ? "true" : undefined}
    >
      <BotAvatar
        name={bot.name}
        id={bot.id}
        size={36}
        shape={bot.avatar_shape}
        color={bot.avatar_color}
        avatar={bot.avatar}
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          {renaming ? (
            <input
              autoFocus
              value={renameValue}
              onChange={(event) => onRenameChange(event.target.value)}
              onClick={(event) => event.stopPropagation()}
              onKeyDown={(event) => {
                event.stopPropagation();
                if (event.key === "Enter") {
                  event.preventDefault();
                  submittedRef.current = true;
                  onRenameSubmit();
                } else if (event.key === "Escape") {
                  event.preventDefault();
                  submittedRef.current = true;
                  onRenameCancel();
                }
              }}
              onBlur={() => {
                if (!submittedRef.current) onRenameSubmit();
              }}
              className="min-w-0 flex-1 rounded-md border border-[var(--sb-accent)] bg-[var(--sb-pane)] px-1.5 py-0.5 text-[14px] leading-[20px] outline-none"
            />
          ) : (
            <span className="sb-row-title">{bot.name}</span>
          )}
          {bot.pinned && <Pin className="h-3 w-3 shrink-0 text-[var(--sb-text-3)]" />}
          <span className="ml-auto flex shrink-0 items-center gap-1">
            {unread > 0 && <span className="sb-unread-dot" aria-label={t("sidebar.unreadCount", { n: unread })} />}
          </span>
        </div>
        <div className="sb-row-subtitle mt-0.5">{subtitle}</div>
      </div>
    </div>
  );
}

function GroupRow({
  group,
  active,
  dragging,
  unread,
  renaming,
  renameValue,
  onDragStart,
  onDragEnd,
  onClick,
  onContextMenu,
  onRenameChange,
  onRenameSubmit,
  onRenameCancel,
}: {
  group: Team;
  active: boolean;
  dragging: boolean;
  unread: boolean;
  renaming: boolean;
  renameValue: string;
  onDragStart: (event: React.DragEvent) => void;
  onDragEnd: () => void;
  onClick: () => void;
  onContextMenu: (event: React.MouseEvent) => void;
  onRenameChange: (value: string) => void;
  onRenameSubmit: () => void;
  onRenameCancel: () => void;
}) {
  const { t } = useLanguage();
  const submittedRef = useRef(false);
  useEffect(() => {
    submittedRef.current = false;
  }, [renaming]);
  const members = group.bots.slice(0, 3).map((item) => item.bot);
  return (
    <div
      role="button"
      tabIndex={0}
      draggable
      onDragStart={onDragStart}
      onDragEnd={onDragEnd}
      onClick={onClick}
      onContextMenu={onContextMenu}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onClick();
        }
      }}
      className="sb-row group/group"
      data-active={active ? "true" : undefined}
      style={{ opacity: dragging ? 0.35 : 1, transition: "opacity .12s ease" }}
    >
      <div className="flex h-9 w-9 shrink-0 items-center justify-center">
        {members.length === 0 ? (
          <span className="flex h-9 w-9 items-center justify-center rounded-[10px] bg-[var(--sb-hover)] text-[var(--sb-text-2)]">
            <Users className="h-4 w-4" />
          </span>
        ) : (
          <span className="flex -space-x-2">
            {members.map((member, index) => (
              <span
                key={member.id}
                className="rounded-[10px] ring-2 ring-[var(--sb-sidebar)]"
                style={{ zIndex: members.length - index }}
              >
                <BotAvatar
                  name={member.name}
                  id={member.id}
                  size={26}
                  shape={member.avatar_shape}
                  color={member.avatar_color}
                  avatar={member.avatar}
                />
              </span>
            ))}
          </span>
        )}
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          {renaming ? (
            <input
              autoFocus
              value={renameValue}
              onChange={(event) => onRenameChange(event.target.value)}
              onClick={(event) => event.stopPropagation()}
              onKeyDown={(event) => {
                event.stopPropagation();
                if (event.key === "Enter") {
                  event.preventDefault();
                  submittedRef.current = true;
                  onRenameSubmit();
                } else if (event.key === "Escape") {
                  event.preventDefault();
                  submittedRef.current = true;
                  onRenameCancel();
                }
              }}
              onBlur={() => {
                if (!submittedRef.current) onRenameSubmit();
              }}
              className="min-w-0 flex-1 rounded-md border border-[var(--sb-accent)] bg-[var(--sb-pane)] px-1.5 py-0.5 text-[14px] leading-[20px] outline-none"
            />
          ) : (
            <span className="sb-row-title">{group.name}</span>
          )}
          {group.pinned && <Pin className="h-3 w-3 shrink-0 text-[var(--sb-text-3)]" />}
          {unread && <span className="sb-unread-dot" aria-label={t("sidebar.unreadActivity")} />}
        </div>
        <div className="sb-row-subtitle mt-0.5">{t("sidebar.groupMeta", { n: group.bots.length })}</div>
      </div>
    </div>
  );
}
