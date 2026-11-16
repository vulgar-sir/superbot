"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ArrowLeft, Loader2, Package, Store } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import type { StoreTemplate, User } from "@/lib/types";
import { Rail } from "@/components/Rail";
import { useToast } from "@/components/ui/Toast";
import { useLanguage } from "@/components/LanguageProvider";

export default function StorePage() {
  const router = useRouter();
  const { t } = useLanguage();
  const { toast } = useToast();
  const [user, setUser] = useState<User | null>(null);
  const [templates, setTemplates] = useState<StoreTemplate[]>([]);
  const [loading, setLoading] = useState(true);
  const [installing, setInstalling] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [me, result] = await Promise.all([api.me(), api.listStoreTemplates()]);
      setUser(me.user);
      setTemplates(result.templates);
      setError(null);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        router.replace("/login");
        return;
      }
      setError(err instanceof Error ? err.message : t("store.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [router]);

  useEffect(() => {
    void load();
  }, [load]);

  const install = async (template: StoreTemplate) => {
    setInstalling(template.id);
    try {
      const result = await api.installStoreTemplate(template.id);
      await api.importBotTemplate(result.template);
      setTemplates((prev) =>
        prev.map((item) =>
          item.id === template.id ? { ...item, install_count: item.install_count + 1 } : item,
        ),
      );
      toast(`已从「${template.name}」创建 Bot`, "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "安装模板失败", "danger");
    } finally {
      setInstalling(null);
    }
  };

  if (!user) {
    return <div className="flex h-screen items-center justify-center"><Loader2 className="h-5 w-5 animate-spin" /></div>;
  }

  return (
    <div className="flex h-screen w-full overflow-hidden bg-[var(--sb-bg)]">
      <Rail user={user} onLogout={() => void api.logout().finally(() => router.replace("/login"))} />
      <main className="flex min-w-0 flex-1 flex-col pb-16 md:pb-0">
        <header className="sb-page-header flex h-[61px] shrink-0 items-center gap-3 border-b border-[var(--sb-border)] px-6">
          <Link href="/" className="sb-icon-btn md:hidden" aria-label={t("store.backToChat")}>
            <ArrowLeft className="h-4 w-4" />
          </Link>
          <Store className="h-4 w-4 text-[var(--sb-text-2)]" />
          <h1 className="text-[17px] font-semibold">Bot Store</h1>
          <span className="rounded-full bg-[var(--sb-hover)] px-2 py-0.5 text-[11px] text-[var(--sb-text-2)]">
            {templates.length}
          </span>
        </header>

        <div className="flex-1 overflow-y-auto px-6 py-6">
          {error && (
            <div className="mb-4 rounded-xl bg-[var(--sb-danger-soft)] px-4 py-3 text-[12px] text-[var(--sb-danger)]">
              {error}
            </div>
          )}
          {loading ? (
            <div className="flex justify-center py-16"><Loader2 className="h-5 w-5 animate-spin" /></div>
          ) : templates.length === 0 ? (
            <div className="rounded-2xl border border-dashed border-[var(--sb-border)] px-4 py-16 text-center text-[13px] text-[var(--sb-text-2)]">
              {t("store.empty")}
            </div>
          ) : (
            <div className="grid gap-4 md:grid-cols-2 2xl:grid-cols-3">
              {templates.map((template) => (
                <article key={template.id} className="flex flex-col rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-4">
                  <div className="flex items-center gap-2">
                    <Package className="h-4 w-4 text-[var(--sb-text-2)]" />
                    <span className="min-w-0 flex-1 truncate text-[14px] font-medium">{template.name}</span>
                    <span className="font-mono text-[9px] text-[var(--sb-text-3)]">v{template.version}</span>
                  </div>
                  <p className="mt-2 line-clamp-3 min-h-[54px] text-[12px] leading-[18px] text-[var(--sb-text-2)]">
                    {template.description || t("store.noDesc")}
                  </p>
                  <div className="mt-3 flex items-center justify-between text-[11px] text-[var(--sb-text-3)]">
                    <span>{template.owner_name || t("store.unknownOwner")}</span>
                    <span>{t("store.installs", { n: template.install_count })}</span>
                  </div>
                  <button
                    type="button"
                    onClick={() => void install(template)}
                    disabled={installing === template.id}
                    className="sb-btn mt-3 w-full"
                    data-variant="primary"
                    data-size="sm"
                  >
                    {installing === template.id ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
                    {t("store.createBot")}
                  </button>
                </article>
              ))}
            </div>
          )}
        </div>
      </main>
    </div>
  );
}
