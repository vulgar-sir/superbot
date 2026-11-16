"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  ArrowLeft,
  Bot as BotIcon,
  Cpu,
  Loader2,
  Pencil,
  Plus,
  Sparkles,
  Trash2,
  X,
} from "lucide-react";

import { api, ApiError } from "@/lib/api";
import type { Bot, Model, Provider, User } from "@/lib/types";
import { Rail } from "@/components/Rail";
import { useLanguage } from "@/components/LanguageProvider";
import { BotAvatar } from "@/components/BotAvatar";
import { NewBotDialog } from "@/components/NewBotDialog";
import { StatusPill } from "@/components/StatusPill";
import { useConfirm } from "@/components/ui/Confirm";
import { cn, formatTime } from "@/lib/format";

type FormState = {
  name: string;
  description: string;
  system_prompt: string;
  provider_id: string;
  model: string;
  computer_profile: "none" | "browser" | "desktop";
  memory_enabled: boolean;
};

const emptyForm: FormState = {
  name: "",
  description: "",
  system_prompt: "",
  provider_id: "",
  model: "",
  computer_profile: "none",
  memory_enabled: true,
};

export default function BotsPage() {
  const { t } = useLanguage();
  const router = useRouter();
  const { confirm, confirmElement } = useConfirm();
  const [user, setUser] = useState<User | null>(null);
  const [bots, setBots] = useState<Bot[]>([]);
  const [providers, setProviders] = useState<Provider[]>([]);
  const [models, setModels] = useState<Record<string, Model[]>>({});
  const [booting, setBooting] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [newBotOpen, setNewBotOpen] = useState(false);
  const [creatingBot, setCreatingBot] = useState(false);
  const [newBotError, setNewBotError] = useState<string | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [form, setForm] = useState<FormState>(emptyForm);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const me = await api.me();
        const [botsResponse, providersResponse] = await Promise.all([
          api.listBots(),
          api.listProviders(),
        ]);
        if (cancelled) return;
        setUser(me.user);
        setBots(botsResponse.bots);
        const llmProviders = providersResponse.providers.filter((provider) => provider.type === "llm");
        setProviders(llmProviders);
        const modelEntries = await Promise.all(
          llmProviders.map(async (provider) => [provider.id, (await api.listModels(provider.id)).models] as const),
        );
        if (cancelled) return;
        setModels(Object.fromEntries(modelEntries));
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


  const openCreate = useCallback(() => {
    setNewBotError(null);
    setNewBotOpen(true);
  }, []);

  const openEdit = useCallback((bot: Bot) => {
    setEditingId(bot.id);
    setForm({
      name: bot.name,
      description: bot.description,
      system_prompt: bot.system_prompt,
      provider_id: bot.provider_id ?? "",
      model: bot.model,
      computer_profile: bot.computer_profile,
      memory_enabled: bot.memory_enabled,
    });
    setError(null);
    setEditorOpen(true);
  }, []);

  const save = useCallback(async () => {
    if (!form.name.trim() || !form.model.trim()) {
      setError("名称和模型必填");
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const payload = {
        name: form.name.trim(),
        description: form.description.trim(),
        system_prompt: form.system_prompt,
        provider_id: form.provider_id || null,
        model: form.model.trim(),
        computer_profile: form.computer_profile,
        memory_enabled: form.memory_enabled,
      };
      if (editingId) {
        const result = await api.updateBot(editingId, payload);
        setBots((prev) => prev.map((bot) => (bot.id === editingId ? result.bot : bot)));
      } else {
        const result = await api.createBot(payload);
        setBots((prev) => [...prev, result.bot]);
      }
      setEditorOpen(false);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "保存失败");
    } finally {
      setSaving(false);
    }
  }, [editingId, form]);

  const remove = useCallback(async (bot: Bot) => {
    const ok = await confirm({
      title: `删除 Bot「${bot.name}」？`,
      description: "已有运行记录的 Bot 不能删除；其他 Bot 的私聊、资料库与例程会一并删除。",
      confirmLabel: "删除",
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteBot(bot.id);
      setBots((prev) => prev.filter((item) => item.id !== bot.id));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "删除失败");
    }
  }, [confirm]);

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } finally {
      router.replace("/login");
    }
  }, [router]);

  const providerName = useMemo(
    () => (id: string | null) => providers.find((provider) => provider.id === id)?.name ?? "未绑定 Provider",
    [providers],
  );

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
              <h1 className="text-[15px] font-semibold">Bot</h1>
            </div>
            <StatusPill tone="idle">{t("bots.count", { n: bots.length })}</StatusPill>
          </div>
          <button
            type="button"
            onClick={openCreate}
            className="sb-btn" data-variant="primary" data-size="sm"
          >
            <Plus className="h-3.5 w-3.5" />
            {t("sidebar.newBot")}
          </button>
        </header>

        <div className="flex-1 overflow-y-auto px-6 py-6">
          {providers.length === 0 && (
            <div className="mb-5 flex items-center justify-between rounded-xl border border-amber/25 bg-amber/10 px-4 py-3">
              <div className="text-[12px] text-amber">
                还没有 LLM Provider。先到设置里添加一个，Bot 才能运行。
              </div>
              <Link href="/settings" className="text-[11.5px] font-medium text-amber underline">
                去设置
              </Link>
            </div>
          )}
          {error && (
            <div className="mb-5 rounded-xl border border-danger/25 bg-danger/10 px-4 py-3 text-[12px] text-danger">
              {error}
            </div>
          )}

          {bots.length === 0 ? (
            <div className="flex min-h-[420px] flex-col items-center justify-center rounded-2xl border border-dashed border-[var(--sb-border)] bg-[var(--sb-pane)]">
              <div className="flex h-14 w-14 items-center justify-center rounded-2xl border bg-[var(--sb-hover)]">
                <BotIcon className="h-6 w-6 text-[var(--sb-text-2)]" />
              </div>
              <h2 className="mt-4 text-[17px] font-medium text-[var(--sb-text)]">{t("bots.empty")}</h2>
              <p className="mt-1.5 max-w-sm text-center text-[12px] text-muted">
                给 Bot 一个名字、一段人设和一个模型。它可以读写自己的工作区，之后还能接入 Skill 和 MCP。
              </p>
              <button
                type="button"
                onClick={openCreate}
                className="mt-5 inline-flex items-center gap-1.5 rounded-lg border border-mint/40 bg-mint px-3.5 py-2 text-[12px] font-semibold text-ink"
              >
                <Plus className="h-3.5 w-3.5" />
                创建第一个 Bot
              </button>
            </div>
          ) : (
            <div className="grid gap-4 md:grid-cols-2 2xl:grid-cols-3">
              {bots.map((bot) => (
                <article
                  key={bot.id}
                  className="group flex flex-col rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-5 transition hover:border-[var(--sb-border-strong)] hover:bg-[var(--sb-pane)]"
                >
                  <div className="flex items-start gap-3">
                    <BotAvatar name={bot.name} id={bot.id} size={44} avatar={bot.avatar} />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <h2 className="truncate text-[14px] font-medium text-[var(--sb-text)]">{bot.name}</h2>
                        {bot.computer_profile !== "none" && (
                          <span className="rounded-full border border-[var(--sb-border)] px-1.5 py-0.5 font-mono text-[8.5px] uppercase tracking-[0.12em] text-muted">
                            {bot.computer_profile}
                          </span>
                        )}
                      </div>
                      <div className="mt-1 flex items-center gap-1.5 font-mono text-[9.5px] text-muted/70">
                        <Cpu className="h-2.5 w-2.5" />
                        {bot.model}
                        <span className="text-muted/30">·</span>
                        {providerName(bot.provider_id)}
                      </div>
                    </div>
                  </div>
                  <p className="mt-3 line-clamp-2 min-h-[32px] text-[11.5px] leading-relaxed text-muted">
                    {bot.description || "未填写职责描述。"}
                  </p>
                  <div className="mt-3 rounded-lg border border-[var(--sb-border)] bg-[var(--sb-hover)] px-2.5 py-2">
                    <div className="font-mono text-[8.5px] uppercase tracking-[0.14em] text-muted/50">{t("bots.systemPrompt")}</div>
                    <div className="mt-1 line-clamp-2 text-[10.5px] leading-relaxed text-muted/75">
                      {bot.system_prompt || "使用平台默认协作规则。"}
                    </div>
                  </div>
                  <div className="mt-4 flex items-center justify-between border-t border-[var(--sb-border)] pt-3">
                    <span className="font-mono text-[9px] text-muted/45">{t("bots.updated", { time: formatTime(bot.updated_at) })}</span>
                    <div className="flex items-center gap-1">
                      <button
                        type="button"
                        onClick={() => openEdit(bot)}
                        className="flex h-7 w-7 items-center justify-center rounded-md text-muted transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-text-2)]"
                        aria-label={t("common.edit")}
                      >
                        <Pencil className="h-3.5 w-3.5" />
                      </button>
                      <button
                        type="button"
                        onClick={() => remove(bot)}
                        className="flex h-7 w-7 items-center justify-center rounded-md text-muted transition hover:bg-danger/10 hover:text-danger"
                        aria-label={t("common.delete")}
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

      <NewBotDialog
        open={newBotOpen}
        creating={creatingBot}
        error={newBotError}
        onClose={() => setNewBotOpen(false)}
        onCreate={async (input) => {
          setCreatingBot(true);
          setNewBotError(null);
          try {
            const result = await api.createBot(input);
            setBots((prev) => [...prev, result.bot]);
            setNewBotOpen(false);
          } catch (err) {
            setNewBotError(err instanceof ApiError ? err.message : "创建 Bot 失败");
          } finally {
            setCreatingBot(false);
          }
        }}
      />

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
                <h2 className="mt-1 text-[17px] font-medium text-[var(--sb-text)]">
                  {editingId ? "编辑 Bot" : "创建一个 Bot"}
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
              <Field label={t("bots.name")}>
                <input
                  className="field"
                  value={form.name}
                  onChange={(event) => setForm((prev) => ({ ...prev, name: event.target.value }))}
                  placeholder={t("bots.namePlaceholder")}
                />
              </Field>
              <Field label={t("bots.computerProfile")}>
                <select
                  className="field"
                  value={form.computer_profile}
                  onChange={(event) =>
                    setForm((prev) => ({
                      ...prev,
                      computer_profile: event.target.value as FormState["computer_profile"],
                    }))
                  }
                >
                  <option value="none">{t("bots.profileNone")}</option>
                  <option value="browser">{t("bots.profileBrowser")}</option>
                  <option value="desktop">{t("bots.profileDesktop")}</option>
                </select>
              </Field>
              <Field label="Provider">
                <select
                  className="field"
                  value={form.provider_id}
                  onChange={(event) => {
                    const providerId = event.target.value;
                    const firstModel = models[providerId]?.[0]?.name ?? "";
                    setForm((prev) => ({ ...prev, provider_id: providerId, model: firstModel }));
                  }}
                >
                  <option value="">{t("bots.unbound")}</option>
                  {providers.map((provider) => (
                    <option key={provider.id} value={provider.id}>
                      {provider.name} · {provider.kind}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label={t("bots.model")}>
                {models[form.provider_id]?.length ? (
                  <select
                    className="field"
                    value={form.model}
                    onChange={(event) => setForm((prev) => ({ ...prev, model: event.target.value }))}
                  >
                    {models[form.provider_id].map((model) => (
                      <option key={model.id} value={model.name}>
                        {model.name}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input
                    className="field"
                    value={form.model}
                    onChange={(event) => setForm((prev) => ({ ...prev, model: event.target.value }))}
                    placeholder="deepseek-flash"
                  />
                )}
              </Field>
              <Field label={t("bots.description")} className="md:col-span-2">
                <input
                  className="field"
                  value={form.description}
                  onChange={(event) => setForm((prev) => ({ ...prev, description: event.target.value }))}
                  placeholder={t("bots.descriptionPlaceholder")}
                />
              </Field>
              <Field label={t("bots.systemPrompt")} className="md:col-span-2">
                <textarea
                  className="field min-h-[140px]"
                  value={form.system_prompt}
                  onChange={(event) => setForm((prev) => ({ ...prev, system_prompt: event.target.value }))}
                  placeholder={t("bots.systemPromptPlaceholder")}
                />
              </Field>
              <label className="flex items-center gap-2 text-[12px] text-muted md:col-span-2">
                <input
                  type="checkbox"
                  checked={form.memory_enabled}
                  onChange={(event) => setForm((prev) => ({ ...prev, memory_enabled: event.target.checked }))}
                  className="h-3.5 w-3.5 accent-[var(--color-mint)]"
                />
                启用长期记忆（M2 生效）
              </label>
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
                  className={cn(
                    "flex items-center gap-1.5 rounded-lg border border-mint/40 bg-mint px-3.5 py-2 text-[11.5px] font-semibold text-ink transition hover:brightness-110",
                    saving && "opacity-60",
                  )}
                >
                  {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Sparkles className="h-3.5 w-3.5" />}
                  {editingId ? "保存修改" : "创建 Bot"}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function Field({
  label,
  children,
  className,
}: {
  label: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={className}>
      <label className="sb-field-label">
        {label}
      </label>
      {children}
    </div>
  );
}
