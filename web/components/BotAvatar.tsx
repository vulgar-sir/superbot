import { avatarHue, cn } from "@/lib/format";

const avatarColors: Record<string, string> = {
  black: "#1c1c1c",
  brown: "#9d6937",
  red: "#e72e3b",
  orange: "#ee620d",
  yellow: "#da830e",
  green: "#10ab61",
  cyan: "#10a693",
  blue: "#1680f3",
  violet: "#8351e4",
  magenta: "#e52f8b",
  gray: "#6f737a",
};

const avatarShapes: Record<string, string> = {
  circle: "9999px",
  square: "10px",
  pill: "9999px",
  blob: "42% 58% 55% 45% / 48% 44% 56% 52%",
  cloud: "45% 45% 50% 50% / 55% 55% 45% 45%",
  flower: "38% 62% 42% 58% / 55% 40% 60% 45%",
  drop: "58% 42% 52% 48% / 42% 52% 48% 58%",
  hexagon: "28%",
  star: "38% 62% 45% 55% / 55% 45% 62% 38%",
  oval: "50% / 38%",
  arch: "50% 50% 12px 12px",
  shield: "50% 50% 50% 50% / 14% 14% 88% 88%",
  burst: "40% 60% 45% 55% / 55% 40% 60% 45%",
  wave: "45% 55% 60% 40% / 40% 45% 55% 60%",
  pebble: "62% 38% 48% 52% / 42% 58% 42% 58%",
};

export function BotAvatar({
  name,
  id,
  size = 36,
  className,
  shape,
  color,
  avatar,
  rounded = true,
}: {
  name: string;
  id: string;
  size?: number;
  className?: string;
  shape?: string;
  color?: string;
  avatar?: string;
  rounded?: boolean;
}) {
  const hue = avatarHue(id);
  const base = (color && avatarColors[color]) || `hsl(${hue} 58% 46%)`;
  const eyeWidth = Math.max(1.5, size * 0.11);
  const eyeHeight = Math.max(2.5, size * 0.2);
  const gap = Math.max(1.5, size * 0.1);
  const radius = rounded ? (shape && avatarShapes[shape]) || "10px" : undefined;
  if (avatar) {
    return (
      <div
        className={cn("relative shrink-0 overflow-hidden", className)}
        style={{ width: size, height: size, borderRadius: radius, background: base }}
        title={name}
      >
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={avatar}
          alt=""
          width={size}
          height={size}
          className="h-full w-full object-cover"
          draggable={false}
        />
      </div>
    );
  }
  return (
    <div
      className={cn("relative flex shrink-0 items-center justify-center", className)}
      style={{
        width: size,
        height: size,
        borderRadius: rounded ? (shape && avatarShapes[shape]) || "10px" : undefined,
        background: base,
        boxShadow:
          "inset 0 0.5px 0 rgba(255,255,255,0.18), inset 0 -0.5px 0 rgba(0,0,0,0.08)",
      }}
      title={name}
      aria-hidden
    >
      <span className="flex items-center justify-center" style={{ gap }}>
        <span
          className="rounded-full bg-white/90"
          style={{ width: eyeWidth, height: eyeHeight }}
        />
        <span
          className="rounded-full bg-white/90"
          style={{ width: eyeWidth, height: eyeHeight }}
        />
      </span>
    </div>
  );
}
