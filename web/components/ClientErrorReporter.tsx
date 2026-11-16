"use client";

import { useEffect, useRef } from "react";

import { api } from "@/lib/api";

const EVENT_NAME = "superbot:data-sharing";

export function ClientErrorReporter() {
  const enabledRef = useRef(false);
  const reportedRef = useRef<Set<string>>(new Set());

  useEffect(() => {
    let cancelled = false;
    api
      .getDataSharing()
      .then((prefs) => {
        if (!cancelled) enabledRef.current = prefs.enabled;
      })
      .catch(() => undefined);

    const onPrefs = (event: Event) => {
      const detail = (event as CustomEvent<{ enabled?: boolean }>).detail;
      if (detail && typeof detail.enabled === "boolean") {
        enabledRef.current = detail.enabled;
      }
    };
    window.addEventListener(EVENT_NAME, onPrefs);

    const report = (kind: string, message: string, stack?: string) => {
      if (!enabledRef.current || !message) return;
      const key = `${kind}:${message}`;
      if (reportedRef.current.has(key)) return;
      reportedRef.current.add(key);
      void api
        .reportClientError({
          kind,
          message,
          stack: stack ?? "",
          url: typeof window === "undefined" ? "" : window.location.href,
        })
        .catch(() => undefined);
    };

    const onError = (event: ErrorEvent) => {
      const error = event.error instanceof Error ? event.error : null;
      report("client_error", event.message || error?.message || "unknown error", error?.stack);
    };
    const onRejection = (event: PromiseRejectionEvent) => {
      const reason = event.reason;
      if (reason instanceof Error) {
        report("unhandled_rejection", reason.message, reason.stack);
      } else if (typeof reason === "string") {
        report("unhandled_rejection", reason);
      }
    };

    window.addEventListener("error", onError);
    window.addEventListener("unhandledrejection", onRejection);
    return () => {
      cancelled = true;
      window.removeEventListener(EVENT_NAME, onPrefs);
      window.removeEventListener("error", onError);
      window.removeEventListener("unhandledrejection", onRejection);
    };
  }, []);

  return null;
}

export function notifyDataSharing(enabled: boolean) {
  window.dispatchEvent(new CustomEvent(EVENT_NAME, { detail: { enabled } }));
}
