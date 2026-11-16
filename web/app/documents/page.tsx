"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { FilePlus2, FileText, History, Loader2, Save, Trash2, Users } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import type { DocumentItem, DocumentVersion, User } from "@/lib/types";
import { Rail } from "@/components/Rail";
import { CanvasEditor } from "@/components/CanvasEditor";
import { Modal } from "@/components/ui/Modal";
import { mergeDocumentContent } from "@/lib/merge3";
import { mergeCanvasContent } from "@/lib/mergeCanvas";
import { useToast } from "@/components/ui/Toast";
import { useLanguage } from "@/components/LanguageProvider";
import { cn, timeAgo } from "@/lib/format";
import * as Y from "yjs";

export default function DocumentsPage() {
  const router = useRouter();
  const { t } = useLanguage();
  const { toast } = useToast();
  const [user, setUser] = useState<User | null>(null);
  const [documents, setDocuments] = useState<DocumentItem[]>([]);
  const [activeID, setActiveID] = useState("");
  const [title, setTitle] = useState("");
  const [content, setContent] = useState("");
  const [kind, setKind] = useState<"document" | "canvas">("document");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [versions, setVersions] = useState<DocumentVersion[]>([]);
  const [versionsLoading, setVersionsLoading] = useState(false);
  const [accessOpen, setAccessOpen] = useState(false);
  const [accessLoading, setAccessLoading] = useState(false);
  const [accessMembers, setAccessMembers] = useState<Array<{ id: string; email: string; display_name: string; role: string }>>([]);
  const [accessACL, setAccessACL] = useState<Array<{ user_id: string; role: string }>>([]);
  const [collabCount, setCollabCount] = useState(1);
  const [remoteCursors, setRemoteCursors] = useState<Array<{ id: string; x: number; y: number; actor?: string; at: number }>>([]);
  const clientID = useRef("");
  const lastCursorSent = useRef(0);
  const baseContent = useRef("");
  const ydocRef = useRef<Y.Doc | null>(null);
  const ytextRef = useRef<Y.Text | null>(null);
  const applyingRemote = useRef(false);
  const kindRef = useRef<"document" | "canvas">("document");

  const encodeUpdate = (update: Uint8Array) => {
    let binary = "";
    for (const byte of update) binary += String.fromCharCode(byte);
    return btoa(binary);
  };

  const decodeUpdate = (value: string) => {
    const binary = atob(value);
    const bytes = new Uint8Array(binary.length);
    for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index);
    return bytes;
  };

  const resetYDoc = useCallback((text: string) => {
    const doc = new Y.Doc();
    const ytext = doc.getText("content");
    if (text) ytext.insert(0, text);
    ydocRef.current = doc;
    ytextRef.current = ytext;
  }, []);

  const loadCRDT = useCallback(
    async (documentID: string, text: string) => {
      resetYDoc("");
      const doc = ydocRef.current;
      const ytext = ytextRef.current;
      if (!doc || !ytext) return;
      try {
        const result = await api.listDocumentCRDT(documentID);
        if (result.updates.length === 0) {
          if (text) ytext.insert(0, text);
          const seed = await api.seedDocumentCRDT(
            documentID,
            encodeUpdate(Y.encodeStateAsUpdate(doc)),
          );
          resetYDoc("");
          const seededDoc = ydocRef.current;
          if (!seededDoc) return;
          for (const update of seed.updates) {
            Y.applyUpdate(seededDoc, decodeUpdate(update));
          }
        } else {
          for (const update of result.updates) {
            Y.applyUpdate(doc, decodeUpdate(update));
          }
        }
        const merged = ydocRef.current?.getText("content").toString() ?? "";
        if (merged === text) return;
        applyingRemote.current = true;
        setContent(merged);
        baseContent.current = merged;
        window.requestAnimationFrame(() => {
          applyingRemote.current = false;
        });
      } catch {
        // CRDT is optional; fall back to the stored content.
      }
    },
    [resetYDoc],
  );

  const applyLocalEdit = useCallback(
    (value: string) => {
      if (kindRef.current !== "document" || applyingRemote.current) return;
      const doc = ydocRef.current;
      const ytext = ytextRef.current;
      if (!doc || !ytext) return;
      const previous = ytext.toString();
      if (previous === value) return;
      let start = 0;
      while (start < previous.length && start < value.length && previous[start] === value[start]) {
        start += 1;
      }
      let previousEnd = previous.length;
      let nextEnd = value.length;
      while (
        previousEnd > start &&
        nextEnd > start &&
        previous[previousEnd - 1] === value[nextEnd - 1]
      ) {
        previousEnd -= 1;
        nextEnd -= 1;
      }
      ytext.delete(start, previousEnd - start);
      if (nextEnd > start) ytext.insert(start, value.slice(start, nextEnd));
      void api
        .pushDocumentCRDT(activeID, encodeUpdate(Y.encodeStateAsUpdate(doc)), clientID.current)
        .catch(() => undefined);
    },
    [activeID],
  );

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [me, result] = await Promise.all([api.me(), api.listDocuments()]);
      setUser(me.user);
      setDocuments(result.documents);
      if (result.documents[0]) {
        setActiveID(result.documents[0].id);
        setTitle(result.documents[0].title);
        setContent(result.documents[0].content);
        setKind(result.documents[0].kind);
        kindRef.current = result.documents[0].kind;
        baseContent.current = result.documents[0].content;
        void loadCRDT(result.documents[0].id, result.documents[0].content);
      }
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) router.replace("/login");
      else toast(err instanceof ApiError ? err.message : "读取文档失败", "danger");
    } finally {
      setLoading(false);
    }
  }, [router, toast]);

  useEffect(() => {
    clientID.current =
      typeof crypto !== "undefined" && typeof crypto.randomUUID === "function"
        ? crypto.randomUUID()
        : `client-${Date.now()}-${Math.random().toString(36).slice(2)}`;
    void load();
  }, [load]);

  useEffect(() => {
    if (!activeID || !clientID.current) return;
    const source = new EventSource(`/api/documents/${activeID}/events?client_id=${encodeURIComponent(clientID.current)}`);
    source.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data) as {
          type: string;
          count?: number;
          client_id?: string;
          document?: DocumentItem;
          actor?: string;
          update?: string;
          x?: number;
          y?: number;
          cursors?: Array<{ id: string; x: number; y: number; actor?: string }>;
        };
        if (typeof data.count === "number") setCollabCount(data.count);
        if (data.type === "presence_state" && Array.isArray(data.cursors)) {
          setCollabCount(data.count ?? data.cursors.length);
          setRemoteCursors(
            data.cursors
              .filter((cursor) => cursor.id !== clientID.current)
              .map((cursor) => ({ ...cursor, at: Date.now() })),
          );
        }
        if (data.type === "cursor" && data.client_id !== clientID.current && typeof data.x === "number" && typeof data.y === "number") {
          setRemoteCursors((prev) => [
            ...prev.filter((cursor) => cursor.id !== data.client_id),
            { id: data.client_id!, x: data.x!, y: data.y!, actor: data.actor, at: Date.now() },
          ]);
        }
        if (data.type === "crdt" && data.update && data.client_id !== clientID.current) {
          const doc = ydocRef.current;
          const ytext = ytextRef.current;
          if (doc && ytext) {
            Y.applyUpdate(doc, decodeUpdate(data.update));
            const merged = ytext.toString();
            applyingRemote.current = true;
            setContent(merged);
            baseContent.current = merged;
            window.requestAnimationFrame(() => {
              applyingRemote.current = false;
            });
          }
        }
        if (data.type === "update" && data.document && data.client_id !== clientID.current && data.document.id === activeID) {
          setDocuments((prev) => prev.map((doc) => (doc.id === activeID ? data.document! : doc)));
          setTitle(data.document.title);
          if (data.document.kind !== "document" || !ydocRef.current) {
            setContent(data.document.content);
            setKind(data.document.kind);
            baseContent.current = data.document.content;
          } else {
            const doc = ydocRef.current;
            const ytext = ytextRef.current;
            if (doc && ytext) {
              applyingRemote.current = true;
              ytext.delete(0, ytext.length);
              if (data.document.content) ytext.insert(0, data.document.content);
              setContent(data.document.content);
              setKind(data.document.kind);
              baseContent.current = data.document.content;
              window.requestAnimationFrame(() => {
                applyingRemote.current = false;
              });
              void api
                .pushDocumentCRDT(activeID, encodeUpdate(Y.encodeStateAsUpdate(doc)), clientID.current)
                .catch(() => undefined);
            }
          }
        }
      } catch {
        // ignore malformed events
      }
    };
    const timer = window.setInterval(() => {
      setRemoteCursors((prev) => prev.filter((cursor) => Date.now() - cursor.at < 8000));
    }, 3000);
    return () => {
      source.close();
      window.clearInterval(timer);
    };
  }, [activeID, resetYDoc]);

  const openDocument = (doc: DocumentItem) => {
    setActiveID(doc.id);
    setTitle(doc.title);
    setContent(doc.content);
    setKind(doc.kind);
    kindRef.current = doc.kind;
    baseContent.current = doc.content;
    void loadCRDT(doc.id, doc.content);
  };

  const createDocument = async () => {
    try {
      const result = await api.createDocument({ title: t("docs.defaultTitle"), content: "", kind: "document" });
      setDocuments((prev) => [result.document, ...prev]);
      openDocument(result.document);
      toast("文档已创建", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "创建文档失败", "danger");
    }
  };

  const openHistory = async () => {
    if (!activeID) return;
    setHistoryOpen(true);
    setVersionsLoading(true);
    try {
      const result = await api.listDocumentVersions(activeID);
      setVersions(result.versions);
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "读取历史版本失败", "danger");
    } finally {
      setVersionsLoading(false);
    }
  };

  const restore = async (version: DocumentVersion) => {
    if (!activeID) return;
    try {
      const result = await api.updateDocument(activeID, {
        title: version.title, content: version.content, kind: version.kind,
      });
      setDocuments((prev) => prev.map((doc) => (doc.id === activeID ? result.document : doc)));
      setTitle(result.document.title);
      setContent(result.document.content);
      setKind(result.document.kind);
      setHistoryOpen(false);
      toast("已恢复历史版本", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "恢复版本失败", "danger");
    }
  };

  const reportCursor = (x: number, y: number) => {
    if (!activeID || !clientID.current) return;
    const now = Date.now();
    if (now - lastCursorSent.current < 120) return;
    lastCursorSent.current = now;
    void api.postDocumentCursor(activeID, { client_id: clientID.current, x, y }).catch(() => undefined);
  };

  const openAccess = async () => {
    if (!activeID) return;
    setAccessOpen(true);
    setAccessLoading(true);
    setAccessACL([]);
    try {
      const accounts = await api.listAccounts();
      const active = accounts.accounts.find((account) => account.active);
      if (!active) return;
      const [members, acl] = await Promise.all([
        api.listAccountMembers(active.id),
        api.listDocumentACL(activeID),
      ]);
      setAccessMembers(members.members);
      setAccessACL(acl.acl);
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "读取文档权限失败", "danger");
    } finally {
      setAccessLoading(false);
    }
  };

  const updateAccessRole = async (userId: string, role: string) => {
    if (!activeID) return;
    try {
      if (role === "none") {
        await api.deleteDocumentACL(activeID, userId);
        setAccessACL((prev) => prev.filter((row) => row.user_id !== userId));
      } else {
        await api.setDocumentACL(activeID, userId, role);
        setAccessACL((prev) => [...prev.filter((row) => row.user_id !== userId), { user_id: userId, role }]);
      }
      toast("文档访问权限已更新", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "更新文档权限失败", "danger");
    }
  };

  const save = async () => {
    if (!activeID) return;
    setSaving(true);
    try {
      const current = documents.find((doc) => doc.id === activeID);
      const result = await api.collabDocument(activeID, {
        title, content, kind, client_id: clientID.current, base_revision: current?.revision ?? 0,
      });
      setDocuments((prev) => prev.map((doc) => (doc.id === activeID ? result.document : doc)));
      baseContent.current = result.document.content;
      toast("文档已保存", "success");
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        try {
          const latest = await api.getDocument(activeID);
          const canvasMerged = kind === "canvas" || content.trim().startsWith("{");
          const merged = (canvasMerged ? mergeCanvasContent(baseContent.current, content, latest.document.content) : null)
            ?? mergeDocumentContent(baseContent.current, content, latest.document.content);
          setDocuments((prev) => prev.map((doc) => (doc.id === activeID ? latest.document : doc)));
          setTitle(latest.document.title);
          setContent(merged.text);
          setKind(latest.document.kind);
          baseContent.current = latest.document.content;
          toast(
            merged.conflicts > 0
              ? t("docs.merged", { n: merged.conflicts })
              : t("docs.mergedClean"),
            merged.conflicts > 0 ? "danger" : "success",
          );
          return;
        } catch {
          // fall through to the generic error
        }
      }
      toast(err instanceof ApiError ? err.message : "保存文档失败", "danger");
    } finally {
      setSaving(false);
    }
  };

  const remove = async () => {
    if (!activeID) return;
    try {
      await api.deleteDocument(activeID);
      const next = documents.filter((doc) => doc.id !== activeID);
      setDocuments(next);
      if (next[0]) openDocument(next[0]);
      else {
        setActiveID("");
        setTitle("");
        setContent("");
      }
      toast("文档已删除", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "删除文档失败", "danger");
    }
  };

  if (!user) return <div className="flex h-screen items-center justify-center"><Loader2 className="h-5 w-5 animate-spin" /></div>;

  return (
    <div className="flex h-screen w-full overflow-hidden bg-[var(--sb-bg)]">
      <Rail user={user} onLogout={() => void api.logout().finally(() => router.replace("/login"))} />
      <main className="flex min-w-0 flex-1 flex-col pb-16 md:pb-0">
        <header className="sb-page-header flex h-[61px] shrink-0 items-center justify-between border-b border-[var(--sb-border)] px-6">
          <div>
            <h1 className="text-[17px] font-semibold">{t("docs.title")}</h1>
          </div>
          <button type="button" onClick={() => void createDocument()} className="sb-btn" data-variant="primary" data-size="sm">
            <FilePlus2 className="h-3.5 w-3.5" />
            {t("docs.new")}
          </button>
        </header>

        <div className="flex min-h-0 flex-1">
          <aside className="hidden w-[260px] shrink-0 overflow-y-auto border-r border-[var(--sb-border)] p-3 sm:block">
            {loading ? (
              <div className="flex justify-center py-10"><Loader2 className="h-4 w-4 animate-spin" /></div>
            ) : documents.length === 0 ? (
              <div className="px-2 py-8 text-center text-[12px] text-[var(--sb-text-3)]">{t("docs.emptyList")}</div>
            ) : (
              <div className="space-y-1">
                {documents.map((doc) => (
                  <button
                    key={doc.id}
                    type="button"
                    onClick={() => openDocument(doc)}
                    className={cn(
                      "flex w-full items-start gap-2 rounded-xl px-3 py-2.5 text-left",
                      activeID === doc.id ? "bg-[var(--sb-hover)]" : "hover:bg-[var(--sb-hover)]",
                    )}
                  >
                    <FileText className="mt-0.5 h-4 w-4 shrink-0 text-[var(--sb-text-3)]" />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-[13px]">{doc.title}</span>
                      <span className="block text-[10px] text-[var(--sb-text-3)]">{doc.kind === "canvas" ? t("docs.kindCanvas") : t("docs.kindDocument")} · {timeAgo(doc.updated_at)}</span>
                    </span>
                  </button>
                ))}
              </div>
            )}
          </aside>

          <section className="flex min-w-0 flex-1 flex-col">
            {!activeID ? (
              <div className="flex flex-1 flex-col items-center justify-center text-[13px] text-[var(--sb-text-2)]">
                <FileText className="mb-3 h-8 w-8 text-[var(--sb-text-4)]" />
                {t("docs.createOrSelect")}
              </div>
            ) : (
              <>
                <div className="flex items-center gap-2 border-b border-[var(--sb-border)] px-6 py-3">
                  <input
                    value={title}
                    onChange={(event) => setTitle(event.target.value)}
                    className="min-w-0 flex-1 bg-transparent text-[17px] font-semibold outline-none"
                  />
                  <span className="text-[10px] text-[var(--sb-text-3)]">{t("docs.collab", { n: collabCount })}</span>
                  <select value={kind} onChange={(event) => setKind(event.target.value as "document" | "canvas")} className="h-8 rounded-lg bg-[var(--sb-hover)] px-2 text-[11px] outline-none">
                    <option value="document">{t("docs.kindDocument")}</option>
                    <option value="canvas">Canvas</option>
                  </select>
                  <button type="button" onClick={() => void openAccess()} className="sb-btn" data-variant="secondary" data-size="sm">
                    <Users className="h-3.5 w-3.5" />
                    {t("docs.tabAccess")}
                  </button>
                  <button type="button" onClick={() => void openHistory()} className="sb-btn" data-variant="secondary" data-size="sm">
                    <History className="h-3.5 w-3.5" />
                    {t("docs.tabHistory")}
                  </button>
                  <button type="button" onClick={() => void save()} disabled={saving} className="sb-btn" data-variant="primary" data-size="sm">
                    {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
                    {t("docs.save")}
                  </button>
                  <button type="button" onClick={() => void remove()} className="sb-icon-btn" aria-label={t("docs.delete")}>
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
                {kind === "canvas" ? (
                  <CanvasEditor
                    key={activeID}
                    value={content}
                    onChange={setContent}
                    onCursor={reportCursor}
                    remoteCursors={remoteCursors}
                  />
                ) : (
                  <textarea
                    value={content}
                    onChange={(event) => {
                      const value = event.target.value;
                      setContent(value);
                      applyLocalEdit(value);
                    }}
                    placeholder={t("docs.placeholder")}
                    className="min-h-0 flex-1 resize-none bg-transparent px-6 py-5 font-mono text-[13px] leading-6 outline-none"
                  />
                )}
              </>
            )}
          </section>
        </div>
      </main>

      <Modal
        open={accessOpen}
        onClose={() => setAccessOpen(false)}
        title={t("docs.accessTitle")}
        subtitle={t("docs.accessSubtitle")}
        size="md"
      >
        {accessLoading ? (
          <div className="flex justify-center py-8"><Loader2 className="h-4 w-4 animate-spin" /></div>
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
                    onChange={(event) => void updateAccessRole(member.id, event.target.value)}
                    className="h-7 rounded-md bg-[var(--sb-hover)] px-1.5 text-[11px] outline-none disabled:opacity-60"
                  >
                    {accountEditor && <option value="editor">{t("docs.roleAccount")}</option>}
                    {!accountEditor && <option value="none">{t("docs.roleNone")}</option>}
                    {!accountEditor && <option value="viewer">{t("docs.roleViewer")}</option>}
                    {!accountEditor && <option value="editor">{t("docs.roleEditor")}</option>}
                    {!accountEditor && <option value="runner">{t("docs.roleRunner")}</option>}
                  </select>
                </div>
              );
            })}
          </div>
        )}
        <div className="mt-4 flex justify-end">
          <button type="button" onClick={() => setAccessOpen(false)} className="sb-btn" data-variant="secondary" data-size="sm">关闭</button>
        </div>
      </Modal>

      <Modal
        open={historyOpen}
        onClose={() => setHistoryOpen(false)}
        title={t("docs.historyTitle")}
        subtitle={t("docs.historySubtitle")}
        size="md"
      >
        {versionsLoading ? (
          <div className="flex justify-center py-8"><Loader2 className="h-4 w-4 animate-spin" /></div>
        ) : versions.length === 0 ? (
          <div className="py-8 text-center text-[12px] text-[var(--sb-text-3)]">{t("docs.historyEmpty")}</div>
        ) : (
          <div className="space-y-2">
            {versions.map((version) => (
              <div key={version.id} className="flex items-center gap-3 rounded-xl border border-[var(--sb-border)] px-3 py-2.5">
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[13px]">{version.title}</div>
                  <div className="text-[10px] text-[var(--sb-text-3)]">{version.kind === "canvas" ? t("docs.kindCanvas") : t("docs.kindDocument")} · {new Date(version.created_at).toLocaleString()}</div>
                </div>
                <button type="button" onClick={() => void restore(version)} className="sb-btn" data-variant="secondary" data-size="sm">
                  {t("docs.restore")}
                </button>
              </div>
            ))}
          </div>
        )}
        <div className="mt-4 flex justify-end">
          <button type="button" onClick={() => setHistoryOpen(false)} className="sb-btn" data-variant="secondary" data-size="sm">关闭</button>
        </div>
      </Modal>
    </div>
  );
}
