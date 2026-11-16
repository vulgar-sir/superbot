"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  ArrowLeft,
  Ban,
  Bot as BotIcon,
  CheckCircle2,
  Loader2,
  PlugZap,
  Plus,
  Puzzle,
  Trash2,
  Users,
  X,
} from "lucide-react";

import { api, ApiError } from "@/lib/api";
import type { Bot, BotTemplateItem, Connector, McpServer, Skill, TeamReview, User } from "@/lib/types";
import { ConnectorDialog } from "@/components/ConnectorDialog";
import { useConfirm } from "@/components/ui/Confirm";
import { useToast } from "@/components/ui/Toast";
import { useLanguage } from "@/components/LanguageProvider";
import { Modal as UiModal } from "@/components/ui/Modal";
import { Rail } from "@/components/Rail";
import { BotAvatar } from "@/components/BotAvatar";
import { StatusPill } from "@/components/StatusPill";
import { cn, formatTime } from "@/lib/format";

const connectorCatalog = [
  { name: "GitHub", categoryKey: "ext.catCode", color: "#24292f", noteKey: "ext.noteGithub" },
  { name: "Slack", categoryKey: "ext.catComm", color: "#4a154b", noteKey: "ext.noteSlack" },
  { name: "Notion", categoryKey: "ext.catProductivity", color: "#111111", noteKey: "ext.noteNotion" },
  { name: "Google Drive", categoryKey: "ext.catProductivity", color: "#1a73e8", noteKey: "ext.noteDrive" },
  { name: "1Password", categoryKey: "ext.catPassword", color: "#1a8cff", noteKey: "ext.note1password" },
  { name: "Linear", categoryKey: "ext.catCode", color: "#5e6ad2", noteKey: "ext.noteLinear" },
  { name: "Vercel", categoryKey: "ext.catCode", color: "#111111", noteKey: "ext.noteVercel" },
  { name: "Sentry", categoryKey: "ext.catCode", color: "#362d59", noteKey: "ext.noteSentry" },
  { name: "Figma", categoryKey: "ext.catDesign", color: "#f24e1e", noteKey: "ext.noteFigma" },
  { name: "Postgres", categoryKey: "ext.catData", color: "#336791", noteKey: "ext.notePostgres" },
  { name: "CalDAV", categoryKey: "ext.catCalendar", color: "#0f766e", noteKey: "ext.noteCaldav" },
];

