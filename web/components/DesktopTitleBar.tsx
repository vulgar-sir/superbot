"use client";

import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { Minus, Square, X } from "lucide-react";

export function DesktopTitleBar() {
  const [desktop, setDesktop] = useState(false);
  const [maximized, setMaximized] = useState(false);
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setMounted(true);
  }, []);

  useEffect(() => {
    if (!window.superbotDesktop) return;
    setDesktop(true);
    document.documentElement.classList.add("sb-desktop");
    void window.superbotDesktop.isWindowMaximized?.().then(setMaximized);
    return () => document.documentElement.classList.remove("sb-desktop");
  }, []);

  if (!desktop || !window.superbotDesktop || !mounted) return null;

  const controls = window.superbotDesktop;

  // Render last in the document so the no-drag region wins over the page
  // headers' drag regions; otherwise Chromium turns caption clicks into a
  // window drag and the buttons never fire.
  return createPortal(
    <>
      <div className="sb-desktop-drag-strip" aria-hidden />
      <div className="sb-desktop-caption" role="banner" aria-label="窗口控制">
        <span className="sr-only">superbot</span>
        <button
          type="button"
          aria-label="最小化"
          title="最小化"
          onClick={() => void controls.minimizeWindow?.()}
        >
          <Minus className="h-2.5 w-2.5" strokeWidth={1.4} />
        </button>
        <button
          type="button"
          aria-label={maximized ? "还原窗口" : "最大化"}
          title={maximized ? "还原窗口" : "最大化"}
          onClick={async () => {
            const result = await controls.toggleMaximizeWindow?.();
            setMaximized(Boolean(result));
          }}
        >
          <Square className="h-[9px] w-[9px]" strokeWidth={1.4} />
        </button>
        <button
          type="button"
          className="sb-desktop-close"
          aria-label="关闭"
          title="关闭"
          onClick={() => void controls.closeWindow?.()}
        >
          <X className="h-2.5 w-2.5" strokeWidth={1.4} />
        </button>
      </div>
    </>,
    document.body,
  );
}
