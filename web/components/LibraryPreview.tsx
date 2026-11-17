"use client";

import { Download } from "lucide-react";

import { api } from "@/lib/api";
import type { LibraryItem, LibraryItemDetail } from "@/lib/types";
import { useLanguage } from "./LanguageProvider";
import { Markdown } from "./Markdown";

const TEXT_EXTENSIONS = [
  "txt", "csv", "tsv", "json", "log", "md", "py", "go", "js", "ts", "tsx", "jsx",
  "sh", "css", "yaml", "yml", "sql", "rs", "java", "c", "cpp", "html", "htm",
];

// Saves a Library item through a same-page link, so the session cookie goes
// along and the desktop app shows its save dialog instead of an external
// browser that is not signed in.
export function downloadLibraryItem(item: Pick<LibraryItem, "id">) {
  const link = document.createElement("a");
  link.href = api.libraryDownloadURL(item.id);
  link.download = "";
  link.rel = "noopener";
  document.body.appendChild(link);
  link.click();
  link.remove();
}

export function LibraryDownloadButton({
  item,
  variant = "button",
}: {
  item: LibraryItem;
  variant?: "button" | "icon";
}) {
  const { t } = useLanguage();
  if (variant === "icon") {
    return (
      <a
        href={api.libraryDownloadURL(item.id)}
        download=""
        className="flex h-6 w-6 shrink-0 items-center justify-center rounded text-[var(--sb-text-4)] transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-text)]"
        aria-label={t("library.downloadItem", { name: item.title })}
        title={t("library.download")}
        onClick={(event) => event.stopPropagation()}
      >
        <Download className="h-3.5 w-3.5" />
      </a>
    );
  }
  return (
    <a
      href={api.libraryDownloadURL(item.id)}
      download=""
      className="sb-btn"
      data-variant="secondary"
      data-size="sm"
      aria-label={t("library.downloadItem", { name: item.title })}
    >
      <Download className="h-3.5 w-3.5" />
      {t("library.download")}
    </a>
  );
}

export function LibraryPreview({ detail }: { detail: LibraryItemDetail }) {
  const { item, content } = detail;
  const url = item.file_url;
  const mime = item.mime_type || "";
  if (url && mime.startsWith("image/")) {
    return <img src={url} alt={item.title} className="max-h-[62vh] w-full rounded-lg object-contain" />;
  }
  if (url && mime.startsWith("audio/")) {
    return <audio controls preload="metadata" src={url} className="w-full" />;
  }
  if (url && mime.startsWith("video/")) {
    return <video controls src={url} className="max-h-[62vh] w-full rounded-lg bg-black" />;
  }
  if (url && mime === "application/pdf") {
    return <iframe title={item.title} src={url} className="h-[62vh] w-full rounded-lg border border-[var(--sb-border)]" />;
  }
  const extension = (item.title.includes(".") ? item.title.split(".").pop() : "")?.toLowerCase() ?? "";
  const officePreview =
    extension === "docx" ||
    extension === "xlsx" ||
    extension === "pptx" ||
    mime.includes("wordprocessingml") ||
    mime.includes("spreadsheetml") ||
    mime.includes("presentationml");
  const textPreview = TEXT_EXTENSIONS.includes(extension) || mime.startsWith("text/");
  if (url && (officePreview || textPreview)) {
    return (
      <iframe
        title={item.title}
        src={`/api/library/${item.id}/preview`}
        className="h-[62vh] w-full rounded-lg border border-[var(--sb-border)] bg-white"
      />
    );
  }
  if (content) return <Markdown content={content} />;
  if (url) {
    return (
      <div className="flex flex-col items-center gap-3 py-10 text-[13px] text-[var(--sb-text-2)]">
        这个文件没有在线预览，可以下载后查看。
        <LibraryDownloadButton item={item} />
      </div>
    );
  }
  return <div className="py-10 text-center text-[13px] text-[var(--sb-text-2)]">这个资料没有可预览的内容。</div>;
}
