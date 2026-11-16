import { cn } from "@/lib/format";

export function Logo({
  compact = false,
  className,
  tone = "default",
}: {
  compact?: boolean;
  className?: string;
  tone?: "default" | "inverse";
}) {
  const color = tone === "inverse" ? "#ffffff" : "var(--sb-text)";
  return (
    <div className={cn("flex items-center gap-2", className)}>
      <div
        className="relative flex h-8 w-8 items-center justify-center rounded-[10px]"
        style={{ background: color }}
      >
        <svg viewBox="0 0 24 24" className="h-[18px] w-[18px]" fill="none" aria-hidden>
          <path
            d="M5 8.4 12 4.6l7 3.8v7.2L12 19.4l-7-3.8V8.4Z"
            stroke={tone === "inverse" ? "#1c1c1c" : "#ffffff"}
            strokeWidth="1.7"
            strokeLinejoin="round"
          />
          <circle cx="12" cy="12" r="2.1" fill={tone === "inverse" ? "#1c1c1c" : "#ffffff"} />
        </svg>
      </div>
      {!compact && (
        <span
          className="text-[15px] font-semibold tracking-[-0.02em]"
          style={{ color: tone === "inverse" ? "#fff" : "var(--sb-text)" }}
        >
          superbot
        </span>
      )}
    </div>
  );
}
