"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Check, ChevronDown, ChevronRight, ExternalLink, Loader2, Search } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import type { Connector } from "@/lib/types";
import { cn } from "@/lib/format";
import { Modal } from "./ui/Modal";
import { ConnectorDialog } from "./ConnectorDialog";
import { useToast } from "./ui/Toast";

type CatalogItem = {
  key: string;
  provider: string;
  name: string;
  category: "password" | "efficiency" | "communication" | "design" | "code";
  color: string;
  supported: boolean;
};

const CATALOG: CatalogItem[] = [
  { key: "github", provider: "github", name: "GitHub", category: "code", color: "#1c1c1c", supported: true },
  { key: "1password", provider: "onepassword", name: "1Password", category: "password", color: "#1c1c1c", supported: true },
  { key: "notion", provider: "notion", name: "Notion", category: "efficiency", color: "#1c1c1c", supported: true },
  { key: "atlassian", provider: "atlassian", name: "Atlassian", category: "efficiency", color: "#1868db", supported: true },
  { key: "gdocs", provider: "google_docs", name: "Google Docs", category: "efficiency", color: "#4285f4", supported: true },
  { key: "canva", provider: "canva", name: "Canva", category: "efficiency", color: "#8b3dff", supported: true },
  { key: "linear", provider: "linear", name: "Linear", category: "efficiency", color: "#5e6ad2", supported: true },
  { key: "onedrive", provider: "onedrive", name: "OneDrive", category: "efficiency", color: "#0078d4", supported: true },
  { key: "slack", provider: "slack", name: "Slack", category: "communication", color: "#611f69", supported: true },
  { key: "email", provider: "email", name: "邮箱", category: "communication", color: "#ee620d", supported: true },
  { key: "figma", provider: "figma", name: "Figma", category: "design", color: "#f24e1e", supported: true },
  { key: "vercel", provider: "vercel", name: "Vercel", category: "code", color: "#1c1c1c", supported: true },
  { key: "sentry", provider: "sentry", name: "Sentry", category: "code", color: "#362d59", supported: true },
  { key: "postgres", provider: "postgres", name: "Postgres", category: "code", color: "#336791", supported: true },
  { key: "gdrive", provider: "google_drive", name: "Google Drive", category: "efficiency", color: "#0f9d58", supported: true },
  { key: "teams", provider: "msteams", name: "Microsoft Teams", category: "communication", color: "#6264a7", supported: true },
];

const CATEGORIES = [
  { key: "all", label: "全部" },
  { key: "password", label: "密码管理器" },
  { key: "efficiency", label: "效率" },
  { key: "communication", label: "通信" },
  { key: "design", label: "设计" },
  { key: "code", label: "代码" },
] as const;

function CatalogIcon({ item }: { item: CatalogItem }) {
  return (
    <span
      className="flex h-6 w-6 shrink-0 items-center justify-center rounded-[7px] text-[11px] font-semibold text-white"
      style={{ background: item.color }}
    >
      {item.name.slice(0, 1).toUpperCase()}
    </span>
  );
}

