"use client";

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Check, ChevronRight } from "lucide-react";

import { cn } from "@/lib/format";

export type MenuItem = {
  key: string;
  label?: string;
  icon?: React.ReactNode;
  shortcut?: string;
  checked?: boolean;
  danger?: boolean;
  disabled?: boolean;
  separator?: boolean;
  heading?: string;
  description?: string;
  children?: MenuItem[];
  onSelect?: () => void;
};

type Anchor = { x: number; y: number } | { rect: DOMRect };

function anchorToPoint(anchor: Anchor): { x: number; y: number } {
  if ("x" in anchor) return anchor;
  return { x: anchor.rect.left, y: anchor.rect.bottom + 4 };
}

export function Menu({
  open,
  anchor,
  items,
  onClose,
  minWidth = 200,
  align = "left",
  header,
  nested = false,
}: {
  open: boolean;
  anchor: Anchor | null;
  items: MenuItem[];
  onClose: () => void;
  minWidth?: number;
  align?: "left" | "right";
  header?: React.ReactNode;
  // Nested menus must stay inside the parent menu's DOM so the parent's
  // outside-click handler does not unmount them before their click fires.
  nested?: boolean;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState<{ left: number; top: number } | null>(null);
  const [highlight, setHighlight] = useState(-1);
  const [submenu, setSubmenu] = useState<{ items: MenuItem[]; rect: DOMRect } | null>(null);

  const actionable = items.filter((item) => !item.separator && !item.heading && !item.disabled);

  useLayoutEffect(() => {
    if (!open || !anchor) {
      setPosition(null);
      return;
    }
    const point = anchorToPoint(anchor);
    const el = ref.current;
    const width = el?.offsetWidth ?? minWidth;
    const height = el?.offsetHeight ?? 240;
    const padding = 8;
    let left = align === "right" ? point.x - width : point.x;
    let top = point.y;
    if (left + width > window.innerWidth - padding) left = window.innerWidth - width - padding;
    if (left < padding) left = padding;
    if (top + height > window.innerHeight - padding) {
      if ("rect" in anchor) top = anchor.rect.top - height - 4;
      else top = window.innerHeight - height - padding;
    }
    if (top < padding) top = padding;
    setPosition({ left, top });
  }, [open, anchor, minWidth, align]);

  // Drop any submenu left over from the previous time this menu was open.
  useEffect(() => {
    if (open) setSubmenu(null);
  }, [open]);

  useEffect(() => {
    if (!open) return;
    setHighlight(-1);
    const onDown = (event: MouseEvent) => {
      if (!ref.current?.contains(event.target as Node)) onClose();
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        onClose();
        return;
      }
      if (event.key === "ArrowRight" && highlight >= 0) {
        const item = items[highlight];
        if (item?.children?.length) {
          event.preventDefault();
          const el = ref.current?.querySelector<HTMLElement>(`[data-menu-index="${highlight}"]`);
          if (el) setSubmenu({ items: item.children, rect: el.getBoundingClientRect() });
        }
        return;
      }
      if (event.key === "ArrowLeft" && submenu) {
        event.preventDefault();
        setSubmenu(null);
        return;
      }
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        if (actionable.length === 0) return;
        setHighlight((prev) => {
          const current = actionable.findIndex((item) => item.key === items[prev]?.key);
          const delta = event.key === "ArrowDown" ? 1 : -1;
          const next = (current + delta + actionable.length) % actionable.length;
          return items.indexOf(actionable[next]);
        });
      }
      if (event.key === "Enter" && highlight >= 0) {
        event.preventDefault();
        items[highlight]?.onSelect?.();
        onClose();
      }
    };
    window.addEventListener("mousedown", onDown, true);
    window.addEventListener("keydown", onKey, true);
    const onScroll = (event: Event) => {
      if (ref.current?.contains(event.target as Node)) return;
      onClose();
    };
    window.addEventListener("resize", onClose);
    window.addEventListener("scroll", onScroll, true);
    return () => {
      window.removeEventListener("mousedown", onDown, true);
      window.removeEventListener("keydown", onKey, true);
      window.removeEventListener("resize", onClose);
      window.removeEventListener("scroll", onScroll, true);
    };
  }, [open, onClose, actionable, highlight, items]);

  const submenuAnchor = useMemo(
    () => (submenu ? { x: submenu.rect.right + 2, y: submenu.rect.top } : null),
    [submenu],
  );

  if (!open || typeof document === "undefined") return null;

  const content = (
    <div
      ref={ref}
      className="sb-menu fixed z-[80]"
      role="menu"
      style={{
        left: position?.left ?? -9999,
        top: position?.top ?? -9999,
        minWidth,
        visibility: position ? "visible" : "hidden",
      }}
    >
      {header && <div className="border-b border-[var(--sb-border)] px-1 py-1">{header}</div>}
      {items.map((item, index) => {
        if (item.separator) return <div key={item.key} className="sb-menu-separator" />;
        if (item.heading) {
          return (
            <div key={item.key} className="sb-menu-label">
              {item.heading}
            </div>
          );
        }
        return (
          <button
            key={item.key}
            type="button"
            role="menuitem"
            data-menu-index={index}
            className="sb-menu-item"
            data-tone={item.danger ? "danger" : undefined}
            data-highlighted={highlight === index ? "true" : undefined}
            disabled={item.disabled}
            aria-haspopup={item.children?.length ? "menu" : undefined}
            onMouseEnter={(event) => {
              setHighlight(index);
              if (item.children?.length) {
                setSubmenu({
                  items: item.children,
                  rect: event.currentTarget.getBoundingClientRect(),
                });
              } else {
                setSubmenu(null);
              }
            }}
            onClick={(event) => {
              if (item.disabled) return;
              if (item.children?.length) {
                setSubmenu({
                  items: item.children,
                  rect: event.currentTarget.getBoundingClientRect(),
                });
                return;
              }
              item.onSelect?.();
              onClose();
            }}
          >
            {item.icon && <span className="flex h-4 w-4 items-center justify-center">{item.icon}</span>}
            <span className="min-w-0 flex-1 truncate">{item.label}</span>
            {item.description && (
              <span className="shrink-0 text-[11px] text-[var(--sb-text-4)]">{item.description}</span>
            )}
            {item.shortcut && (
              <span className="shrink-0 text-[11px] text-[var(--sb-text-4)]">{item.shortcut}</span>
            )}
            {item.checked ? (
              <Check className="h-3.5 w-3.5 shrink-0 text-[var(--sb-text)]" />
            ) : null}
            {item.children?.length ? (
              <ChevronRight className="h-3.5 w-3.5 shrink-0 text-[var(--sb-text-3)]" />
            ) : null}
          </button>
        );
      })}
      {submenu && (
        <Menu
          open
          anchor={submenuAnchor}
          items={submenu.items.map((item) => ({
            ...item,
            onSelect: () => {
              item.onSelect?.();
              onClose();
            },
          }))}
          onClose={() => setSubmenu(null)}
          minWidth={180}
          nested
        />
      )}
    </div>
  );
  return nested ? content : createPortal(content, document.body);
}

export function useContextMenu() {
  const [anchor, setAnchor] = useState<{ x: number; y: number } | null>(null);

  const openAt = useCallback((event: React.MouseEvent) => {
    event.preventDefault();
    event.stopPropagation();
    setAnchor({ x: event.clientX, y: event.clientY });
  }, []);

  const close = useCallback(() => setAnchor(null), []);

  return { anchor, openAt, close, isOpen: anchor !== null };
}

export function MenuShortcut({ children }: { children: React.ReactNode }) {
  return <span className={cn("text-[11px] text-[var(--sb-text-4)]")}>{children}</span>;
}
