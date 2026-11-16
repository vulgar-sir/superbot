import { cn } from "@/lib/format";

type Tone = "live" | "idle" | "busy" | "error";

const toneColor: Record<Tone, string> = {
  live: "var(--sb-success)",
  idle: "var(--sb-text-4)",
  busy: "var(--sb-warn)",
  error: "var(--sb-danger)",
};

export function StatusPill({
  tone,
  children,
  pulse = false,
  className,
}: {
  tone: Tone;
  children: React.ReactNode;
  pulse?: boolean;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] leading-[16px]",
        className,
      )}
      style={{ background: "var(--sb-hover)", color: "var(--sb-text-2)" }}
    >
      <span
        className={cn("h-1.5 w-1.5 rounded-full", pulse && "sb-pulse")}
        style={{ background: toneColor[tone] }}
      />
      {children}
    </span>
  );
}
