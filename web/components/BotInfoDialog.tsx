"use client";

import { useEffect, useState } from "react";
import { Download, Loader2, Pin, Sparkles } from "lucide-react";

import { api } from "@/lib/api";
import { BotAvatar } from "./BotAvatar";
import { BotLibrary } from "./BotLibrary";
import { BotMemory } from "./BotMemory";
import { Modal } from "./ui/Modal";
import { useToast } from "./ui/Toast";
import { cn } from "@/lib/format";
import type { Bot, SidebarSection } from "@/lib/types";

type Props = {
  bot: Bot;
  initialTab?: "profile" | "library" | "memory" | "access";
  sections: SidebarSection[];
  saving: boolean;
  error: string | null;
  isPrimary: boolean;
  settingPrimary: boolean;
  primaryError: string | null;
  onSetPrimary: () => void;
  onClose: () => void;
  onSave: (input: {
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
  }) => void;
};

export function BotInfoDialog({
  bot,
  initialTab = "profile",
  sections,
  saving,
  error,
  isPrimary,
  settingPrimary,
  primaryError,
  onSetPrimary,
  onClose,
  onSave,
}: Props) {
  const { toast } = useToast();
  const [tab, setTab] = useState(initialTab);
  const [name, setName] = useState(bot.name);
  const [title, setTitle] = useState(bot.title);
  const [description, setDescription] = useState(bot.description);
  const [sectionID, setSectionID] = useState(bot.section_id ?? "");
  const [pinned, setPinned] = useState(bot.pinned);
  const [proactivity, setProactivity] = useState<boolean>(
    (bot.proactivity as { enabled?: boolean } | null)?.enabled !== false,
  );
  const [providerID, setProviderID] = useState(bot.provider_id ?? "");
  const [model, setModel] = useState(bot.model ?? "");
  const [paramsText, setParamsText] = useState(
    JSON.stringify(bot.params ?? {}, null, 2),
  );
  const [paramsError, setParamsError] = useState<string | null>(null);
  const [visibility, setVisibility] = useState<"owner" | "account">(
    bot.visibility === "owner" ? "owner" : "account",
  );
  const [providers, setProviders] = useState<Array<{ id: string; name: string }>>([]);
  const [models, setModels] = useState<Record<string, Array<{ name: string }>>>({});
  const [exporting, setExporting] = useState(false);
  const [accountMembers, setAccountMembers] = useState<Array<{ id: string; email: string; display_name: string; role: string }>>([]);
  const [aclRows, setAclRows] = useState<Array<{ user_id: string; role: string }>>([]);
  const [aclLoading, setAclLoading] = useState(false);

  useEffect(() => {
    setName(bot.name);
    setTitle(bot.title);
    setDescription(bot.description);
    setSectionID(bot.section_id ?? "");
    setPinned(bot.pinned);
    setProactivity((bot.proactivity as { enabled?: boolean } | null)?.enabled !== false);
    setProviderID(bot.provider_id ?? "");
    setModel(bot.model ?? "");
    setVisibility(bot.visibility === "owner" ? "owner" : "account");
    setParamsText(JSON.stringify(bot.params ?? {}, null, 2));
    setParamsError(null);
    setTab(initialTab);
  }, [bot, initialTab]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const response = await api.listProviders();
        const llmProviders = response.providers.filter((provider) => provider.type === "llm");
        if (cancelled) return;
        setProviders(llmProviders);
        const entries = await Promise.all(
          llmProviders.map(async (provider) => {
            const result = await api.listModels(provider.id);
            return [provider.id, result.models] as const;
          }),
        );
        if (!cancelled) setModels(Object.fromEntries(entries));
      } catch {
        // The profile fields still work if provider discovery fails.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const exportTemplate = async () => {
    setExporting(true);
    try {
      const result = await api.getBotTemplate(bot.id);
      const blob = new Blob([JSON.stringify(result.template, null, 2)], {
        type: "application/json",
      });
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = `${bot.name}-template.json`;
      link.click();
      URL.revokeObjectURL(url);
      toast("模板已导出", "success");
    } finally {
      setExporting(false);
    }
  };

  const tabs: Array<{ key: "profile" | "library" | "memory" | "access"; label: string }> = [
    { key: "profile", label: "资料" },
    { key: "access", label: "访问" },
    { key: "memory", label: "记忆" },
    { key: "library", label: "资料库" },
  ];

  useEffect(() => {
    if (tab !== "access") return;
    let cancelled = false;
    setAclLoading(true);
    (async () => {
      try {
        const accounts = await api.listAccounts();
        const active = accounts.accounts.find((account) => account.active);
        if (!active) return;
        const [members, acl] = await Promise.all([
          api.listAccountMembers(active.id),
          api.listBotACL(bot.id),
        ]);
        if (cancelled) return;
        setAccountMembers(members.members);
        setAclRows(acl.acl);
      } finally {
        if (!cancelled) setAclLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [tab, bot.id]);

  return (
    <Modal
      open
      onClose={onClose}
      title="Bot 设置"
      size="md"
      footer={
        tab === "profile" ? (
          <>
            <button type="button" className="sb-btn" data-variant="secondary" onClick={onClose}>
              取消
            </button>
            <button
              type="button"
              className="sb-btn"
              data-variant="primary"
              disabled={saving || !name.trim()}
              onClick={() => {
                let params: Record<string, unknown> | null = null;
                try {
                  const parsed = JSON.parse(paramsText || "{}");
                  if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
                    params = parsed as Record<string, unknown>;
                    setParamsError(null);
                  } else {
                    setParamsError("参数必须是 JSON 对象");
                    return;
                  }
                } catch (err) {
                  setParamsError(err instanceof Error ? err.message : "参数不是合法 JSON");
                  return;
                }
                onSave({
                  name: name.trim(),
                  title: title.trim(),
                  description: description.trim(),
                  avatar_shape: bot.avatar_shape,
                  avatar_color: bot.avatar_color,
                  section_id: sectionID || null,
                  pinned,
                  provider_id: providerID || null,
                  model,
                  params,
                  visibility,
                });
              }}
            >
              {saving && <Loader2 className="sb-spin h-3.5 w-3.5" />}
              保存
            </button>
          </>
        ) : (
          <button type="button" className="sb-btn" data-variant="secondary" onClick={onClose}>
            关闭
          </button>
        )
      }
    >
      <div className="flex items-center gap-3">
        <BotAvatar
          name={name || bot.name}
          id={bot.id}
          size={52}
          shape={bot.avatar_shape}
          color={bot.avatar_color}
          avatar={bot.avatar}
        />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="truncate text-[16px] font-semibold">{name || bot.name}</span>
            {isPrimary && (
              <span className="shrink-0 rounded-full bg-[var(--sb-hover)] px-2 py-0.5 text-[10px] text-[var(--sb-text-2)]">
                主 Bot
              </span>
            )}
            {bot.kind === "team" && (
              <span className="shrink-0 rounded-full bg-[var(--sb-hover)] px-2 py-0.5 text-[10px] text-[var(--sb-text-2)]">
                Team Bot
              </span>
            )}
          </div>
          <div className="truncate text-[12px] text-[var(--sb-text-2)]">
            {title || bot.description || bot.model || "AI 同事"}
          </div>
        </div>
      </div>

      <div className="sb-tabs mt-4 border-b border-[var(--sb-border)] pb-2">
        {tabs.map((entry) => (
          <button
            key={entry.key}
            type="button"
            className="sb-tab"
            data-selected={tab === entry.key ? "true" : undefined}
            onClick={() => setTab(entry.key)}
          >
            {entry.label}
          </button>
        ))}
      </div>

      {tab === "profile" ? (
        <div className="mt-5 space-y-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <label className="block">
              <span className="sb-field-label">名称</span>
              <input className="sb-field" value={name} onChange={(event) => setName(event.target.value)} />
            </label>
            <label className="block">
              <span className="sb-field-label">标题</span>
              <input
                className="sb-field"
                value={title}
                onChange={(event) => setTitle(event.target.value)}
                placeholder="一句话角色，例如：研究负责人"
              />
            </label>
          </div>
          <label className="block">
            <span className="sb-field-label">描述</span>
            <textarea
              className="sb-field !min-h-[88px]"
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              placeholder="它会负责什么、怎么和队友协作、有哪些边界。"
            />
          </label>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="grid gap-3 sm:grid-cols-2">
              <label className="block">
                <span className="sb-field-label">Provider</span>
                <select
                  className="sb-field"
                  value={providerID}
                  onChange={(event) => {
                    const next = event.target.value;
                    setProviderID(next);
                    const first = models[next]?.[0]?.name;
                    setModel((prev) => (prev && models[next]?.some((item) => item.name === prev) ? prev : first ?? ""));
                  }}
                >
                  <option value="">自动选择</option>
                  {providers.map((provider) => (
                    <option key={provider.id} value={provider.id}>
                      {provider.name}
                    </option>
                  ))}
                </select>
              </label>
              <label className="block">
                <span className="sb-field-label">模型</span>
                <select
                  className="sb-field"
                  value={model}
                  onChange={(event) => setModel(event.target.value)}
                >
                  {!providerID && <option value={model}>{model || "自动选择"}</option>}
                  {(models[providerID] ?? []).map((item) => (
                    <option key={item.name} value={item.name}>
                      {item.name}
                    </option>
                  ))}
                </select>
              </label>
            </div>
            <label className="block">
              <span className="sb-field-label">可见性</span>
              <select
                className="sb-field"
                value={visibility}
                onChange={(event) => setVisibility(event.target.value as "owner" | "account")}
              >
                <option value="account">账户成员可见</option>
                <option value="owner">仅创建者可见</option>
              </select>
            </label>
            <label className="block">
              <span className="sb-field-label">侧栏分组</span>
              <select
                className="sb-field"
                value={sectionID}
                onChange={(event) => setSectionID(event.target.value)}
              >
                <option value="">未分组</option>
                {sections.map((section) => (
                  <option key={section.id} value={section.id}>
                    {section.name}
                  </option>
                ))}
              </select>
            </label>
            <label className="block">
              <span className="sb-field-label">参数（JSON）</span>
              <textarea
                className="sb-field font-[var(--sb-mono)] !text-[12px]"
                rows={4}
                value={paramsText}
                onChange={(event) => setParamsText(event.target.value)}
                placeholder={'{"temperature": 0.7}'}
              />
              {paramsError && (
                <span className="mt-1 block text-[11px] text-[var(--sb-danger)]">{paramsError}</span>
              )}
            </label>
            <div className="flex items-end">
              <button
                type="button"
                onClick={() => setPinned((prev) => !prev)}
                className={cn(
                  "flex h-10 w-full items-center gap-2 rounded-lg border px-3 text-[13px] transition",
                  pinned
                    ? "border-[var(--sb-border-strong)] bg-[var(--sb-hover)] text-[var(--sb-text)]"
                    : "border-[var(--sb-border)] text-[var(--sb-text-2)] hover:bg-[var(--sb-hover)]",
                )}
              >
                <Pin className={cn("h-3.5 w-3.5", pinned && "text-[var(--sb-text)]")} />
                {pinned ? "已置顶到分组" : "置顶到分组"}
              </button>
            </div>
          </div>

          <div className="flex items-center justify-between gap-4 border-t border-[var(--sb-border)] pt-4">
            <div>
              <div className="text-[13px] leading-[18px]">主动消息</div>
              <div className="text-[11px] leading-[16px] text-[var(--sb-text-3)]">
                允许 Bot 主动给你发消息（send_to_user）；关闭后该工具会被拒绝。
              </div>
            </div>
            <button
              type="button"
              className="sb-switch"
              data-on={proactivity ? "true" : undefined}
              aria-label="主动消息"
              onClick={() => {
                const next = !proactivity;
                setProactivity(next);
                void api
                  .setBotProactivity(bot.id, next)
                  .catch(() => {
                    setProactivity(!next);
                    toast("更新主动消息设置失败", "danger");
                  });
              }}
            />
          </div>

          <div className="flex flex-wrap items-center gap-2 border-t border-[var(--sb-border)] pt-4">
            <button
              type="button"
              onClick={() => void exportTemplate()}
              disabled={exporting}
              className="sb-btn"
              data-variant="outline"
              data-size="sm"
            >
              {exporting ? <Loader2 className="sb-spin h-3.5 w-3.5" /> : <Download className="h-3.5 w-3.5" />}
              导出模板
            </button>
            {isPrimary ? (
              <span className="text-[12px] text-[var(--sb-text-2)]">当前主 Bot</span>
            ) : (
              <button
                type="button"
                disabled={settingPrimary || bot.kind === "team"}
                onClick={onSetPrimary}
                title={bot.kind === "team" ? "Team Bot 不能设为主 Bot" : "设为主 Bot"}
                className="sb-btn"
                data-variant="secondary"
                data-size="sm"
              >
                {settingPrimary ? <Loader2 className="sb-spin h-3.5 w-3.5" /> : <Sparkles className="h-3.5 w-3.5" />}
                {bot.kind === "team" ? "Team Bot 不能设为主 Bot" : "设为主 Bot"}
              </button>
            )}
          </div>

          {(error || primaryError) && (
            <div className="rounded-lg bg-[var(--sb-danger-soft)] px-3 py-2 text-[12px] text-[var(--sb-danger)]">
              {error || primaryError}
            </div>
          )}
        </div>
      ) : tab === "access" ? (
        <div className="mt-5 space-y-3">
          <div className="text-[12px] leading-[18px] text-[var(--sb-text-2)]">
            owner/admin 默认可编辑；普通成员默认仅可查看，可单独授予编辑或运行权限。
          </div>
          {aclLoading ? (
            <div className="flex justify-center py-8"><Loader2 className="sb-spin h-4 w-4 text-[var(--sb-text-3)]" /></div>
          ) : (
            accountMembers.map((member) => {
              const acl = aclRows.find((row) => row.user_id === member.id);
              const accountEditor = member.role === "owner" || member.role === "admin";
              return (
                <div key={member.id} className="flex items-center gap-3 rounded-xl border border-[var(--sb-border)] px-3 py-2.5">
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-[13px]">{member.display_name || member.email}</div>
                    <div className="truncate text-[11px] text-[var(--sb-text-3)]">{member.email} · {member.role}</div>
                  </div>
                  <select
                    className="h-8 rounded-lg bg-[var(--sb-hover)] px-2 text-[12px] outline-none disabled:opacity-60"
                    disabled={accountEditor}
                    value={accountEditor ? "editor" : acl?.role ?? "none"}
                    onChange={async (event) => {
                      const role = event.target.value;
                      try {
                        if (role === "none") {
                          await api.deleteBotACL(bot.id, member.id);
                          setAclRows((prev) => prev.filter((row) => row.user_id !== member.id));
                        } else {
                          await api.setBotACL(bot.id, member.id, role);
                          setAclRows((prev) => [
                            ...prev.filter((row) => row.user_id !== member.id),
                            { user_id: member.id, role },
                          ]);
                        }
                        toast("访问权限已更新", "success");
                      } catch {
                        toast("更新访问权限失败", "danger");
                      }
                    }}
                  >
                    {accountEditor && <option value="editor">可编辑（账户角色）</option>}
                    {!accountEditor && <option value="none">无单独授权</option>}
                    {!accountEditor && <option value="viewer">可查看</option>}
                    {!accountEditor && <option value="editor">可编辑</option>}
                    {!accountEditor && <option value="runner">可运行</option>}
                  </select>
                </div>
              );
            })
          )}
        </div>
      ) : tab === "memory" ? (
        <div className="mt-4">
          <BotMemory bot={bot} />
        </div>
      ) : (
        <div className="mt-4">
          <BotLibrary bot={bot} />
        </div>
      )}
    </Modal>
  );
}
