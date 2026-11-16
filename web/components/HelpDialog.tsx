"use client";

import packageInfo from "../package.json";
import { useLanguage } from "./LanguageProvider";
import { Modal } from "./ui/Modal";

const shortcutKeys: Array<[string, string]> = [
  ["⌘/Ctrl + K", "help.search"],
  ["⌘/Ctrl + ,", "help.openSettings"],
  ["⌘/Ctrl + \\", "help.toggleDetails"],
  ["⌘/Ctrl + B", "help.toggleSidebar"],
  ["/", "help.focusComposer"],
  ["Enter", "help.send"],
  ["Shift + Enter", "help.newline"],
  ["@", "help.mention"],
  ["Esc", "help.dismiss"],
];

export function HelpDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useLanguage();
  return (
    <Modal open={open} onClose={onClose} title={t("help.title")} subtitle={t("help.subtitle")} size="md">
      <div>
        <div className="text-[12px] text-[var(--sb-text-3)]">{t("help.shortcuts")}</div>
        <div className="mt-2 overflow-hidden rounded-xl border border-[var(--sb-border)]">
          {shortcutKeys.map(([key, labelKey], index) => (
            <div
              key={key}
              className={`flex items-center justify-between gap-4 px-3.5 py-2.5 ${
                index > 0 ? "border-t border-[var(--sb-border)]" : ""
              }`}
            >
              <span className="text-[13px] text-[var(--sb-text)]">{t(labelKey)}</span>
              <kbd className="rounded-md bg-[var(--sb-hover)] px-2 py-0.5 font-[var(--sb-mono)] text-[11px] text-[var(--sb-text-2)]">
                {key}
              </kbd>
            </div>
          ))}
        </div>

        <div className="mt-5 text-[12px] text-[var(--sb-text-3)]">{t("help.install")}</div>
        <p className="mt-2 text-[12.5px] leading-[19px] text-[var(--sb-text-2)]">{t("help.installBody")}</p>

        <div className="mt-5 text-[12px] text-[var(--sb-text-3)]">{t("help.selfHosted")}</div>
        <p className="mt-2 text-[12.5px] leading-[19px] text-[var(--sb-text-2)]">
          {t("help.selfHostedBefore")}{" "}
          <code className="rounded bg-[var(--sb-hover)] px-1 py-0.5 font-[var(--sb-mono)] text-[11px]">
            README.md
          </code>
          {t("help.selfHostedBetween")}{" "}
          <code className="rounded bg-[var(--sb-hover)] px-1 py-0.5 font-[var(--sb-mono)] text-[11px]">
            docs/grokbot-parity-plan.md
          </code>
          {t("help.selfHostedAfter")}
        </p>

        <div className="mt-5 flex items-center justify-between rounded-xl bg-[var(--sb-hover)] px-3.5 py-3 text-[12px] text-[var(--sb-text-2)]">
          <span>superbot</span>
          <span className="font-[var(--sb-mono)] text-[11px]">v{packageInfo.version} · self-hosted</span>
        </div>
      </div>
    </Modal>
  );
}
