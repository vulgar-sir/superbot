"use client";

import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { X } from "lucide-react";

import { cn } from "@/lib/format";

export function Modal({
  open,
  onClose,
  title,
  subtitle,
  children,
  footer,
  size = "md",
  closeOnBackdrop = true,
  bodyClassName,
  headerClassName,
  titleClassName,
  headerExtra,
  panelClassName,
}: {
  open: boolean;
  onClose: () => void;
  title?: React.ReactNode;
  subtitle?: React.ReactNode;
  children: React.ReactNode;
  footer?: React.ReactNode;
  size?: "sm" | "md" | "lg" | "xl";
  closeOnBackdrop?: boolean;
  bodyClassName?: string;
  headerClassName?: string;
  titleClassName?: string;
  headerExtra?: React.ReactNode;
  panelClassName?: string;
}) {
  const panelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.stopPropagation();
        onClose();
      }
    };
    document.addEventListener("keydown", onKey, true);
    const previous = document.activeElement as HTMLElement | null;
    const timer = window.setTimeout(() => {
      const panel = panelRef.current;
      const input = panel?.querySelector<HTMLElement>(
        "input:not([type=hidden]), textarea, [contenteditable=true]",
      );
      const button = Array.from(panel?.querySelectorAll<HTMLElement>("button") ?? []).find(
        (item) => item.getAttribute("aria-label") !== "关闭",
      );
      (input ?? button)?.focus();
    }, 40);
    return () => {
      document.removeEventListener("keydown", onKey, true);
      window.clearTimeout(timer);
      previous?.focus?.();
    };
  }, [open, onClose]);

  if (!open || typeof document === "undefined") return null;

  const sizeClass = {
    sm: "sb-dialog-narrow",
    md: "sb-dialog-md",
    lg: "sb-dialog-lg",
    xl: "sb-dialog-xl",
  }[size];

  return createPortal(
    <div
      className="sb-overlay"
      onMouseDown={(event) => {
        if (closeOnBackdrop && event.target === event.currentTarget) onClose();
      }}
      role="presentation"
    >
      <div
        ref={panelRef}
        className={cn("sb-modal", sizeClass, panelClassName)}
        role="dialog"
        aria-modal="true"
        aria-label={typeof title === "string" ? title : undefined}
      >
        {(title || subtitle) && (
          <div className={cn("sb-modal-header", headerClassName)}>
            <div className="min-w-0">
              {title && <div className={cn("sb-modal-title truncate", titleClassName)}>{title}</div>}
              {subtitle && (
                <div className="mt-0.5 text-[12px] leading-[16px] text-[var(--sb-text-2)]">
                  {subtitle}
                </div>
              )}
            </div>
            <div className="ml-auto flex shrink-0 items-center gap-3">
              {headerExtra}
              <button type="button" className="sb-icon-btn" data-size="sm" onClick={onClose} aria-label="关闭">
                <X className="h-4 w-4" />
              </button>
            </div>
          </div>
        )}
        {!title && (
          <button
            type="button"
            className="sb-icon-btn absolute right-3 top-3 z-10"
            data-size="sm"
            onClick={onClose}
            aria-label="关闭"
          >
            <X className="h-4 w-4" />
          </button>
        )}
        <div className={cn("sb-modal-body", bodyClassName)}>{children}</div>
        {footer && <div className="sb-modal-footer">{footer}</div>}
      </div>
    </div>,
    document.body,
  );
}
