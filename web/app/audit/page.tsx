"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ArrowLeft, Loader2, ScrollText } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import type { AuditLogEntry, User } from "@/lib/types";
import { Rail } from "@/components/Rail";
import { useLanguage } from "@/components/LanguageProvider";
import { timeAgo } from "@/lib/format";

export default function AuditPage() {
  const { t } = useLanguage();
  const router = useRouter();
  const [user, setUser] = useState<User | null>(null);
  const [logs, setLogs] = useState<AuditLogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [me, result] = await Promise.all([api.me(), api.auditLogs()]);
      setUser(me.user);
      setLogs(result.logs);
      setError(null);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        router.replace("/login");
        return;
      }
      setError(err instanceof ApiError ? err.message : "加载审计日志失败");
    } finally {
      setLoading(false);
    }
  }, [router]);

  useEffect(() => {
    void load();
  }, [load]);

  if (!user) {
    return <div className="flex h-screen items-center justify-center"><Loader2 className="h-5 w-5 animate-spin" /></div>;
  }

  return (
    <div className="flex h-screen w-full overflow-hidden bg-[var(--sb-bg)]">
      <Rail user={user} onLogout={() => void api.logout().finally(() => router.replace("/login"))} />
      <main className="flex min-w-0 flex-1 flex-col pb-16 md:pb-0">
        <header className="sb-page-header flex h-[61px] shrink-0 items-center gap-3 border-b border-[var(--sb-border)] px-6">
          <Link href="/" className="sb-icon-btn md:hidden" aria-label="返回会话">
            <ArrowLeft className="h-4 w-4" />
          </Link>
          <ScrollText className="h-4 w-4 text-[var(--sb-text-2)]" />
          <h1 className="text-[17px] font-semibold">{t("audit.title")}</h1>
          <span className="rounded-full bg-[var(--sb-hover)] px-2 py-0.5 text-[11px] text-[var(--sb-text-2)]">
            {logs.length}
          </span>
        </header>

        <div className="flex-1 overflow-y-auto px-6 py-5">
          <div className="mx-auto max-w-[1000px]">
            {error && (
              <div className="mb-4 rounded-xl bg-[var(--sb-danger-soft)] px-4 py-3 text-[12px] text-[var(--sb-danger)]">
                {error}
              </div>
            )}
            {loading ? (
              <div className="flex justify-center py-16"><Loader2 className="h-5 w-5 animate-spin" /></div>
            ) : logs.length === 0 ? (
              <div className="rounded-2xl border border-dashed border-[var(--sb-border)] px-4 py-16 text-center text-[13px] text-[var(--sb-text-2)]">
                还没有审计记录。ACL 变更、工具审批、Bot 删除与保险库查看会写入这里。
              </div>
            ) : (
              <div className="overflow-hidden rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)]">
                <div className="grid grid-cols-[140px_120px_1fr_180px] gap-3 border-b border-[var(--sb-border)] px-4 py-2 text-[10.5px] uppercase tracking-[0.08em] text-[var(--sb-text-3)]">
                  <span>{t("audit.time")}</span>
                  <span>{t("audit.action")}</span>
                  <span>{t("audit.target")}</span>
                  <span>{t("audit.actor")}</span>
                </div>
                {logs.map((entry, index) => (
                  <div
                    key={entry.id}
                    className={`grid grid-cols-[140px_120px_1fr_180px] gap-3 px-4 py-3 text-[12px] ${index > 0 ? "border-t border-[var(--sb-border)]" : ""}`}
                  >
                    <span className="text-[var(--sb-text-3)]" title={new Date(entry.created_at).toLocaleString("zh-CN")}>
                      {timeAgo(entry.created_at)}
                    </span>
                    <span className="font-mono text-[11px] text-[var(--sb-text)]">{entry.action}</span>
                    <span className="min-w-0">
                      <span className="block text-[var(--sb-text-2)]">
                        {entry.target_type}
                        {entry.target_id ? ` · ${entry.target_id.slice(0, 8)}` : ""}
                      </span>
                      <span className="block truncate font-mono text-[10.5px] text-[var(--sb-text-3)]">
                        {JSON.stringify(entry.payload)}
                      </span>
                    </span>
                    <span className="truncate text-[var(--sb-text-2)]">
                      {entry.actor_type}
                      {entry.actor_id ? ` · ${entry.actor_id.slice(0, 8)}` : ""}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </main>
    </div>
  );
}