export default function ExtensionsPage() {
  const router = useRouter();
  const { confirm, confirmElement } = useConfirm();
  const { t } = useLanguage();
  const { toast } = useToast();
  const [user, setUser] = useState<User | null>(null);
  const [bots, setBots] = useState<Bot[]>([]);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [mcpServers, setMcpServers] = useState<McpServer[]>([]);
  const [booting, setBooting] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [createForm, setCreateForm] = useState({ name: "", description: "", content: "" });
  const [saving, setSaving] = useState(false);

  const [applySkill, setApplySkill] = useState<Skill | null>(null);
  const [accessSkill, setAccessSkill] = useState<Skill | null>(null);
  const [accessTemplate, setAccessTemplate] = useState<BotTemplateItem | null>(null);
  const [accessMembers, setAccessMembers] = useState<Array<{ id: string; email: string; display_name: string; role: string }>>([]);
  const [accessACL, setAccessACL] = useState<Array<{ user_id: string; role: string }>>([]);
  const [accessLoading, setAccessLoading] = useState(false);
  const [applyLoading, setApplyLoading] = useState(false);
  const [applySaving, setApplySaving] = useState(false);
  const [botSkillMap, setBotSkillMap] = useState<Record<string, string[]>>({});
  const [selectedBots, setSelectedBots] = useState<Set<string>>(new Set());

  const [connectors, setConnectors] = useState<Connector[]>([]);
  const [connectorOpen, setConnectorOpen] = useState(false);
  const [connectorProvider, setConnectorProvider] = useState("github");
  const [mcpOpen, setMcpOpen] = useState(false);
  const mcpImportRef = useRef<HTMLInputElement>(null);
  const [mcpForm, setMcpForm] = useState({
    name: "",
    transport: "http",
    url: "",
    headers: "{}",
    command: "",
    args: "[]",
    env: "{}",
  });
  const [mcpSaving, setMcpSaving] = useState(false);
  const [mcpTesting, setMcpTesting] = useState<string | null>(null);
  const [mcpTestResult, setMcpTestResult] = useState<Record<string, string>>({});
  const [applyMcp, setApplyMcp] = useState<McpServer | null>(null);
  const [applyMcpLoading, setApplyMcpLoading] = useState(false);
  const [applyMcpSaving, setApplyMcpSaving] = useState(false);
  const [botMcpMap, setBotMcpMap] = useState<Record<string, string[]>>({});
  const [selectedMcpBots, setSelectedMcpBots] = useState<Set<string>>(new Set());
  const [templates, setTemplates] = useState<BotTemplateItem[]>([]);
  const [publishBotID, setPublishBotID] = useState("");
  const [publishingTemplate, setPublishingTemplate] = useState(false);
  const [publishPublic, setPublishPublic] = useState(false);
  const [reviews, setReviews] = useState<TeamReview[]>([]);
  const [reviewBusy, setReviewBusy] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const me = await api.me();
        const [skillsResponse, botsResponse, mcpResponse, connectorsResponse, templatesResponse, reviewsResponse] = await Promise.all([
          api.listSkills(),
          api.listBots(),
          api.listMcpServers(),
          api.listConnectors().catch(() => ({ connectors: [] })),
          api.listBotTemplates().catch(() => ({ templates: [] })),
          api.listTeamReviews("pending").catch(() => ({ reviews: [] })),
        ]);
        if (cancelled) return;
        setUser(me.user);
        setSkills(skillsResponse.skills);
        setBots(botsResponse.bots);
        setMcpServers(mcpResponse.mcp_servers);
        setConnectors(connectorsResponse.connectors);
        setTemplates(templatesResponse.templates);
        setReviews(reviewsResponse.reviews);
        setBooting(false);
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setError(err instanceof Error ? err.message : "加载失败");
        setBooting(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [router]);

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } finally {
      router.replace("/login");
    }
  }, [router]);

  const drafts = useMemo(() => skills.filter((skill) => skill.status === "draft"), [skills]);
  const active = useMemo(() => skills.filter((skill) => skill.status === "active"), [skills]);
  const disabled = useMemo(() => skills.filter((skill) => skill.status === "disabled"), [skills]);

  const createSkill = useCallback(async () => {
    if (!createForm.name.trim() || !createForm.content.trim()) {
      setError("名称和内容必填");
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const result = await api.createSkill({
        name: createForm.name.trim(),
        description: createForm.description.trim(),
        content: createForm.content,
      });
      setSkills((prev) => [result.skill, ...prev]);
      setCreateOpen(false);
      setCreateForm({ name: "", description: "", content: "" });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "创建 Skill 失败");
    } finally {
      setSaving(false);
    }
  }, [createForm]);

  const approve = useCallback(async (skill: Skill) => {
    try {
      const result = await api.approveSkill(skill.id);
      setSkills((prev) => prev.map((item) => (item.id === skill.id ? result.skill : item)));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "批准失败");
    }
  }, []);

  const disable = useCallback(async (skill: Skill) => {
    try {
      const result = await api.disableSkill(skill.id);
      setSkills((prev) => prev.map((item) => (item.id === skill.id ? result.skill : item)));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "停用失败");
    }
  }, []);

  const remove = useCallback(async (skill: Skill) => {
    const ok = await confirm({
      title: `删除 Skill「${skill.name}」？`,
      description: t("ext.deleteSkillDesc"),
      confirmLabel: "删除",
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteSkill(skill.id);
      setSkills((prev) => prev.filter((item) => item.id !== skill.id));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "删除失败");
    }
  }, [confirm]);

  const openApply = useCallback(
    async (skill: Skill) => {
      setApplySkill(skill);
      setApplyLoading(true);
      setError(null);
      try {
        const entries = await Promise.all(
          bots.map(async (bot) => {
            const response = await api.getBotSkills(bot.id);
            const enabledIds = response.skills.filter((item) => item.enabled).map((item) => item.skill.id);
            return [bot.id, enabledIds] as const;
          }),
        );
        const map = Object.fromEntries(entries);
        setBotSkillMap(map);
        const selected = new Set(
          entries.filter(([, ids]) => ids.includes(skill.id)).map(([botID]) => botID),
        );
        setSelectedBots(selected as Set<string>);
      } catch (err) {
        setError(err instanceof ApiError ? err.message : "加载 Bot 技能失败");
      } finally {
        setApplyLoading(false);
      }
    },
    [bots],
  );

  const saveApply = useCallback(async () => {
    if (!applySkill) return;
    setApplySaving(true);
    setError(null);
    try {
      for (const bot of bots) {
        const current = botSkillMap[bot.id] ?? [];
        const has = current.includes(applySkill.id);
        const should = selectedBots.has(bot.id);
        if (has === should) continue;
        const next = should ? [...current, applySkill.id] : current.filter((id) => id !== applySkill.id);
        await api.setBotSkills(bot.id, next);
      }
      setApplySkill(null);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "应用失败");
    } finally {
      setApplySaving(false);
    }
  }, [applySkill, bots, botSkillMap, selectedBots]);

  const createMcp = useCallback(async () => {
    if (!mcpForm.name.trim()) {
      setError("MCP 名称必填");
      return;
    }
    const parseObject = (raw: string, label: string): Record<string, string> | null => {
      if (!raw.trim()) return {};
      try {
        const parsed = JSON.parse(raw);
        if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
          throw new Error(`${label} must be a JSON object`);
        }
        return Object.fromEntries(
          Object.entries(parsed as Record<string, unknown>).map(([key, value]) => [key, String(value)]),
        );
      } catch (err) {
        setError(err instanceof Error ? `${label} JSON 无效：${err.message}` : `${label} JSON 无效`);
        return null;
      }
    };
    if (mcpForm.transport === "http" && !mcpForm.url.trim()) {
      setError("HTTP MCP 需要 Endpoint");
      return;
    }
    if (mcpForm.transport === "stdio" && !mcpForm.command.trim()) {
      setError("stdio MCP 需要 command");
      return;
    }
    const headers = parseObject(mcpForm.headers, "headers");
    if (headers === null) return;
    const env = parseObject(mcpForm.env, "env");
    if (env === null) return;
    let args: string[] = [];
    try {
      const parsed = JSON.parse(mcpForm.args || "[]");
      if (!Array.isArray(parsed)) throw new Error("args must be a JSON array");
      args = parsed.map((item) => String(item));
    } catch (err) {
      setError(err instanceof Error ? `args JSON 无效：${err.message}` : "args JSON 无效");
      return;
    }
    setMcpSaving(true);
    setError(null);
    try {
      const result = await api.createMcpServer({
        name: mcpForm.name.trim(),
        transport: mcpForm.transport,
        url: mcpForm.url.trim(),
        headers,
        command: mcpForm.command.trim(),
        args,
        env,
      });
      setMcpServers((prev) => [result.mcp_server, ...prev]);
      setMcpOpen(false);
      setMcpForm({ name: "", transport: "http", url: "", headers: "{}", command: "", args: "[]", env: "{}" });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "创建 MCP Server 失败");
    } finally {
      setMcpSaving(false);
    }
  }, [mcpForm]);

  const testMcp = useCallback(async (server: McpServer) => {
    setMcpTesting(server.id);
    setError(null);
    try {
      const result = await api.testMcpServer(server.id);
      setMcpTestResult((prev) => ({
        ...prev,
        [server.id]: result.tools.length
          ? `连接成功 · ${result.tools.length} 个工具：${result.tools.join(", ")}`
          : "连接成功 · 没有工具",
      }));
    } catch (err) {
      setMcpTestResult((prev) => ({
        ...prev,
        [server.id]: err instanceof ApiError ? err.message : "连接失败",
      }));
    } finally {
      setMcpTesting(null);
    }
  }, []);

  const removeMcp = useCallback(async (server: McpServer) => {
    const ok = await confirm({
      title: `删除 MCP Server「${server.name}」？`,
      description: t("ext.deleteMcpDesc"),
      confirmLabel: "删除",
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteMcpServer(server.id);
      setMcpServers((prev) => prev.filter((item) => item.id !== server.id));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "删除失败");
    }
  }, [confirm]);

  const toggleMcpStatus = useCallback(async (server: McpServer) => {
    try {
      const result =
        server.status === "active"
          ? await api.disableMcpServer(server.id)
          : await api.approveMcpServer(server.id);
      setMcpServers((prev) => prev.map((item) => (item.id === server.id ? result.mcp_server : item)));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "更新状态失败");
    }
  }, []);

  const openApplyMcp = useCallback(
    async (server: McpServer) => {
      setApplyMcp(server);
      setApplyMcpLoading(true);
      setError(null);
      try {
        const entries = await Promise.all(
          bots.map(async (bot) => {
            const response = await api.getBotMcpServers(bot.id);
            return [
              bot.id,
              response.mcp_servers.filter((item) => item.enabled).map((item) => item.mcp_server.id),
            ] as const;
          }),
        );
        const map = Object.fromEntries(entries);
        setBotMcpMap(map);
        setSelectedMcpBots(
          new Set(entries.filter(([, ids]) => ids.includes(server.id)).map(([botID]) => botID)),
        );
      } catch (err) {
        setError(err instanceof ApiError ? err.message : "加载 Bot MCP 配置失败");
      } finally {
        setApplyMcpLoading(false);
      }
    },
    [bots],
  );

  const saveApplyMcp = useCallback(async () => {
    if (!applyMcp) return;
    setApplyMcpSaving(true);
    setError(null);
    try {
      for (const bot of bots) {
        const current = botMcpMap[bot.id] ?? [];
        const has = current.includes(applyMcp.id);
        const should = selectedMcpBots.has(bot.id);
        if (has === should) continue;
        const next = should ? [...current, applyMcp.id] : current.filter((id) => id !== applyMcp.id);
        await api.setBotMcpServers(bot.id, next);
      }
      setApplyMcp(null);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "应用失败");
    } finally {
      setApplyMcpSaving(false);
    }
  }, [applyMcp, bots, botMcpMap, selectedMcpBots]);

  if (booting) {
    return (
      <div className="flex h-screen items-center justify-center">
        <Loader2 className="h-5 w-5 animate-spin text-[var(--sb-text-2)]" />
      </div>
    );
  }
  if (!user) {
    return (
      <div className="flex h-screen items-center justify-center">
        <Link href="/login" className="text-[var(--sb-text-2)]">
          去登录
        </Link>
      </div>
    );
  }

  const publishTemplate = async () => {
    const bot = bots.find((item) => item.id === publishBotID);
    if (!bot) return;
    setPublishingTemplate(true);
    try {
      const result = await api.createBotTemplate({
        name: `${bot.name} 模板`,
        description: bot.description,
        bot_id: bot.id,
        visibility: publishPublic ? "public" : "account",
      });
      setTemplates((prev) => [result.template, ...prev]);
      toast("模板已发布", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "发布模板失败", "danger");
    } finally {
      setPublishingTemplate(false);
    }
  };

  const resolveReview = async (review: TeamReview, decision: "approve" | "reject") => {
    setReviewBusy(review.id);
    try {
      await api.resolveTeamReview(review.id, decision);
      setReviews((prev) => prev.filter((item) => item.id !== review.id));
      toast(decision === "approve" ? t("ext.approved") : t("ext.rejected"), "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "处理审查失败", "danger");
    } finally {
      setReviewBusy(null);
    }
  };

  const importTemplate = async (item: BotTemplateItem) => {
    try {
      const detail = await api.getBotTemplateRegistry(item.id);
      if (!detail.template.template) throw new Error("模板内容为空");
      await api.importBotTemplate(detail.template.template as Record<string, unknown>);
      toast("已从模板创建 Bot", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "导入模板失败", "danger");
    }
  };

  const removeTemplate = async (item: BotTemplateItem) => {
    const ok = await confirm({ title: "删除模板", description: `确定删除「${item.name}」？`, confirmLabel: "删除", danger: true });
    if (!ok) return;
    try {
      await api.deleteBotTemplate(item.id);
      setTemplates((prev) => prev.filter((tpl) => tpl.id !== item.id));
      toast("模板已删除", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "删除模板失败", "danger");
    }
  };

  const openTemplateAccess = async (item: BotTemplateItem) => {
    setAccessTemplate(item);
    setAccessLoading(true);
    setAccessACL([]);
    try {
      const accounts = await api.listAccounts();
      const active = accounts.accounts.find((account) => account.active);
      if (!active) return;
      const [members, acl] = await Promise.all([
        api.listAccountMembers(active.id),
        api.listTemplateACL(item.id),
      ]);
      setAccessMembers(members.members);
      setAccessACL(acl.acl);
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "读取模板访问权限失败", "danger");
    } finally {
      setAccessLoading(false);
    }
  };

  const updateTemplateRole = async (userId: string, role: string) => {
    if (!accessTemplate) return;
    try {
      if (role === "none") {
        await api.deleteTemplateACL(accessTemplate.id, userId);
        setAccessACL((prev) => prev.filter((row) => row.user_id !== userId));
      } else {
        await api.setTemplateACL(accessTemplate.id, userId, role);
        setAccessACL((prev) => [...prev.filter((row) => row.user_id !== userId), { user_id: userId, role }]);
      }
      toast("模板访问权限已更新", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "更新模板访问权限失败", "danger");
    }
  };

  const openSkillAccess = async (skill: Skill) => {
    setAccessSkill(skill);
    setAccessLoading(true);
    setAccessACL([]);
    try {
      const accounts = await api.listAccounts();
      const active = accounts.accounts.find((account) => account.active);
      if (!active) return;
      const [members, acl] = await Promise.all([
        api.listAccountMembers(active.id),
        api.listSkillACL(skill.id),
      ]);
      setAccessMembers(members.members);
      setAccessACL(acl.acl);
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "读取 Skill 访问权限失败", "danger");
    } finally {
      setAccessLoading(false);
    }
  };

  const updateSkillRole = async (userId: string, role: string) => {
    if (!accessSkill) return;
    try {
      if (role === "none") {
        await api.deleteSkillACL(accessSkill.id, userId);
        setAccessACL((prev) => prev.filter((row) => row.user_id !== userId));
      } else {
        await api.setSkillACL(accessSkill.id, userId, role);
        setAccessACL((prev) => [...prev.filter((row) => row.user_id !== userId), { user_id: userId, role }]);
      }
      toast("Skill 访问权限已更新", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "更新 Skill 访问权限失败", "danger");
    }
  };

  return (
    <div className="flex h-screen w-full overflow-hidden">
      <Rail user={user} onLogout={logout} />
      <main className="flex min-w-0 flex-1 flex-col pb-16 md:pb-0">
        <header className="sb-page-header flex h-[61px] shrink-0 items-center justify-between gap-3 border-b border-[var(--sb-border)] px-6">
          <div className="flex items-center gap-3">
            <Link
              href="/"
              className="sb-icon-btn"
              aria-label={t("ext.backToChat")}
            >
              <ArrowLeft className="h-4 w-4" />
            </Link>
            <div>
              <h1 className="text-[15px] font-semibold">{t("ext.title")}</h1>
            </div>
            <StatusPill tone="idle">{t("ext.skillsCount", { n: skills.length })}</StatusPill>
          </div>
          <button
            type="button"
            onClick={() => setCreateOpen(true)}
            className="sb-btn" data-variant="primary" data-size="sm"
          >
            <Plus className="h-3.5 w-3.5" />
            {t("ext.newSkill")}
          </button>
        </header>

        <div className="flex-1 overflow-y-auto px-6 py-6">
          {error && (
            <div className="mb-5 rounded-xl border border-danger/25 bg-danger/10 px-4 py-3 text-[12px] text-danger">
              {error}
            </div>
          )}

          <div className="mb-5 flex items-center gap-2 rounded-xl border border-[var(--sb-border)] bg-[var(--sb-pane)] px-4 py-3">
            <Puzzle className="h-4 w-4 text-[var(--sb-text-2)]" />
            <div className="text-[12px] leading-[18px] text-[var(--sb-text-2)]">
              {t("ext.skillIntro")}
            </div>
          </div>

          <section className="mb-7">
            <SectionTitle title={t("ext.templates")} hint={`${templates.length} templates`} />
            <div className="mb-3 flex flex-wrap items-center gap-2 rounded-xl border border-[var(--sb-border)] bg-[var(--sb-pane)] px-3 py-2.5">
              <select
                value={publishBotID}
                onChange={(event) => setPublishBotID(event.target.value)}
                className="h-8 min-w-[180px] flex-1 rounded-lg bg-[var(--sb-hover)] px-2 text-[12px] outline-none"
              >
                <option value="">{t("ext.selectBotToPublish")}</option>
                {bots.map((bot) => <option key={bot.id} value={bot.id}>{bot.name}</option>)}
              </select>
              <label className="flex items-center gap-1.5 text-[11.5px] text-[var(--sb-text-2)]">
                <input
                  type="checkbox"
                  checked={publishPublic}
                  onChange={(event) => setPublishPublic(event.target.checked)}
                  className="h-3.5 w-3.5 accent-[var(--sb-text)]"
                />
                {t("ext.publishToStore")}
              </label>
              <button
                type="button"
                disabled={!publishBotID || publishingTemplate}
                onClick={() => void publishTemplate()}
                className="sb-btn" data-variant="primary" data-size="sm"
              >
                {publishingTemplate ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
                {t("ext.publishTemplate")}
              </button>
            </div>
            {templates.length === 0 ? (
              <div className="rounded-xl border border-dashed border-[var(--sb-border)] py-8 text-center text-[12px] text-[var(--sb-text-3)]">
                {t("ext.templatesEmpty")}
              </div>
            ) : (
              <div className="grid gap-3 md:grid-cols-2">
                {templates.map((item) => (
                  <div key={item.id} className="rounded-xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-3.5">
                    <div className="flex items-center gap-2">
                      <span className="min-w-0 flex-1 truncate text-[13px] font-medium">{item.name}</span>
                      <span className="font-mono text-[9px] uppercase text-[var(--sb-text-3)]">v{item.version}</span>
                      <span className="text-[10px] text-[var(--sb-text-3)]">{item.visibility === "owner" ? t("ext.ownerOnly") : t("ext.account")}</span>
                    </div>
                    <div className="mt-1 line-clamp-2 text-[11.5px] text-[var(--sb-text-2)]">{item.description || t("ext.noDescription")}</div>
                    <div className="mt-3 flex gap-2">
                      <ActionButton onClick={() => void importTemplate(item)} tone="mint">
                        <Plus className="h-3.5 w-3.5" />
                        {t("ext.importAsBot")}
                      </ActionButton>
                      <ActionButton onClick={() => void openTemplateAccess(item)} tone="muted">
                        <Users className="h-3.5 w-3.5" />
                        {t("ext.access")}
                      </ActionButton>
                      <ActionButton onClick={() => void removeTemplate(item)} tone="danger">
                        <Trash2 className="h-3.5 w-3.5" />
                        {t("ext.delete")}
                      </ActionButton>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </section>

          <section className="mb-7">
            <SectionTitle title={t("ext.reviews")} hint={`${reviews.length} pending`} />
            {reviews.length === 0 ? (
              <div className="rounded-xl border border-dashed border-[var(--sb-border)] py-8 text-center text-[12px] text-[var(--sb-text-3)]">
                {t("ext.reviewsEmpty")}
              </div>
            ) : (
              <div className="space-y-3">
                {reviews.map((review) => {
                  const bot = bots.find((item) => item.id === review.team_bot_id);
                  return (
                    <div
                      key={review.id}
                      className="rounded-xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-4"
                    >
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="text-[13px] font-medium">{review.title}</span>
                        <span className="rounded-full bg-[var(--sb-hover)] px-2 py-0.5 font-mono text-[9px] uppercase text-[var(--sb-text-3)]">
                          {review.kind}
                        </span>
                        <span className="text-[11px] text-[var(--sb-text-3)]">
                          {bot?.name ?? "Team Bot"} · {formatTime(review.created_at)}
                        </span>
                      </div>
                      {review.description && (
                        <div className="mt-1 text-[12px] text-[var(--sb-text-2)]">{review.description}</div>
                      )}
                      {review.content && (
                        <pre className="mt-2 max-h-40 overflow-y-auto whitespace-pre-wrap rounded-lg bg-[var(--sb-hover)] p-2.5 text-[11.5px] leading-[18px] text-[var(--sb-text-2)]">
                          {review.content}
                        </pre>
                      )}
                      <div className="mt-3 flex items-center gap-2">
                        <button
                          type="button"
                          disabled={reviewBusy === review.id}
                          onClick={() => void resolveReview(review, "approve")}
                          className="sb-btn"
                          data-variant="primary"
                          data-size="sm"
                        >
                          {reviewBusy === review.id ? (
                            <Loader2 className="h-3.5 w-3.5 animate-spin" />
                          ) : (
                            <CheckCircle2 className="h-3.5 w-3.5" />
                          )}
                          {t("ext.approve")}
                        </button>
                        <button
                          type="button"
                          disabled={reviewBusy === review.id}
                          onClick={() => void resolveReview(review, "reject")}
                          className="sb-btn"
                          data-variant="secondary"
                          data-size="sm"
                        >
                          <Ban className="h-3.5 w-3.5" />
                          {t("ext.reject")}
                        </button>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </section>

          <section className="mb-7">
            <SectionTitle title={t("ext.title")} hint="MCP connectors" />
            <div className="grid gap-x-8 gap-y-0 sm:grid-cols-2">
              {connectorCatalog.map((connector) => (
                <div
                  key={connector.name}
                  className="flex h-16 items-center gap-3 border-b border-[var(--sb-border)]"
                >
                  <div
                    className="flex h-6 w-6 shrink-0 items-center justify-center rounded-[7px] font-mono text-[10px] font-semibold text-white"
                    style={{ background: connector.color }}
                  >
                    {connector.name.slice(0, 1)}
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5">
                      <span className="truncate text-[13px] font-medium text-[var(--sb-text)]">{connector.name}</span>
                      <span className="font-mono text-[9px] uppercase tracking-[0.08em] text-[var(--sb-text-3)]">
                        {t(connector.categoryKey)}
                      </span>
                    </div>
                    <div className="truncate text-[12px] text-[var(--sb-text-2)]">{t(connector.noteKey)}</div>
                  </div>
                  <button
                    type="button"
                    onClick={() => {
                      setConnectorProvider(connector.name.toLowerCase());
                      setConnectorOpen(true);
                    }}
                    className="h-8 shrink-0 rounded-full bg-[var(--sb-hover)] px-4 text-[12px] text-[var(--sb-text)] transition hover:bg-[var(--sb-active)]"
                  >
                    {t("ext.connect")}
                  </button>
                </div>
              ))}
            </div>
            <p className="mt-3 px-1 text-[11px] text-[var(--sb-text-3)]">
              {t("ext.connectorNote")}
            </p>
          </section>

          {connectors.length > 0 && (
            <section className="mb-7">
              <SectionTitle title={t("ext.connectedApps")} hint={`${connectors.length} connectors`} />
              <div className="grid gap-2 sm:grid-cols-2">
                {connectors.map((connector) => (
                  <div key={connector.id} className="flex items-center gap-2 rounded-xl border border-[var(--sb-border)] bg-[var(--sb-pane)] px-3 py-2.5">
                    <span className="min-w-0 flex-1 truncate text-[11.5px] text-[var(--sb-text)]">
                      {connector.name}
                      <span className="ml-1.5 font-mono text-[8px] uppercase tracking-[0.08em] text-muted/45">
                        {connector.provider}
                      </span>
                    </span>
                    <button
                      type="button"
                      onClick={async () => {
                        try {
                          await api.testConnector(connector.id);
                          toast(`${connector.name} 连接正常`, "success");
                        } catch (err) {
                          toast(err instanceof ApiError ? err.message : "连接测试失败", "danger");
                        }
                      }}
                      className="rounded-md border border-[var(--sb-border)] px-2 py-1 text-[9px] text-muted hover:border-[var(--sb-border-strong)] hover:text-[var(--sb-text-2)]"
                    >
                      测试
                    </button>
                    <button
                      type="button"
                      onClick={async () => {
                        const ok = await confirm({
                          title: `断开 ${connector.name}？`,
                          description: t("ext.connectorDisconnectDesc"),
                          confirmLabel: "断开",
                          danger: true,
                        });
                        if (!ok) return;
                        try {
                          await api.deleteConnector(connector.id);
                          setConnectors((prev) => prev.filter((item) => item.id !== connector.id));
                        } catch (err) {
                          setError(err instanceof ApiError ? err.message : "断开失败");
                        }
                      }}
                      className="text-muted/40 hover:text-danger"
                      aria-label={`断开 ${connector.name}`}
                    >
                      <Trash2 className="h-3 w-3" />
                    </button>
                  </div>
                ))}
              </div>
            </section>
          )}

          {drafts.length > 0 && (
            <section className="mb-7">
              <SectionTitle title={t("ext.pending")} hint={`${drafts.length} drafts`} />
              <div className="grid gap-3 md:grid-cols-2">
                {drafts.map((skill) => (
                  <SkillCard
                    key={skill.id}
                    skill={skill}
                    actions={
                      <>
                        <ActionButton onClick={() => approve(skill)} tone="mint">
                          <CheckCircle2 className="h-3.5 w-3.5" />
                          {t("ext.approve")}
                        </ActionButton>
                        <ActionButton onClick={() => void openSkillAccess(skill)} tone="muted">
                          <Users className="h-3.5 w-3.5" />
                          {t("ext.access")}
                        </ActionButton>
                        <ActionButton onClick={() => remove(skill)} tone="danger">
                          <Trash2 className="h-3.5 w-3.5" />
                          {t("ext.delete")}
                        </ActionButton>
                      </>
                    }
                  />
                ))}
              </div>
            </section>
          )}

          <section className="mb-7">
            <SectionTitle title={t("ext.enabled")} hint={`${active.length} active`} />
            {active.length === 0 ? (
              <EmptySkills onNew={() => setCreateOpen(true)} />
            ) : (
              <div className="grid gap-3 md:grid-cols-2">
                {active.map((skill) => (
                  <SkillCard
                    key={skill.id}
                    skill={skill}
                    actions={
                      <>
                        <ActionButton onClick={() => openApply(skill)} tone="mint">
                          <BotIcon className="h-3.5 w-3.5" />
                          {t("ext.applyToBots")}
                        </ActionButton>
                        <ActionButton onClick={() => void openSkillAccess(skill)} tone="muted">
                          <Users className="h-3.5 w-3.5" />
                          {t("ext.access")}
                        </ActionButton>
                        <ActionButton onClick={() => disable(skill)} tone="muted">
                          <Ban className="h-3.5 w-3.5" />
                          {t("ext.disable")}
                        </ActionButton>
                      </>
                    }
                  />
                ))}
              </div>
            )}
          </section>

          {disabled.length > 0 && (
            <section>
              <SectionTitle title={t("ext.disabled")} hint={`${disabled.length} disabled`} />
              <div className="grid gap-3 md:grid-cols-2">
                {disabled.map((skill) => (
                  <SkillCard
                    key={skill.id}
                    skill={skill}
                    actions={
                      <>
                        <ActionButton onClick={() => approve(skill)} tone="mint">
                          <CheckCircle2 className="h-3.5 w-3.5" />
                          {t("ext.enable")}
                        </ActionButton>
                        <ActionButton onClick={() => void openSkillAccess(skill)} tone="muted">
                          <Users className="h-3.5 w-3.5" />
                          {t("ext.access")}
                        </ActionButton>
                        <ActionButton onClick={() => remove(skill)} tone="danger">
                          <Trash2 className="h-3.5 w-3.5" />
                          {t("ext.delete")}
                        </ActionButton>
                      </>
                    }
                  />
                ))}
              </div>
            </section>
          )}

          <section className="mt-8">
            <div className="mb-3 flex items-center justify-between px-1">
              <div className="font-mono text-[9px] uppercase tracking-[0.2em] text-muted">
                {t("ext.mcpServers")}
              </div>
              <div className="flex items-center gap-2">
                <input
                  ref={mcpImportRef}
                  type="file"
                  accept="application/json,.json"
                  className="hidden"
                  onChange={async (event) => {
                    const file = event.target.files?.[0];
                    event.target.value = "";
                    if (!file) return;
                    try {
                      const payload = JSON.parse(await file.text()) as Record<string, unknown>;
                      const result = await api.importMcp(payload);
                      setMcpServers((prev) => [...result.mcp_servers, ...prev]);
                      toast("MCP 配置已导入", "success");
                    } catch (err) {
                      toast(err instanceof Error ? err.message : "导入 MCP 失败", "danger");
                    }
                  }}
                />
                <button
                  type="button"
                  onClick={() => mcpImportRef.current?.click()}
                  className="inline-flex items-center gap-1 rounded-lg border border-[var(--sb-border)] px-2.5 py-1.5 text-[10.5px] text-[var(--sb-text-2)] transition hover:bg-[var(--sb-hover)]"
                >
                  {t("ext.import")}
                </button>
                <button
                  type="button"
                  onClick={() => setMcpOpen(true)}
                  className="inline-flex items-center gap-1 rounded-lg border border-[var(--sb-border-strong)] bg-[var(--sb-hover)] px-2.5 py-1.5 text-[10.5px] text-[var(--sb-text-2)] transition hover:bg-mint/[0.1]"
                >
                  <Plus className="h-3 w-3" />
                  {t("ext.newMcp")}
                </button>
              </div>
            </div>
            <div className="mb-3 flex items-center gap-2 rounded-xl border border-[var(--sb-border)] bg-[var(--sb-pane)] px-4 py-3">
              <PlugZap className="h-4 w-4 text-[var(--sb-text-2)]" />
              <div className="text-[11.5px] text-muted">
                {t("ext.mcpIntro")}
              </div>
            </div>
            {mcpServers.length === 0 ? (
              <div className="flex min-h-[180px] flex-col items-center justify-center rounded-2xl border border-dashed border-[var(--sb-border)] bg-[var(--sb-pane)]">
                <PlugZap className="h-6 w-6 text-muted/50" />
                <div className="mt-2 text-[12px] text-muted">{t("ext.noMcp")}</div>
                <div className="mt-1 text-[10.5px] text-muted/50">
                  {t("ext.mcpUrlPlaceholder")}
                </div>
              </div>
            ) : (
              <div className="grid gap-3 md:grid-cols-2">
                {mcpServers.map((server) => (
                  <article key={server.id} className="rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-4">
                    <div className="flex items-start gap-3">
                      <div className="flex h-9 w-9 items-center justify-center rounded-xl border bg-[var(--sb-hover)] text-[var(--sb-text-2)]">
                        <PlugZap className="h-4 w-4" />
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <h3 className="truncate text-[13px] font-medium text-[var(--sb-text)]">{server.name}</h3>
                          <span className="rounded border border-[var(--sb-border)] px-1.5 py-0.5 font-mono text-[8.5px] uppercase tracking-[0.1em] text-muted">
                            {server.status}
                          </span>
                          {server.has_headers && (
                            <span className="rounded bg-[var(--sb-hover)] px-1 py-0.5 font-mono text-[8px] text-muted">
                              headers
                            </span>
                          )}
                        </div>
                        <div className="mt-1 truncate font-mono text-[9.5px] text-muted/55">{server.url}</div>
                      </div>
                    </div>
                    {mcpTestResult[server.id] && (
                      <div
                        className={cn(
                          "mt-2 rounded-lg border px-2.5 py-1.5 text-[10px] leading-relaxed",
                          /失败|错误|error|failed/i.test(mcpTestResult[server.id])
                            ? "border-danger/20 bg-danger/[0.05] text-danger/85"
                            : "border-mint/20 bg-mint/[0.05] text-[var(--sb-text-2)]/85",
                        )}
                      >
                        {mcpTestResult[server.id]}
                      </div>
                    )}
                    <div className="mt-3 flex flex-wrap items-center gap-1.5 border-t border-[var(--sb-border)] pt-3">
                      <ActionButton onClick={() => testMcp(server)} tone="muted">
                        {mcpTesting === server.id ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          <PlugZap className="h-3.5 w-3.5" />
                        )}
                        {t("ext.testConnection")}
                      </ActionButton>
                      <ActionButton onClick={() => openApplyMcp(server)} tone="mint">
                        <BotIcon className="h-3.5 w-3.5" />
                        {t("ext.applyToBots")}
                      </ActionButton>
                      <ActionButton onClick={() => toggleMcpStatus(server)} tone="muted">
                        {server.status === "active" ? (
                          <>
                            <Ban className="h-3.5 w-3.5" />
                            {t("ext.disable")}
                          </>
                        ) : (
                          <>
                            <CheckCircle2 className="h-3.5 w-3.5" />
                            {t("ext.enable")}
                          </>
                        )}
                      </ActionButton>
                      <ActionButton onClick={() => removeMcp(server)} tone="danger">
                        <Trash2 className="h-3.5 w-3.5" />
                        {t("ext.delete")}
                      </ActionButton>
                    </div>
                  </article>
                ))}
              </div>
            )}
          </section>
        </div>
      </main>

      {confirmElement}

      {createOpen && (
        <Modal title="新建 Skill" subtitle={t("ext.skillHint")} onClose={() => setCreateOpen(false)}>
          <div className="grid gap-3">
            <Field label="名称">
              <input
                className="field"
                value={createForm.name}
                onChange={(event) => setCreateForm((prev) => ({ ...prev, name: event.target.value }))}
                placeholder="例如：发布前检查清单"
              />
            </Field>
            <Field label={t("ext.skillSummary")}>
              <input
                className="field"
                value={createForm.description}
                onChange={(event) =>
                  setCreateForm((prev) => ({ ...prev, description: event.target.value }))
                }
                placeholder="Bot 会看到这段描述，决定何时加载它"
              />
            </Field>
            <Field label={t("ext.skillBody")}>
              <textarea
                className="field min-h-[220px] font-mono text-[12px]"
                value={createForm.content}
                onChange={(event) =>
                  setCreateForm((prev) => ({ ...prev, content: event.target.value }))
                }
                placeholder={"# 发布前检查清单\n\n1. 运行测试\n2. 检查变更\n3. 输出结论"}
              />
            </Field>
          </div>
          <div className="mt-4 flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setCreateOpen(false)}
              className="rounded-lg border border-[var(--sb-border)] px-3 py-2 text-[11.5px] text-muted hover:text-[var(--sb-text)]"
            >
              {t("ext.cancel")}
            </button>
            <button
              type="button"
              onClick={createSkill}
              disabled={saving}
              className="flex items-center gap-1.5 rounded-lg border border-mint/40 bg-mint px-3.5 py-2 text-[11.5px] font-semibold text-ink disabled:opacity-60"
            >
              {saving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {t("ext.create")}
            </button>
          </div>
        </Modal>
      )}

      {applySkill && (
        <Modal
          title={`应用「${applySkill.name}」`}
          subtitle={t("ext.skillBots")}
          onClose={() => setApplySkill(null)}
        >
          {applyLoading ? (
            <div className="flex items-center justify-center py-10">
              <Loader2 className="h-5 w-5 animate-spin text-[var(--sb-text-2)]" />
            </div>
          ) : (
            <div className="grid max-h-[52vh] gap-1.5 overflow-y-auto md:grid-cols-2">
              {bots.map((bot) => {
                const checked = selectedBots.has(bot.id);
                return (
                  <button
                    key={bot.id}
                    type="button"
                    onClick={() => {
                      setSelectedBots((prev) => {
                        const next = new Set(prev);
                        if (next.has(bot.id)) next.delete(bot.id);
                        else next.add(bot.id);
                        return next;
                      });
                    }}
                    className={cn(
                      "flex items-center gap-2.5 rounded-lg border px-3 py-2.5 text-left transition",
                      checked ? "border-[var(--sb-border)] bg-[var(--sb-hover)]" : "border-[var(--sb-border)] bg-[var(--sb-hover)]/30",
                    )}
                  >
                    <BotAvatar name={bot.name} id={bot.id} size={30} />
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-[12px] text-[var(--sb-text)]">{bot.name}</div>
                      <div className="truncate font-mono text-[9px] text-muted/60">{bot.model}</div>
                    </div>
                    {checked && <CheckCircle2 className="h-4 w-4 text-[var(--sb-text-2)]" />}
                  </button>
                );
              })}
              {bots.length === 0 && (
                <div className="md:col-span-2 py-8 text-center text-[12px] text-muted">
                  {t("ext.noBots")}
                </div>
              )}
            </div>
          )}
          <div className="mt-4 flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setApplySkill(null)}
              className="rounded-lg border border-[var(--sb-border)] px-3 py-2 text-[11.5px] text-muted hover:text-[var(--sb-text)]"
            >
              {t("ext.cancel")}
            </button>
            <button
              type="button"
              onClick={saveApply}
              disabled={applySaving || applyLoading}
              className="flex items-center gap-1.5 rounded-lg border border-mint/40 bg-mint px-3.5 py-2 text-[11.5px] font-semibold text-ink disabled:opacity-60"
            >
              {applySaving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {t("ext.saveApply")}
            </button>
          </div>
        </Modal>
      )}

      <ConnectorDialog
        open={connectorOpen}
        initialProvider={connectorProvider}
        onClose={() => setConnectorOpen(false)}
        onCreated={(connector) => setConnectors((prev) => [connector, ...prev])}
      />

      {mcpOpen && (
        <Modal
          title={t("ext.newMcpServer")}
          subtitle={t("ext.mcpHint")}
          onClose={() => setMcpOpen(false)}
        >
          <div className="grid gap-3">
            <Field label="名称">
              <input
                className="field"
                value={mcpForm.name}
                onChange={(event) => setMcpForm((prev) => ({ ...prev, name: event.target.value }))}
                placeholder="例如：内部工具网关"
              />
            </Field>
            <Field label={t("ext.mcpTransport")}>
              <select
                className="field"
                value={mcpForm.transport}
                onChange={(event) => setMcpForm((prev) => ({ ...prev, transport: event.target.value }))}
              >
                <option value="http">HTTP（Streamable HTTP）</option>
                <option value="stdio">stdio（本地命令）</option>
              </select>
            </Field>
            {mcpForm.transport === "http" ? (
              <>
                <Field label="MCP Endpoint (HTTP)">
                  <input
                    className="field font-mono text-[12px]"
                    value={mcpForm.url}
                    onChange={(event) => setMcpForm((prev) => ({ ...prev, url: event.target.value }))}
                    placeholder="https://mcp.example.com/mcp"
                  />
                </Field>
                <Field label={t("ext.mcpHeaders")}>
                  <textarea
                    className="field min-h-[100px] font-mono text-[11.5px]"
                    value={mcpForm.headers}
                    onChange={(event) => setMcpForm((prev) => ({ ...prev, headers: event.target.value }))}
                    placeholder={'{"Authorization": "Bearer …"}'}
                  />
                </Field>
              </>
            ) : (
              <>
                <Field label="Command">
                  <input
                    className="field font-mono text-[12px]"
                    value={mcpForm.command}
                    onChange={(event) => setMcpForm((prev) => ({ ...prev, command: event.target.value }))}
                    placeholder="npx"
                  />
                </Field>
                <Field label={t("ext.mcpArgs")}>
                  <textarea
                    className="field min-h-[80px] font-mono text-[11.5px]"
                    value={mcpForm.args}
                    onChange={(event) => setMcpForm((prev) => ({ ...prev, args: event.target.value }))}
                    placeholder={'["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]'}
                  />
                </Field>
                <Field label={t("ext.mcpEnv")}>
                  <textarea
                    className="field min-h-[80px] font-mono text-[11.5px]"
                    value={mcpForm.env}
                    onChange={(event) => setMcpForm((prev) => ({ ...prev, env: event.target.value }))}
                    placeholder={'{"TOKEN": "…"}'}
                  />
                </Field>
              </>
            )}
          </div>
          <div className="mt-4 flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setMcpOpen(false)}
              className="rounded-lg border border-[var(--sb-border)] px-3 py-2 text-[11.5px] text-muted hover:text-[var(--sb-text)]"
            >
              {t("ext.cancel")}
            </button>
            <button
              type="button"
              onClick={createMcp}
              disabled={mcpSaving}
              className="flex items-center gap-1.5 rounded-lg border border-mint/40 bg-mint px-3.5 py-2 text-[11.5px] font-semibold text-ink disabled:opacity-60"
            >
              {mcpSaving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {t("ext.saveMcp")}
            </button>
          </div>
        </Modal>
      )}

      {applyMcp && (
        <Modal
          title={`应用「${applyMcp.name}」`}
          subtitle={t("ext.mcpBots")}
          onClose={() => setApplyMcp(null)}
        >
          {applyMcpLoading ? (
            <div className="flex items-center justify-center py-10">
              <Loader2 className="h-5 w-5 animate-spin text-[var(--sb-text-2)]" />
            </div>
          ) : (
            <div className="grid max-h-[52vh] gap-1.5 overflow-y-auto md:grid-cols-2">
              {bots.map((bot) => {
                const checked = selectedMcpBots.has(bot.id);
                return (
                  <button
                    key={bot.id}
                    type="button"
                    onClick={() => {
                      setSelectedMcpBots((prev) => {
                        const next = new Set(prev);
                        if (next.has(bot.id)) next.delete(bot.id);
                        else next.add(bot.id);
                        return next;
                      });
                    }}
                    className={cn(
                      "flex items-center gap-2.5 rounded-lg border px-3 py-2.5 text-left transition",
                      checked ? "border-[var(--sb-border)] bg-[var(--sb-hover)]" : "border-[var(--sb-border)] bg-[var(--sb-hover)]/30",
                    )}
                  >
                    <BotAvatar name={bot.name} id={bot.id} size={30} />
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-[12px] text-[var(--sb-text)]">{bot.name}</div>
                      <div className="truncate font-mono text-[9px] text-muted/60">{bot.model}</div>
                    </div>
                    {checked && <CheckCircle2 className="h-4 w-4 text-[var(--sb-text-2)]" />}
                  </button>
                );
              })}
              {bots.length === 0 && (
                <div className="md:col-span-2 py-8 text-center text-[12px] text-muted">{t("ext.noBots")}</div>
              )}
            </div>
          )}
          <div className="mt-4 flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setApplyMcp(null)}
              className="rounded-lg border border-[var(--sb-border)] px-3 py-2 text-[11.5px] text-muted hover:text-[var(--sb-text)]"
            >
              {t("ext.cancel")}
            </button>
            <button
              type="button"
              onClick={saveApplyMcp}
              disabled={applyMcpSaving || applyMcpLoading}
              className="flex items-center gap-1.5 rounded-lg border border-mint/40 bg-mint px-3.5 py-2 text-[11.5px] font-semibold text-ink disabled:opacity-60"
            >
              {applyMcpSaving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {t("ext.saveApply")}
            </button>
          </div>
        </Modal>
      )}

      {accessTemplate && (
        <Modal
          title={`「${accessTemplate.name}」访问权限`}
          subtitle="账户成员默认只读，可单独授予编辑或运行权限"
          onClose={() => setAccessTemplate(null)}
        >
          {accessLoading ? (
            <div className="flex justify-center py-8"><Loader2 className="h-4 w-4 animate-spin text-[var(--sb-text-3)]" /></div>
          ) : (
            <div className="space-y-1">
              {accessMembers.map((member) => {
                const acl = accessACL.find((row) => row.user_id === member.id);
                const accountEditor = member.role === "owner" || member.role === "admin";
                return (
                  <div key={member.id} className="flex items-center gap-2 rounded-lg border border-[var(--sb-border)] px-2.5 py-2">
                    <span className="min-w-0 flex-1 truncate text-[12px]">{member.display_name || member.email}</span>
                    <select
                      disabled={accountEditor}
                      value={accountEditor ? "editor" : acl?.role ?? "none"}
                      onChange={(event) => void updateTemplateRole(member.id, event.target.value)}
                      className="h-7 rounded-md bg-[var(--sb-hover)] px-1.5 text-[11px] outline-none disabled:opacity-60"
                    >
                      {accountEditor && <option value="editor">账户角色</option>}
                      {!accountEditor && <option value="none">无授权</option>}
                      {!accountEditor && <option value="viewer">查看</option>}
                      {!accountEditor && <option value="editor">编辑</option>}
                      {!accountEditor && <option value="runner">运行</option>}
                    </select>
                  </div>
                );
              })}
            </div>
          )}
          <div className="mt-4 flex justify-end">
            <button type="button" onClick={() => setAccessTemplate(null)} className="sb-btn" data-variant="secondary" data-size="sm">关闭</button>
          </div>
        </Modal>
      )}

      {accessSkill && (
        <Modal
          title={`「${accessSkill.name}」访问权限`}
          subtitle="账户成员默认只读，可单独授予编辑或运行权限"
          onClose={() => setAccessSkill(null)}
        >
          {accessLoading ? (
            <div className="flex justify-center py-8"><Loader2 className="h-4 w-4 animate-spin text-[var(--sb-text-3)]" /></div>
          ) : (
            <div className="space-y-1">
              {accessMembers.map((member) => {
                const acl = accessACL.find((row) => row.user_id === member.id);
                const accountEditor = member.role === "owner" || member.role === "admin";
                return (
                  <div key={member.id} className="flex items-center gap-2 rounded-lg border border-[var(--sb-border)] px-2.5 py-2">
                    <span className="min-w-0 flex-1 truncate text-[12px]">{member.display_name || member.email}</span>
                    <select
                      disabled={accountEditor}
                      value={accountEditor ? "editor" : acl?.role ?? "none"}
                      onChange={(event) => void updateSkillRole(member.id, event.target.value)}
                      className="h-7 rounded-md bg-[var(--sb-hover)] px-1.5 text-[11px] outline-none disabled:opacity-60"
                    >
                      {accountEditor && <option value="editor">账户角色</option>}
                      {!accountEditor && <option value="none">无授权</option>}
                      {!accountEditor && <option value="viewer">查看</option>}
                      {!accountEditor && <option value="editor">编辑</option>}
                      {!accountEditor && <option value="runner">运行</option>}
                    </select>
                  </div>
                );
              })}
            </div>
          )}
          <div className="mt-4 flex justify-end">
            <button type="button" onClick={() => setAccessSkill(null)} className="sb-btn" data-variant="secondary" data-size="sm">关闭</button>
          </div>
        </Modal>
      )}
    </div>
  );
}

function SkillCard({ skill, actions }: { skill: Skill; actions: React.ReactNode }) {
  const { t, language } = useLanguage();
  return (
    <article className="flex flex-col rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-4">
      <div className="flex items-start gap-3">
        <div className="flex h-9 w-9 items-center justify-center rounded-xl border bg-[var(--sb-hover)] text-[var(--sb-text-2)]">
          <Puzzle className="h-4 w-4" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <h3 className="truncate text-[13px] font-medium text-[var(--sb-text)]">{skill.name}</h3>
            <span className="rounded border border-[var(--sb-border)] px-1.5 py-0.5 font-mono text-[8.5px] uppercase tracking-[0.1em] text-muted">
              {skill.status}
            </span>
          </div>
          <div className="mt-1 flex items-center gap-2 font-mono text-[9px] text-muted/55">
            <span>{skill.created_by === "bot" ? t("ext.botProposed") : t("ext.userCreated")}</span>
            <span className="text-muted/30">·</span>
            <span>{formatTime(skill.updated_at)}</span>
          </div>
        </div>
      </div>
      <p className="mt-2 line-clamp-2 min-h-[32px] text-[11.5px] leading-relaxed text-muted">
        {(language === "en" && skill.description_en) || skill.description || t("ext.noDescription")}
      </p>
      <div className="mt-3 flex flex-wrap items-center gap-1.5 border-t border-[var(--sb-border)] pt-3">
        {actions}
      </div>
    </article>
  );
}

function ActionButton({
  onClick,
  tone,
  children,
}: {
  onClick: () => void;
  tone: "mint" | "muted" | "danger";
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "inline-flex items-center gap-1 rounded-lg border px-2.5 py-1.5 text-[10.5px] transition",
        tone === "mint" && "border-[var(--sb-border-strong)] bg-[var(--sb-hover)] text-[var(--sb-text-2)] hover:bg-mint/[0.1]",
        tone === "muted" && "border-[var(--sb-border)] text-muted hover:text-text",
        tone === "danger" && "border-danger/20 bg-danger/[0.04] text-danger/80 hover:text-danger",
      )}
    >
      {children}
    </button>
  );
}

function SectionTitle({ title, hint }: { title: string; hint: string }) {
  return (
    <div className="mb-3 flex items-center justify-between px-1">
      <div className="text-[14px] font-medium text-[var(--sb-text)]">{title}</div>
      <div className="font-mono text-[10px] uppercase tracking-[0.08em] text-[var(--sb-text-3)]">{hint}</div>
    </div>
  );
}

function EmptySkills({ onNew }: { onNew: () => void }) {
  return (
    <div className="flex min-h-[220px] flex-col items-center justify-center rounded-2xl border border-dashed border-[var(--sb-border)] bg-[var(--sb-pane)]">
      <div className="flex h-12 w-12 items-center justify-center rounded-2xl border bg-[var(--sb-hover)]">
        <Puzzle className="h-5 w-5 text-[var(--sb-text-2)]" />
      </div>
      <h3 className="mt-3 text-[14px] font-medium text-[var(--sb-text)]">还没有启用的 Skill</h3>
      <p className="mt-1 max-w-sm text-center text-[11.5px] text-muted">
        创建一个流程说明，然后应用到 Bot。Bot 也可以在对话中通过 extension_propose 提交草稿。
      </p>
      <button
        type="button"
        onClick={onNew}
        className="mt-4 inline-flex items-center gap-1.5 rounded-lg border border-mint/40 bg-mint px-3 py-2 text-[11.5px] font-semibold text-ink"
      >
        <Plus className="h-3.5 w-3.5" />
        新建 Skill
      </button>
    </div>
  );
}

function Modal({
  title,
  subtitle,
  onClose,
  children,
}: {
  title: string;
  subtitle?: string;
  onClose: () => void;
  children: React.ReactNode;
}) {
  return (
    <UiModal open onClose={onClose} title={title} subtitle={subtitle} size="lg">
      {children}
    </UiModal>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="mb-1.5 block font-mono text-[9px] uppercase tracking-[0.16em] text-muted">
        {label}
      </label>
      {children}
    </div>
  );
}
