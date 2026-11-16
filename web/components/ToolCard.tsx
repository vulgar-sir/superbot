"use client";

import { useEffect, useState } from "react";
import {
  Check,
  ChevronDown,
  FileText,
  FolderOpen,
  Loader2,
  Search,
  Terminal,
  Wrench,
  X,
} from "lucide-react";

import { truncate } from "@/lib/format";
import { useLanguage } from "@/components/LanguageProvider";
import type { ToolActivity } from "@/lib/types";

function toolIcon(name: string) {
  if (name.startsWith("workspace_read") || name.startsWith("workspace_write")) return FileText;
  if (name.startsWith("workspace_list")) return FolderOpen;
  if (name.startsWith("workspace_search") || name === "web_search") return Search;
  if (name.startsWith("shell") || name.startsWith("python")) return Terminal;
  return Wrench;
}

export function ToolCard({ tool }: { tool: ToolActivity }) {
  const { t } = useLanguage();
  const Icon = toolIcon(tool.name);
  return (
    <div className="overflow-hidden rounded-[10px] border border-[var(--sb-border)] bg-[var(--sb-pane)]">
      <details className="group">
        <summary className="flex cursor-pointer list-none items-center gap-2 px-2.5 py-1.5 transition hover:bg-[var(--sb-hover)]">
          <Icon className="h-3.5 w-3.5 shrink-0 text-[var(--sb-text-2)]" />
          <span className="min-w-0 flex-1 truncate text-[12px] text-[var(--sb-text)]">{tool.name}</span>
          {tool.status === "running" ? (
            <Loader2 className="sb-spin h-3.5 w-3.5 shrink-0 text-[var(--sb-warn)]" />
          ) : tool.status === "failed" ? (
            <X className="h-3.5 w-3.5 shrink-0 text-[var(--sb-danger)]" />
          ) : (
            <Check className="h-3.5 w-3.5 shrink-0 text-[var(--sb-success)]" />
          )}
          <ChevronDown className="h-3 w-3 shrink-0 text-[var(--sb-text-4)] transition group-open:rotate-180" />
        </summary>
        <div className="space-y-1.5 border-t border-[var(--sb-border)] px-2.5 py-2">
          <div>
            <div className="mb-1 text-[10px] uppercase tracking-wide text-[var(--sb-text-4)]">{t("tool.params")}</div>
            <pre className="max-h-32 overflow-auto rounded-md bg-[var(--sb-sunken)] p-2 font-[var(--sb-mono)] text-[11px] leading-[17px] text-[var(--sb-text-2)]">
              {JSON.stringify(tool.args ?? {}, null, 2)}
            </pre>
          </div>
          {tool.widgetHtml && (
            <div>
              <div className="mb-1 text-[10px] uppercase tracking-wide text-[var(--sb-text-4)]">{t("tool.app")}</div>
              <iframe
                title={`${tool.name} MCP App`}
                sandbox="allow-scripts"
                srcDoc={`<script>window.__MCP_TOOL_RESULT__ = ${JSON.stringify(tool.result ?? "").replace(/</g, "\u003c")};</script>${tool.widgetHtml}`}
                className="h-64 w-full rounded-md border border-[var(--sb-border)] bg-white"
              />
            </div>
          )}
          {tool.result !== undefined && (
            <div>
              <div className="mb-1 text-[10px] uppercase tracking-wide text-[var(--sb-text-4)]">{t("tool.result")}</div>
              <pre
                className="max-h-40 overflow-auto rounded-md bg-[var(--sb-sunken)] p-2 font-[var(--sb-mono)] text-[11px] leading-[17px]"
                style={{ color: tool.isError ? "var(--sb-danger)" : "var(--sb-text-2)" }}
              >
                {truncate(tool.result || t("tool.noOutput"), 1600)}
              </pre>
            </div>
          )}
        </div>
      </details>
    </div>
  );
}

/**
 * Collapse a run's tool activity into one row. Expanding it reveals the
 * individual (already collapsed) tool cards, so a long browser/shell session
 * no longer occupies a screenful of chat space.
 */
export function ToolGroup({ tools }: { tools: ToolActivity[] }) {
  const { t } = useLanguage();
  const [open, setOpen] = useState(false);
  const firstCallId = tools[0]?.callId ?? "";
  useEffect(() => {
    // A new run starts collapsed; expanding while the same run is streaming
    // keeps the user's choice.
    setOpen(false);
  }, [firstCallId]);
  if (tools.length === 0) return null;
  if (tools.length === 1) return <ToolCard tool={tools[0]} />;
  const running = tools.filter((tool) => tool.status === "running").length;
  const failed = tools.filter((tool) => tool.status === "failed").length;
  return (
    <div className="overflow-hidden rounded-[10px] border border-[var(--sb-border)] bg-[var(--sb-pane)]">
      <button
        type="button"
        className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left transition hover:bg-[var(--sb-hover)]"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        <Wrench className="h-3.5 w-3.5 shrink-0 text-[var(--sb-text-2)]" />
        <span className="shrink-0 text-[12px] font-medium text-[var(--sb-text)]">
          {t("tool.calls", { n: tools.length })}
        </span>
        <span className="min-w-0 flex-1 truncate text-[11px] text-[var(--sb-text-4)]">
          {tools.map((tool) => tool.name).join(" · ")}
        </span>
        {running > 0 ? (
          <Loader2 className="sb-spin h-3.5 w-3.5 shrink-0 text-[var(--sb-warn)]" />
        ) : failed > 0 ? (
          <X className="h-3.5 w-3.5 shrink-0 text-[var(--sb-danger)]" />
        ) : (
          <Check className="h-3.5 w-3.5 shrink-0 text-[var(--sb-success)]" />
        )}
        <ChevronDown
          className={`h-3 w-3 shrink-0 text-[var(--sb-text-4)] transition ${open ? "rotate-180" : ""}`}
        />
      </button>
      {open && (
        <div className="grid grid-cols-1 gap-1 border-t border-[var(--sb-border)] p-1.5 sm:grid-cols-2">
          {tools.map((tool) => (
            <ToolCard key={tool.callId} tool={tool} />
          ))}
        </div>
      )}
    </div>
  );
}
