import { clsx, type ClassValue } from "clsx";

export function cn(...values: ClassValue[]): string {
  return clsx(values);
}

export function formatCNY(micros: number): string {
  const value = (micros ?? 0) / 1_000_000;
  if (value === 0) return "¥0";
  if (value < 0.01) return `¥${value.toFixed(4)}`;
  return `¥${value.toFixed(2)}`;
}

export function formatNumber(value: number): string {
  if (!value) return "0";
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}k`;
  return String(value);
}

export function initials(name: string): string {
  const trimmed = (name || "?").trim();
  if (!trimmed) return "?";
  const chars = Array.from(trimmed);
  if (/[一-鿿]/.test(trimmed)) {
    return chars.slice(0, 1).join("");
  }
  const words = trimmed.split(/\s+/).filter(Boolean);
  if (words.length >= 2) {
    return (words[0][0] + words[1][0]).toUpperCase();
  }
  return chars.slice(0, 2).join("").toUpperCase();
}

export function avatarHue(id: string): number {
  let hash = 0;
  for (let i = 0; i < id.length; i++) {
    hash = (hash * 31 + id.charCodeAt(i)) >>> 0;
  }
  return hash % 360;
}

export function formatTime(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
}

export function timeAgo(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  const diff = Date.now() - date.getTime();
  const minutes = Math.floor(diff / 60_000);
  const english =
    typeof document !== "undefined" && document.documentElement.lang.startsWith("en");
  if (minutes < 1) return english ? "just now" : "刚刚";
  if (minutes < 60) return english ? `${minutes} min ago` : `${minutes} 分钟前`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return english ? `${hours} h ago` : `${hours} 小时前`;
  const days = Math.floor(hours / 24);
  return english ? `${days} d ago` : `${days} 天前`;
}

export function truncate(value: string, max = 120): string {
  if (value.length <= max) return value;
  return `${value.slice(0, max)}…`;
}

export function formatBytes(value: number): string {
  if (!value || value < 1024) return `${Math.max(0, Math.round(value || 0))} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let size = value / 1024;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit += 1;
  }
  return `${size >= 10 ? Math.round(size) : size.toFixed(1)} ${units[unit]}`;
}