export function ConnectorsDialog({
  open,
  onClose,
  onOpenBots,
  initialProvider = null,
}: {
  open: boolean;
  onClose: () => void;
  onOpenBots: () => void;
  initialProvider?: string | null;
}) {
  const { toast } = useToast();
  const [connectors, setConnectors] = useState<Connector[]>([]);
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState("");
  const [category, setCategory] = useState<(typeof CATEGORIES)[number]["key"]>("all");
  const [onlyConnected, setOnlyConnected] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);
  const [dialogProvider, setDialogProvider] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const result = await api.listConnectors();
      setConnectors(result.connectors);
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "加载连接应用失败", "danger");
    } finally {
      setLoading(false);
    }
  }, [toast]);

  useEffect(() => {
    if (open) {
      void load();
      setQuery("");
      setCategory("all");
      setOnlyConnected(false);
      setDialogProvider(initialProvider);
    }
  }, [open, load, initialProvider]);

  const connectedProviders = useMemo(() => {
    return new Set(connectors.map((connector) => connector.provider));
  }, [connectors]);

  const visible = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return CATALOG.filter((item) => {
      if (category !== "all" && item.category !== category) return false;
      if (onlyConnected && !connectedProviders.has(item.provider)) return false;
      if (needle && !`${item.name} ${item.provider}`.toLowerCase().includes(needle)) return false;
      return true;
    });
  }, [category, onlyConnected, connectedProviders, query]);

  const grouped = useMemo(() => {
    const groups: Array<{ key: string; label: string; items: CatalogItem[] }> = [];
    const labels: Record<string, string> = {
      password: "密码管理器",
      efficiency: "效率",
      communication: "通信",
      design: "设计",
      code: "代码",
    };
    const recommended = !query && category === "all" ? CATALOG[0].key : "";
    for (const item of visible) {
      if (item.key === recommended) continue;
      let group = groups.find((entry) => entry.key === item.category);
      if (!group) {
        group = { key: item.category, label: labels[item.category], items: [] };
        groups.push(group);
      }
      group.items.push(item);
    }
    return groups;
  }, [visible]);

  return (
    <>
      {initialProvider === null && (
      <Modal
        open={open}
        onClose={onClose}
        title="连接应用"
        size="xl"
        headerClassName="!border-b-0 !px-8 !pb-1 !pt-6"
        titleClassName="!text-[17px]"
        bodyClassName="!px-8 !pb-8 !pt-0"
        headerExtra={
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={() => {
                setCategory("all");
                setOnlyConnected(true);
              }}
              className="flex items-center gap-1 text-[12px] text-[var(--sb-text-2)] transition hover:text-[var(--sb-text)]"
            >
              <span className="flex -space-x-1">
                {connectors.slice(0, 3).map((connector) => (
                  <span
                    key={connector.id}
                    className="flex h-5 w-5 items-center justify-center rounded-full border border-[var(--sb-pane)] bg-[var(--sb-hover)] text-[9px]"
                    title={connector.name}
                  >
                    {connector.name.slice(0, 1).toUpperCase()}
                  </span>
                ))}
              </span>
              已连接 {connectors.length} 个
              <ChevronRight className="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              onClick={onOpenBots}
              className="flex h-7 items-center gap-1.5 rounded-full bg-[var(--sb-hover)] px-3 text-[12px] text-[var(--sb-text)] transition hover:bg-[var(--sb-active)]"
            >
              <span className="flex h-4 w-4 items-center justify-center rounded bg-[var(--sb-primary)] text-[9px] text-[var(--sb-primary-contrast)]">
                B
              </span>
              Bot 模板
              <ExternalLink className="h-3 w-3" />
            </button>
          </div>
        }
      >
        <label className="flex h-10 items-center gap-2 rounded-full border border-[var(--sb-border)] px-3.5">
          <Search className="h-4 w-4 text-[var(--sb-text-3)]" />
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="在应用和技能中搜索"
            className="min-w-0 flex-1 bg-transparent text-[13px] outline-none placeholder:text-[var(--sb-text-4)]"
          />
        </label>

        <div className="mt-3 flex flex-wrap items-center gap-2">
          {CATEGORIES.map((item) => (
            <button
              key={item.key}
              type="button"
              onClick={() => setCategory(item.key)}
              className={cn(
                "h-7 rounded-full px-3 text-[13px] transition",
                category === item.key
                  ? "bg-[var(--sb-primary)] text-[var(--sb-primary-contrast)]"
                  : "bg-[var(--sb-hover)] text-[var(--sb-text-2)] hover:text-[var(--sb-text)]",
              )}
            >
              {item.label}
            </button>
          ))}
          <div className="relative">
            <button
              type="button"
              onClick={() => setMoreOpen((prev) => !prev)}
              className={cn(
                "flex h-7 items-center gap-1 rounded-full px-3 text-[13px] transition",
                onlyConnected
                  ? "bg-[var(--sb-text)] text-[var(--sb-pane)]"
                  : "bg-[var(--sb-hover)] text-[var(--sb-text-2)] hover:text-[var(--sb-text)]",
              )}
            >
              更多
              <ChevronDown className="h-3.5 w-3.5" />
            </button>
            {moreOpen && (
              <div className="absolute left-0 top-9 z-20 w-40 overflow-hidden rounded-xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-1 shadow-[var(--sb-shadow-popup)]">
                <button
                  type="button"
                  onClick={() => {
                    setOnlyConnected(false);
                    setMoreOpen(false);
                  }}
                  className="flex h-8 w-full items-center rounded-lg px-2.5 text-left text-[13px] transition hover:bg-[var(--sb-hover)]"
                >
                  全部应用
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setOnlyConnected(true);
                    setMoreOpen(false);
                  }}
                  className="flex h-8 w-full items-center rounded-lg px-2.5 text-left text-[13px] transition hover:bg-[var(--sb-hover)]"
                >
                  仅已连接
                </button>
              </div>
            )}
          </div>
        </div>

        <div className="mt-2 pb-2">
          {loading ? (
            <div className="flex justify-center py-10">
              <Loader2 className="sb-spin h-5 w-5 text-[var(--sb-text-3)]" />
            </div>
          ) : (
            <>
              {category === "all" && !query && (
                <div>
                  <div className="mt-6 text-[14px] font-medium">为你推荐</div>
                  <div className="mt-2 grid gap-x-8 md:grid-cols-2">
                    <CatalogRow
                      item={CATALOG[0]}
                      connected={connectedProviders.has(CATALOG[0].provider)}
                      onConnect={() => setDialogProvider(CATALOG[0].provider)}
                    />
                  </div>
                </div>
              )}
              {grouped.map((group) => (
                <div key={group.key}>
                  <div className="mt-8 flex items-center justify-between gap-3">
                    <div className="text-[14px] font-medium">{group.label}</div>
                    {category === "all" && group.items.length > 2 && (
                      <button
                        type="button"
                        onClick={() => setCategory(group.key as (typeof CATEGORIES)[number]["key"])}
                        className="text-[12px] text-[var(--sb-text-2)] transition hover:text-[var(--sb-text)]"
                      >
                        查看全部
                      </button>
                    )}
                  </div>
                  <div className="mt-2 grid gap-x-8 md:grid-cols-2">
                    {group.items.map((item) => (
                      <CatalogRow
                        key={item.key}
                        item={item}
                        connected={connectedProviders.has(item.provider)}
                        onConnect={() => setDialogProvider(item.provider)}
                      />
                    ))}
                  </div>
                </div>
              ))}
              {visible.length === 0 && (
                <div className="py-12 text-center text-[13px] text-[var(--sb-text-2)]">
                  没有找到匹配的应用。
                </div>
              )}
            </>
          )}
        </div>
      </Modal>
      )}

      <ConnectorDialog
        open={dialogProvider !== null}
        initialProvider={dialogProvider ?? "github"}
        onClose={() => {
          setDialogProvider(null);
          if (initialProvider !== null) onClose();
        }}
        onCreated={() => {
          setDialogProvider(null);
          if (initialProvider !== null) {
            onClose();
          } else {
            void load();
          }
        }}
      />
    </>
  );
}

function CatalogRow({
  item,
  connected,
  onConnect,
}: {
  item: CatalogItem;
  connected: boolean;
  onConnect: () => void;
}) {
  return (
    <div className="flex h-16 items-center gap-3 border-b border-[var(--sb-border)]">
      <CatalogIcon item={item} />
      <span className="min-w-0 flex-1 truncate text-[13px] font-medium">{item.name}</span>
      {connected ? (
        <span className="inline-flex h-8 items-center gap-1 rounded-full bg-[var(--sb-hover)] px-3 text-[12px] text-[var(--sb-text-2)]">
          <Check className="h-3.5 w-3.5" />
          已连接
        </span>
      ) : item.supported ? (
        <button
          type="button"
          onClick={onConnect}
          className="h-8 rounded-full bg-[var(--sb-hover)] px-4 text-[13px] text-[var(--sb-text)] transition hover:bg-[var(--sb-active)]"
        >
          连接
        </button>
      ) : (
        <span className="rounded-full px-3 text-[12px] text-[var(--sb-text-4)]">即将支持</span>
      )}
    </div>
  );
}
