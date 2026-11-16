"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  ArrowLeft,
  Cpu,
  Loader2,
  Pencil,
  Play,
  Plus,
  Trash2,
  Users,
  X,
} from "lucide-react";

import { api, ApiError } from "@/lib/api";
import type { Bot, Model, Provider, Team, User } from "@/lib/types";
import { Rail } from "@/components/Rail";
import { useLanguage } from "@/components/LanguageProvider";
import { BotAvatar } from "@/components/BotAvatar";
import { StatusPill } from "@/components/StatusPill";
import { useConfirm } from "@/components/ui/Confirm";
import { cn } from "@/lib/format";

type TeamForm = {
  name: string;
  description: string;
  selected: string[];
  maxRounds: string;
  mode: string;
  coordinatorProvider: string;
  coordinatorModel: string;
};

const emptyForm: TeamForm = {
  name: "",
  description: "",
  selected: [],
  maxRounds: "4",
  mode: "roundtable",
  coordinatorProvider: "",
  coordinatorModel: "",
};

export default function TeamsPage() {
  const { t } = useLanguage();
  const router = useRouter();
  const { confirm, confirmElement } = useConfirm();
  const [user, setUser] = useState<User | null>(null);
  const [bots, setBots] = useState<Bot[]>([]);
  const [providers, setProviders] = useState<Provider[]>([]);
  const [models, setModels] = useState<Record<string, Model[]>>({});
  const [teams, setTeams] = useState<Team[]>([]);
  const [booting, setBooting] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [form, setForm] = useState<TeamForm>(emptyForm);
  const [saving, setSaving] = useState(false);
  const [starting, setStarting] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const me = await api.me();
        const [botsResponse, providersResponse, teamsResponse] = await Promise.all([
          api.listBots(),
          api.listProviders(),
          api.listTeams(),
        ]);
        if (cancelled) return;
        setUser(me.user);
        setBots(botsResponse.bots);
        const llmProviders = providersResponse.providers.filter((provider) => provider.type === "llm");
        setProviders(llmProviders);
        setTeams(teamsResponse.teams);
        const entries = await Promise.all(
          llmProviders.map(async (provider) => [provider.id, (await api.listModels(provider.id)).models] as const),
        );
        if (cancelled) return;
        setModels(Object.fromEntries(entries));
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

  const botByID = useMemo(() => new Map(bots.map((bot) => [bot.id, bot])), [bots]);
  const coordinatorOptions = useMemo(
    () =>
      providers.flatMap((provider) =>
        (models[provider.id] ?? []).map((model) => ({
          providerId: provider.id,
          providerName: provider.name,
          model: model.name,
          label: `${provider.name} · ${model.name}`,
        })),
      ),
    [providers, models],
  );

  const openCreate = useCallback(() => {
    setEditingId(null);
    setForm({
      ...emptyForm,
      coordinatorProvider: providers[0]?.id ?? "",
      coordinatorModel: providers[0] ? models[providers[0].id]?.[0]?.name ?? "" : "",
    });
    setError(null);
    setEditorOpen(true);
  }, [providers, models]);

  const openEdit = useCallback((team: Team) => {
    setEditingId(team.id);
    setForm({
      name: team.name,
      description: team.description,
      selected: [...team.bots].sort((a, b) => a.position - b.position).map((item) => item.bot.id),
      maxRounds: String(team.config.max_rounds ?? 4),
      mode: team.mode || "roundtable",
      coordinatorProvider: String(team.config.coordinator_provider_id ?? ""),
      coordinatorModel: String(team.config.coordinator_model ?? ""),
    });
    setError(null);
    setEditorOpen(true);
  }, []);

  const toggleMember = (botID: string) => {
    const selected = form.selected;
    if (!selected.includes(botID) && form.mode === "group" && selected.length >= 6) {
      setError("群聊最多包含 6 个 Bot");
      return;
    }
    setForm((prev) => ({
      ...prev,
      selected: prev.selected.includes(botID)
        ? prev.selected.filter((id) => id !== botID)
        : [...prev.selected, botID],
    }));
  };

  const save = useCallback(async () => {
    if (!form.name.trim()) {
      setError("团队名称必填");
      return;
    }
    if (form.selected.length === 0) {
      setError("至少选择一个 Bot");
      return;
    }
    if (form.mode === "group" && form.selected.length > 6) {
      setError("群聊最多包含 6 个 Bot");
      return;
    }
    setSaving(true);
    setError(null);
    const config: Record<string, unknown> = {
      max_rounds: Math.max(1, Math.min(12, Number(form.maxRounds || "4"))),
    };
    if (form.coordinatorProvider && form.coordinatorModel) {
      config.coordinator_provider_id = form.coordinatorProvider;
      config.coordinator_model = form.coordinatorModel;
    }
    try {
      if (editingId) {
        const result = await api.updateTeam(editingId, {
          name: form.name.trim(),
          description: form.description.trim(),
          mode: form.mode,
          config,
        });
        const withBots = await api.setTeamBots(
          editingId,
          form.selected.map((botID, position) => ({ bot_id: botID, position, role: "member" })),
        );
        setTeams((prev) => prev.map((team) => (team.id === editingId ? withBots.team : team)));
        void result;
      } else {
        const result = await api.createTeam({
          name: form.name.trim(),
          description: form.description.trim(),
          mode: form.mode,
          config,
          bot_ids: form.selected,
        });
        setTeams((prev) => [...prev, result.team]);
      }
      setEditorOpen(false);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "保存团队失败");
    } finally {
      setSaving(false);
    }
  }, [editingId, form]);

  const remove = useCallback(async (team: Team) => {
    const ok = await confirm({
      title: `删除群聊「${team.name}」？`,
      description: "该群聊的会话与房间记录也会被删除，此操作不可恢复。",
      confirmLabel: "删除",
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteTeam(team.id);
      setTeams((prev) => prev.filter((item) => item.id !== team.id));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "删除失败");
    }
  }, [confirm]);

  const start = useCallback(
    async (team: Team) => {
      setStarting(team.id);
      setError(null);
      try {
        if (team.mode === "group" && team.room_conversation_id) {
          router.push(`/?conv=${team.room_conversation_id}`);
          return;
        }
        const result = await api.createConversation({ kind: "team", team_id: team.id });
        router.push(`/?conv=${result.conversation.id}`);
      } catch (err) {
        setError(err instanceof ApiError ? err.message : "创建团队会话失败");
      } finally {
        setStarting(null);
      }
    },
    [router],
  );

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } finally {
      router.replace("/login");
    }
  }, [router]);

  useEffect(() => {
    if (!editorOpen) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setEditorOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [editorOpen]);

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

  return (
    <div className="flex h-screen w-full overflow-hidden">
      <Rail user={user} onLogout={logout} />
      <main className="flex min-w-0 flex-1 flex-col pb-16 md:pb-0">
        <header className="sb-page-header flex h-[61px] shrink-0 items-center justify-between gap-3 border-b border-[var(--sb-border)] px-6">
          <div className="flex items-center gap-3">
            <Link
              href="/"
              className="sb-icon-btn"
              aria-label={t("common.backToChat")}
            >
              <ArrowLeft className="h-4 w-4" />
            </Link>
            <div>
              <h1 className="text-[15px] font-semibold">{t("teams.title")}</h1>
            </div>
            <StatusPill tone="idle">{t("teams.count", { n: teams.length })}</StatusPill>
          </div>
          <button
            type="button"
            onClick={openCreate}
            className="sb-btn" data-variant="primary" data-size="sm"
          >
            <Plus className="h-3.5 w-3.5" />
            {t("sidebar.newGroup")}
          </button>
        </header>

        <div className="flex-1 overflow-y-auto px-6 py-6">
          {error && (
            <div className="mb-5 rounded-xl border border-danger/25 bg-danger/10 px-4 py-3 text-[12px] text-danger">
              {error}
            </div>
          )}
          {teams.length === 0 ? (
            <div className="flex min-h-[420px] flex-col items-center justify-center rounded-2xl border border-dashed border-[var(--sb-border)] bg-[var(--sb-pane)]">
              <div className="flex h-14 w-14 items-center justify-center rounded-2xl border bg-[var(--sb-hover)]">
                <Users className="h-6 w-6 text-[var(--sb-text-2)]" />
              </div>
              <h2 className="mt-4 text-[17px] font-medium text-[var(--sb-text)]">{t("teams.empty")}</h2>
              <p className="mt-1.5 max-w-md text-center text-[12px] text-muted">
                把 2-6 个 Bot 组成团队。协调者会读取团队名册和对话，自动决定下一位发言者。
              </p>
              <button
                type="button"
                onClick={openCreate}
                className="mt-5 inline-flex items-center gap-1.5 rounded-lg border border-mint/40 bg-mint px-3.5 py-2 text-[12px] font-semibold text-ink"
              >
                <Plus className="h-3.5 w-3.5" />
                组建第一个团队
              </button>
            </div>
          ) : (
            <div className="grid gap-4 md:grid-cols-2 2xl:grid-cols-3">
              {teams.map((team) => (
                <article
                  key={team.id}
                  className="flex flex-col rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-5 transition hover:border-[var(--sb-border-strong)] hover:bg-[var(--sb-pane)]"
                >
                  <div className="flex items-start gap-3">
                    <div className="flex -space-x-2">
                      {team.bots.slice(0, 3).map((item) => (
                        <BotAvatar
                          key={item.bot.id}
                          name={item.bot.name}
                          id={item.bot.id}
                          size={38}
                          avatar={item.bot.avatar}
                          className="ring-2 ring-panel"
                        />
                      ))}
                      {team.bots.length === 0 && (
                        <div className="flex h-[38px] w-[38px] items-center justify-center rounded-xl border border-[var(--sb-border)] text-muted">
                          <Users className="h-4 w-4" />
                        </div>
                      )}
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <h2 className="truncate text-[14px] font-medium text-[var(--sb-text)]">{team.name}</h2>
                        <span className="rounded border border-[var(--sb-border)] px-1.5 py-0.5 font-mono text-[8.5px] text-muted">
                          {team.bots.length} bots
                        </span>
                      </div>
                      <div className="mt-1 flex items-center gap-1.5 font-mono text-[10.5px] text-[var(--sb-text-3)]">
                        <Cpu className="h-2.5 w-2.5" />
                        {team.mode} · max {team.config.max_rounds ?? 4} rounds
                      </div>
                    </div>
                  </div>
                  <p className="mt-3 line-clamp-2 min-h-[36px] text-[12.5px] leading-[19px] text-[var(--sb-text-2)]">
                    {team.description || "圆桌协作模式"}
                  </p>
                  <div className="mt-3 flex flex-wrap gap-1.5">
                    {team.bots.slice(0, 6).map((item, index) => (
                      <span
                        key={item.bot.id}
                        className="inline-flex items-center gap-1 rounded-full border border-[var(--sb-border)] bg-[var(--sb-hover)] px-2.5 py-1 font-mono text-[10px] text-[var(--sb-text-2)]"
                      >
                        <span className="text-muted/40">{index + 1}</span>
                        {item.bot.name}
                      </span>
                    ))}
                  </div>
                  <div className="mt-4 flex items-center justify-between border-t border-[var(--sb-border)] pt-3">
                    <button
                      type="button"
                      onClick={() => start(team)}
                      disabled={starting === team.id || team.bots.length === 0}
                      className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-[var(--sb-border)] bg-[var(--sb-hover)] px-3 text-[12px] font-medium text-[var(--sb-text-2)] transition hover:bg-mint/15 disabled:opacity-50"
                    >
                      {starting === team.id ? (
                        <Loader2 className="h-3 w-3 animate-spin" />
                      ) : (
                        <Play className="h-3 w-3" />
                      )}
                      开始协作
                    </button>
                    <div className="flex items-center gap-1">
                      <button
                        type="button"
                        onClick={() => openEdit(team)}
                        className="flex h-7 w-7 items-center justify-center rounded-md text-muted transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-text-2)]"
                        aria-label={t("teams.edit")}
                      >
                        <Pencil className="h-3.5 w-3.5" />
                      </button>
                      <button
                        type="button"
                        onClick={() => remove(team)}
                        className="flex h-7 w-7 items-center justify-center rounded-md text-muted transition hover:bg-danger/10 hover:text-danger"
                        aria-label={t("teams.delete")}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </div>
                  </div>
                </article>
              ))}
            </div>
          )}
        </div>
      </main>

      {confirmElement}

      {editorOpen && (
        <div className="sb-overlay">
          <button
            type="button"
            className="absolute inset-0"
            onClick={() => setEditorOpen(false)}
            aria-label={t("common.close")}
          />
          <div className="sb-modal sb-dialog-lg">
            <div className="flex items-start justify-between border-b border-[var(--sb-border)] px-5 py-4">
              <div>
                <div className="font-mono text-[9px] uppercase tracking-[0.22em] text-[var(--sb-text-2)]">
                  {editingId ? "edit team" : "new team"}
                </div>
                <h2 className="mt-1 text-[17px] font-medium text-[var(--sb-text)]">
                  {editingId ? "编辑团队" : "组建团队"}
                </h2>
              </div>
              <button
                type="button"
                onClick={() => setEditorOpen(false)}
                className="sb-icon-btn" data-size="sm"
                aria-label={t("common.close")}
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            <div className="grid gap-4 overflow-y-auto p-5 md:grid-cols-2">
              <Field label={t("teams.name")}>
                <input
                  className="field"
                  value={form.name}
                  onChange={(event) => setForm((prev) => ({ ...prev, name: event.target.value }))}
                  placeholder={t("teams.namePlaceholder")}
                />
              </Field>
              <Field label={t("teams.maxRounds")}>
                <input
                  className="field"
                  type="number"
                  min={1}
                  max={12}
                  value={form.maxRounds}
                  onChange={(event) => setForm((prev) => ({ ...prev, maxRounds: event.target.value }))}
                />
              </Field>
              <Field label={t("teams.mode")}>
                <select
                  className="field"
                  value={form.mode}
                  onChange={(event) => setForm((prev) => ({ ...prev, mode: event.target.value }))}
                >
                  <option value="roundtable">{t("teams.modeRoundtable")}</option>
                  <option value="pipeline">{t("teams.modePipeline")}</option>
                  <option value="router">{t("teams.modeRouter")}</option>
                </select>
              </Field>
              <Field label={t("teams.description")} className="md:col-span-2">
                <input
                  className="field"
                  value={form.description}
                  onChange={(event) =>
                    setForm((prev) => ({ ...prev, description: event.target.value }))
                  }
                  placeholder={t("teams.descriptionPlaceholder")}
                />
              </Field>
              <div className="md:col-span-2">
                <div className="mb-1.5 flex items-center justify-between">
                  <label className="sb-field-label">
                    成员（按顺序加入）
                  </label>
                  <span className="font-mono text-[9px] text-muted/50">{form.selected.length} selected</span>
                </div>
                <div className="grid max-h-56 gap-1.5 overflow-y-auto rounded-xl border border-[var(--sb-border)] bg-[var(--sb-hover)] p-2 md:grid-cols-2">
                  {bots.map((bot) => {
                    const selected = form.selected.includes(bot.id);
                    const order = form.selected.indexOf(bot.id);
                    return (
                      <button
                        key={bot.id}
                        type="button"
                        onClick={() => toggleMember(bot.id)}
                        className={cn(
                          "flex items-center gap-2 rounded-lg border px-2.5 py-2 text-left transition",
                          selected
                            ? "border-[var(--sb-border)] bg-[var(--sb-hover)]"
                            : "border-[var(--sb-border)] bg-[var(--sb-hover)]/30 hover:border-mint/20",
                        )}
                      >
                        <BotAvatar name={bot.name} id={bot.id} size={26} avatar={bot.avatar} />
                        <div className="min-w-0 flex-1">
                          <div className="truncate text-[11.5px] text-[var(--sb-text)]">{bot.name}</div>
                          <div className="truncate font-mono text-[8.5px] text-muted/60">{bot.model}</div>
                        </div>
                        {selected && (
                          <span className="flex h-4 w-4 items-center justify-center rounded-full bg-mint font-mono text-[9px] text-ink">
                            {order + 1}
                          </span>
                        )}
                      </button>
                    );
                  })}
                  {bots.length === 0 && (
                    <div className="md:col-span-2 px-3 py-4 text-center text-[11px] text-muted">
                      还没有 Bot，请先到{" "}
                      <Link href="/bots" className="text-[var(--sb-text-2)] underline">
                        Bot 页面
                      </Link>{" "}
                      创建。
                    </div>
                  )}
                </div>
              </div>
              <Field label={t("teams.coordinatorModel")}>
                <select
                  className="field"
                  value={
                    form.coordinatorProvider && form.coordinatorModel
                      ? `${form.coordinatorProvider}::${form.coordinatorModel}`
                      : ""
                  }
                  onChange={(event) => {
                    const value = event.target.value;
                    if (!value) {
                      setForm((prev) => ({
                        ...prev,
                        coordinatorProvider: "",
                        coordinatorModel: "",
                      }));
                      return;
                    }
                    const [providerId, model] = value.split("::");
                    setForm((prev) => ({
                      ...prev,
                      coordinatorProvider: providerId,
                      coordinatorModel: model,
                    }));
                  }}
                >
                  <option value="">{t("teams.followFirst")}</option>
                  {coordinatorOptions.map((option) => (
                    <option
                      key={`${option.providerId}::${option.model}`}
                      value={`${option.providerId}::${option.model}`}
                    >
                      {option.label}
                    </option>
                  ))}
                </select>
              </Field>
              <div className="flex items-end pb-2">
                <p className="text-[10.5px] leading-relaxed text-muted/60">
                  协调者不直接完成任务，只负责选择下一位发言者、派发指令和决定何时结束。
                </p>
              </div>
            </div>

            <div className="flex items-center justify-between border-t border-[var(--sb-border)] px-5 py-3">
              {error ? <span className="text-[11px] text-danger">{error}</span> : <span />}
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => setEditorOpen(false)}
                  className="rounded-lg border border-[var(--sb-border)] px-3 py-2 text-[11.5px] text-muted hover:text-[var(--sb-text)]"
                >
                  取消
                </button>
                <button
                  type="button"
                  onClick={save}
                  disabled={saving}
                  className="flex items-center gap-1.5 rounded-lg border border-mint/40 bg-mint px-3.5 py-2 text-[11.5px] font-semibold text-ink disabled:opacity-60"
                >
                  {saving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                  {editingId ? "保存团队" : "创建团队"}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function Field({ label, children, className }: { label: string; children: React.ReactNode; className?: string }) {
  return (
    <div className={className}>
      <label className="sb-field-label">
        {label}
      </label>
      {children}
    </div>
  );
}
