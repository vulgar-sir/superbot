// copyText copies text to the clipboard. The async Clipboard API exists only
// in secure contexts (HTTPS or localhost); the deployment is reached over
// plain HTTP, where navigator.clipboard is undefined and every copy button
// failed. The fallback selects the text in an off-screen textarea and uses
// the legacy copy command, which browsers still allow on a user gesture.
export async function copyText(text: string): Promise<void> {
  if (typeof window !== "undefined" && window.isSecureContext && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch {
      // Permission denied or not focused: try the legacy path below.
    }
  }
  if (typeof document === "undefined") throw new Error("clipboard unavailable");
  const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  // Inside a modal, a focus trap would pull focus back out of a textarea
  // appended to <body>, so the textarea goes into the dialog itself.
  const host = previous?.closest<HTMLElement>("[role=dialog]") ?? document.body;
  const area = document.createElement("textarea");
  area.value = text;
  area.setAttribute("readonly", "");
  area.setAttribute("aria-hidden", "true");
  area.style.position = "fixed";
  area.style.top = "0";
  area.style.left = "0";
  area.style.width = "1px";
  area.style.height = "1px";
  area.style.opacity = "0";
  area.style.pointerEvents = "none";
  host.appendChild(area);
  area.focus({ preventScroll: true });
  area.select();
  area.setSelectionRange(0, text.length);
  let copied = false;
  try {
    copied = document.execCommand("copy");
  } finally {
    host.removeChild(area);
    previous?.focus({ preventScroll: true });
  }
  if (!copied) throw new Error("copy command was rejected");
}
