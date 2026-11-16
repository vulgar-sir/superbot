"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Activity, Bot, CalendarDays, CheckSquare, Clock, FileText, LogOut, Mail, MessageSquare, Package, Puzzle, ScrollText, Settings2, Users } from "lucide-react";
import { Logo } from "./Logo";
import { useLanguage } from "./LanguageProvider";
import { cn } from "@/lib/format";
import type { User } from "@/lib/types";

const items = [
  { href: "/", key: "nav.chats", icon: MessageSquare },
  { href: "/activity", key: "nav.activity", icon: Activity },
  { href: "/bots", key: "nav.bots", icon: Bot },
  { href: "/teams", key: "nav.teams", icon: Users },
  { href: "/extensions", key: "nav.connectors", icon: Puzzle },
  { href: "/tasks", key: "nav.tasks", icon: CheckSquare },
  { href: "/documents", key: "nav.documents", icon: FileText },
  { href: "/calendar", key: "nav.calendar", icon: CalendarDays },
  { href: "/email", key: "nav.email", icon: Mail },
  { href: "/routines", key: "nav.routines", icon: Clock },
  { href: "/audit", key: "nav.audit", icon: ScrollText },
  { href: "/store", key: "nav.store", icon: Package },
  { href: "/settings", key: "nav.settings", icon: Settings2 },
];

export function Rail({ user, onLogout }: { user: User; onLogout: () => void }) {
  const pathname = usePathname();
  const { t } = useLanguage();
  return (
    <>
    <nav className="hidden w-[310px] shrink-0 flex-col border-r border-[var(--sb-border)] bg-[var(--sb-sidebar)] md:flex">
      <div className="px-4 pb-3 pt-4">
        <Link href="/" aria-label="superbot 首页" className="inline-flex rounded-lg px-1 py-0.5">
          <Logo />
        </Link>
      </div>
      <div className="flex-1 space-y-0.5 overflow-y-auto px-3">
        {items.map((item) => {
          const active = item.href === "/" ? pathname === "/" : pathname.startsWith(item.href);
          const Icon = item.icon;
          return (
            <Link
              key={item.href}
              href={item.href}
              title={t(item.key)}
              className={cn("sb-menu-item", active && "bg-[var(--sb-hover)]")}
              data-highlighted={active ? "true" : undefined}
            >
              <span className="flex h-4 w-4 items-center justify-center text-[var(--sb-text-2)]">
                <Icon className="h-4 w-4" />
              </span>
              <span className="flex-1">{t(item.key)}</span>
            </Link>
          );
        })}
      </div>
      <div className="flex items-center gap-2 border-t border-[var(--sb-border)] px-3 py-3">
        <div
          className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-[var(--sb-hover)] text-[12px] font-medium text-[var(--sb-text-2)]"
          title={`${user.display_name || user.email} · ${user.email}`}
        >
          {(user.display_name || user.email).slice(0, 1).toUpperCase()}
        </div>
        <div className="min-w-0 flex-1">
          <div className="truncate text-[12px] leading-[16px]">{user.display_name || user.email}</div>
          <div className="truncate text-[10px] leading-[14px] text-[var(--sb-text-4)]">{user.email}</div>
        </div>
        <button
          type="button"
          onClick={onLogout}
          title="退出登录"
          className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--sb-text-3)] transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-danger)]"
        >
          <LogOut className="h-4 w-4" />
        </button>
      </div>
    </nav>
    <nav className="fixed inset-x-0 bottom-0 z-40 grid grid-cols-13 border-t border-[var(--sb-border)] bg-[var(--sb-pane)] pb-[env(safe-area-inset-bottom)] md:hidden">
      {items.map((item) => {
        const active = item.href === "/" ? pathname === "/" : pathname.startsWith(item.href);
        const Icon = item.icon;
        return (
          <Link
            key={item.href}
            href={item.href}
            className={cn(
              "flex min-w-0 flex-col items-center gap-0.5 py-2 text-[9px]",
              active ? "text-[var(--sb-text)]" : "text-[var(--sb-text-3)]",
            )}
          >
            <Icon className="h-4 w-4" />
            <span className="max-w-full truncate">{t(item.key)}</span>
          </Link>
        );
      })}
    </nav>
    </>
  );
}
