import type { NotificationSound } from "./types";

let context: AudioContext | null = null;

function audioContext(): AudioContext | null {
  if (typeof window === "undefined") return null;
  const Constructor =
    window.AudioContext ??
    (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
  if (!Constructor) return null;
  context ??= new Constructor();
  if (context.state === "suspended") void context.resume();
  return context;
}

function tone(
  ctx: AudioContext,
  frequency: number,
  start: number,
  duration: number,
  peak: number,
  type: OscillatorType,
) {
  const oscillator = ctx.createOscillator();
  const gain = ctx.createGain();
  oscillator.type = type;
  oscillator.frequency.value = frequency;
  gain.gain.setValueAtTime(0.0001, start);
  gain.gain.exponentialRampToValueAtTime(Math.max(0.0001, peak), start + 0.012);
  gain.gain.exponentialRampToValueAtTime(0.0001, start + duration);
  oscillator.connect(gain).connect(ctx.destination);
  oscillator.start(start);
  oscillator.stop(start + duration + 0.02);
}

export function playNotificationSound(sound: NotificationSound, volume: number) {
  if (sound === "none" || volume <= 0) return;
  const ctx = audioContext();
  if (!ctx) return;
  const now = ctx.currentTime;
  const peak = Math.min(1, Math.max(0, volume)) * 0.22;
  if (sound === "chime") {
    tone(ctx, 880, now, 0.14, peak, "sine");
    tone(ctx, 1174.66, now + 0.1, 0.22, peak * 0.8, "sine");
    return;
  }
  if (sound === "soft") {
    tone(ctx, 523.25, now, 0.28, peak * 0.75, "sine");
    tone(ctx, 659.25, now + 0.05, 0.32, peak * 0.5, "sine");
    return;
  }
  tone(ctx, 784, now, 0.16, peak, "triangle");
  tone(ctx, 1046.5, now + 0.08, 0.18, peak * 0.6, "sine");
}
