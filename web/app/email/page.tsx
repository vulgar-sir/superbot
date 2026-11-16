"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ArrowLeft, Loader2, Mail, Paperclip, Plus, RefreshCw, Send, Trash2 } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import type { EmailDraft, EmailThread, User } from "@/lib/types";
import { Rail } from "@/components/Rail";
import { useToast } from "@/components/ui/Toast";
import { useLanguage } from "@/components/LanguageProvider";
import { cn, timeAgo } from "@/lib/format";

type Compose = { to: string; cc: string; subject: string; body: string };

const emptyCompose: Compose = { to: "", cc: "", subject: "", body: "" };

export default function EmailPage() {
  const router = useRouter();
  const { t } = useLanguage();
  const { toast } = useToast();
  const [user, setUser] = useState<User | null>(null);
  const [status, setStatus] = useState<{ configured: boolean; from?: string; imap?: boolean; smtp?: boolean } | null>(null);
  const [threads, setThreads] = useState<EmailThread[]>([]);
  const [drafts, setDrafts] = useState<EmailDraft[]>([]);
  const [tab, setTab] = useState<"inbox" | "drafts">("inbox");
  const [selectedThread, setSelectedThread] = useState<EmailThread | null>(null);
  const [compose, setCompose] = useState<Compose>(emptyCompose);
  const [editingDraftID, setEditingDraftID] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [sending, setSending] = useState(false);
  const [savingDraft, setSavingDraft] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [me, emailStatus] = await Promise.all([api.me(), api.emailStatus()]);
      setUser(me.user);
      setStatus(emailStatus);
      const [inbox, draftsResponse] = await Promise.all([
        emailStatus.imap ? api.emailInbox(20).catch(() => ({ messages: [], threads: [] })) : Promise.resolve({ messages: [], threads: [] }),
        api.listEmailDrafts().catch(() => ({ drafts: [] })),
      ]);
      setThreads(inbox.threads);
      setDrafts(draftsResponse.drafts);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        router.replace("/login");
        return;
      }
      setError(err instanceof Error ? err.message : "加载邮件失败");
    } finally {
      setLoading(false);
    }
  }, [router]);

  useEffect(() => {
    void load();
  }, [load]);

  const refreshInbox = async () => {
    setRefreshing(true);
    try {
      const inbox = await api.emailInbox(20);
      setThreads(inbox.threads);
      setError(null);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "刷新收件箱失败");
    } finally {
      setRefreshing(false);
    }
  };

  const send = async () => {
    if (!compose.to.trim()) return;
    setSending(true);
    setError(null);
    try {
      await api.sendEmail(compose);
      toast("邮件已发送", "success");
      if (editingDraftID) {
        await api.deleteEmailDraft(editingDraftID).catch(() => undefined);
        setDrafts((prev) => prev.filter((item) => item.id !== editingDraftID));
        setEditingDraftID(null);
      }
      setCompose(emptyCompose);
      await refreshInbox();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "发送邮件失败");
    } finally {
      setSending(false);
    }
  };

  const saveDraft = async () => {
    setSavingDraft(true);
    try {
      if (editingDraftID) {
        const result = await api.updateEmailDraft(editingDraftID, compose);
        setDrafts((prev) => prev.map((item) => (item.id === editingDraftID ? result.draft : item)));
      } else {
        const result = await api.createEmailDraft(compose);
        setDrafts((prev) => [result.draft, ...prev]);
        setEditingDraftID(result.draft.id);
      }
      toast("草稿已保存", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : t("email.saveDraftFailed"), "danger");
    } finally {
      setSavingDraft(false);
    }
  };

  const openDraft = (draft: EmailDraft) => {
    setEditingDraftID(draft.id);
    setCompose({ to: draft.to, cc: draft.cc, subject: draft.subject, body: draft.body });
    setSelectedThread(null);
    setTab("drafts");
  };

  const removeDraft = async (draft: EmailDraft) => {
    try {
      await api.deleteEmailDraft(draft.id);
      setDrafts((prev) => prev.filter((item) => item.id !== draft.id));
      if (editingDraftID === draft.id) {
        setEditingDraftID(null);
        setCompose(emptyCompose);
      }
      toast("草稿已删除", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "删除草稿失败", "danger");
    }
  };

  if (!user) {
    return <div className="flex h-screen items-center justify-center"><Loader2 className="h-5 w-5 animate-spin" /></div>;
  }

  return (
    <div className="flex h-screen w-full overflow-hidden bg-[var(--sb-bg)]">
      <Rail user={user} onLogout={() => void api.logout().finally(() => router.replace("/login"))} />
      <main className="flex min-w-0 flex-1 flex-col pb-16 md:pb-0">
        <header className="sb-page-header flex h-[61px] shrink-0 items-center justify-between gap-3 border-b border-[var(--sb-border)] px-6">
          <div className="flex items-center gap-3">
            <Link href="/" className="sb-icon-btn md:hidden" aria-label="返回会话">
              <ArrowLeft className="h-4 w-4" />
            </Link>
            <Mail className="h-4 w-4 text-[var(--sb-text-2)]" />
            <h1 className="text-[17px] font-semibold">{t("email.title")}</h1>
            {status?.configured ? (
              <span className="rounded-full bg-[var(--sb-hover)] px-2 py-0.5 text-[11px] text-[var(--sb-text-2)]">
                {status.from || "已连接"}
              </span>
            ) : (
              <span className="rounded-full bg-[var(--sb-warn-soft)] px-2 py-0.5 text-[11px] text-[var(--sb-warn)]">
                {t("email.notConfigured")}
              </span>
            )}
          </div>
          <div className="flex items-center gap-2">
            <button type="button" onClick={() => { setEditingDraftID(null); setCompose(emptyCompose); setSelectedThread(null); }} className="sb-btn" data-variant="secondary" data-size="sm">
              <Plus className="h-3.5 w-3.5" />
              {t("email.compose")}
            </button>
            <button type="button" onClick={() => void refreshInbox()} disabled={refreshing || !status?.imap} className="sb-icon-btn" aria-label="刷新收件箱">
              {refreshing ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
            </button>
          </div>
        </header>

        {!status?.configured && (
          <div className="mx-6 mt-4 rounded-xl border border-[var(--sb-warn)]/25 bg-[var(--sb-warn-soft)] px-4 py-3 text-[12px] text-[var(--sb-warn)]">
            {t("email.configHint")}
          </div>
        )}
        {error && (
          <div className="mx-6 mt-4 rounded-xl bg-[var(--sb-danger-soft)] px-4 py-3 text-[12px] text-[var(--sb-danger)]">
            {error}
          </div>
        )}

        <div className="flex min-h-0 flex-1">
          <aside className="hidden w-[320px] shrink-0 flex-col border-r border-[var(--sb-border)] sm:flex">
            <div className="flex items-center gap-1 px-3 py-2">
              {(["inbox", "drafts"] as const).map((key) => (
                <button
                  key={key}
                  type="button"
                  onClick={() => setTab(key)}
                  className={cn(
                    "h-7 rounded-full px-3 text-[12px]",
                    tab === key ? "bg-[var(--sb-text)] text-[var(--sb-pane)]" : "text-[var(--sb-text-2)] hover:bg-[var(--sb-hover)]",
                  )}
                >
                  {key === "inbox" ? `${t("email.inbox")} ${threads.length}` : `${t("email.drafts")} ${drafts.length}`}
                </button>
              ))}
            </div>
            <div className="flex-1 overflow-y-auto px-2 pb-3">
              {loading ? (
                <div className="flex justify-center py-10"><Loader2 className="h-4 w-4 animate-spin" /></div>
              ) : tab === "inbox" ? (
                threads.length === 0 ? (
                  <div className="px-3 py-10 text-center text-[12px] text-[var(--sb-text-3)]">{t("email.emptyInbox")}</div>
                ) : (
                  threads.map((thread) => (
                    <button
                      key={thread.id}
                      type="button"
                      onClick={() => setSelectedThread(thread)}
                      className={cn(
                        "w-full rounded-xl px-3 py-2.5 text-left",
                        selectedThread?.id === thread.id ? "bg-[var(--sb-hover)]" : "hover:bg-[var(--sb-hover)]",
                      )}
                    >
                      <span className="block truncate text-[13px]">{thread.subject || "(无主题)"}</span>
                      <span className="mt-0.5 block truncate text-[11px] text-[var(--sb-text-3)]">
                        {thread.messages.at(-1)?.from} · {thread.messages.length} 封 · {timeAgo(thread.messages.at(-1)?.date ?? "")}
                      </span>
                    </button>
                  ))
                )
              ) : drafts.length === 0 ? (
                <div className="px-3 py-10 text-center text-[12px] text-[var(--sb-text-3)]">没有草稿。</div>
              ) : (
                drafts.map((draft) => (
                  <div key={draft.id} className="group flex items-center gap-1 rounded-xl px-2 py-2 hover:bg-[var(--sb-hover)]">
                    <button type="button" onClick={() => openDraft(draft)} className="min-w-0 flex-1 text-left">
                      <span className="block truncate text-[13px]">{draft.subject || "(无主题)"}</span>
                      <span className="block truncate text-[11px] text-[var(--sb-text-3)]">{draft.to || "未填收件人"} · {timeAgo(draft.updated_at)}</span>
                    </button>
                    <button type="button" onClick={() => void removeDraft(draft)} className="text-[var(--sb-text-4)] hover:text-[var(--sb-danger)]" aria-label="删除草稿">
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                ))
              )}
            </div>
          </aside>

          <section className="min-w-0 flex-1 overflow-y-auto px-6 py-5">
            <div className="mx-auto max-w-[900px] space-y-4">
              <section className="rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-4">
                <div className="grid gap-2 sm:grid-cols-2">
                  <input value={compose.to} onChange={(event) => setCompose((prev) => ({ ...prev, to: event.target.value }))} placeholder="收件人（逗号分隔）" className="field" />
                  <input value={compose.cc} onChange={(event) => setCompose((prev) => ({ ...prev, cc: event.target.value }))} placeholder="抄送（可选）" className="field" />
                  <input value={compose.subject} onChange={(event) => setCompose((prev) => ({ ...prev, subject: event.target.value }))} placeholder="主题" className="field sm:col-span-2" />
                </div>
                <textarea
                  value={compose.body}
                  onChange={(event) => setCompose((prev) => ({ ...prev, body: event.target.value }))}
                  placeholder="正文…"
                  className="field mt-2 min-h-[180px] w-full resize-y text-[13px] leading-6"
                />
                <div className="mt-2 flex items-center justify-end gap-2">
                  <button type="button" onClick={() => void saveDraft()} disabled={savingDraft} className="sb-btn" data-variant="secondary" data-size="sm">
                    {savingDraft ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
                    {t("email.saveDraft")}
                  </button>
                  <button type="button" onClick={() => void send()} disabled={sending || !compose.to.trim() || !status?.smtp} className="sb-btn" data-variant="primary" data-size="sm">
                    {sending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Send className="h-3.5 w-3.5" />}
                    {t("email.send")}
                  </button>
                </div>
              </section>

              {selectedThread && (
                <section className="space-y-2">
                  {selectedThread.messages.map((message) => (
                    <article key={message.uid} className="rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-4">
                      <div className="flex flex-wrap items-center gap-2 text-[12px] text-[var(--sb-text-2)]">
                        <span className="font-medium text-[var(--sb-text)]">{message.from}</span>
                        <span>{message.date ? new Date(message.date).toLocaleString("zh-CN") : ""}</span>
                      </div>
                      <div className="mt-1 text-[13px] font-medium">{message.subject}</div>
                      <pre className="mt-2 whitespace-pre-wrap text-[12.5px] leading-[20px] text-[var(--sb-text-2)]">{message.body}</pre>
                      {message.attachments && message.attachments.length > 0 && (
                        <div className="mt-2 flex flex-wrap gap-2">
                          {message.attachments.map((attachment) => (
                            <span key={`${message.uid}-${attachment.filename}`} className="inline-flex items-center gap-1 rounded-lg bg-[var(--sb-hover)] px-2 py-1 text-[11px] text-[var(--sb-text-2)]">
                              <Paperclip className="h-3 w-3" />
                              {attachment.filename} · {attachment.mime_type} · {attachment.size} B
                            </span>
                          ))}
                        </div>
                      )}
                    </article>
                  ))}
                </section>
              )}
            </div>
          </section>
        </div>
      </main>
    </div>
  );
}
