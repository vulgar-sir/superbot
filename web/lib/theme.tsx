"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";

export type ThemeMode = "light" | "dark" | "system";
export type AccentName = "green" | "blue" | "violet" | "orange" | "black";

const ACCENTS: Record<AccentName, { bubble: string; hover: string; strong: string }> = {
  green: { bubble: "#0d9655", hover: "#0b8550", strong: "#10ab61" },
  blue: { bubble: "#1680f3", hover: "#126fd4", strong: "#1680f3" },
  violet: { bubble: "#8351e4", hover: "#7442d1", strong: "#8351e4" },
  orange: { bubble: "#ee620d", hover: "#d2550a", strong: "#ee620d" },
  black: { bubble: "#1c1c1c", hover: "#333333", strong: "#1c1c1c" },
};

type ThemeContextValue = {
  mode: ThemeMode;
  resolved: "light" | "dark";
  accent: AccentName;
  setMode: (mode: ThemeMode) => void;
  setAccent: (accent: AccentName) => void;
};

const ThemeContext = createContext<ThemeContextValue | null>(null);

const THEME_KEY = "superbot-theme";
const ACCENT_KEY = "superbot-accent";

function readMode(): ThemeMode {
  if (typeof window === "undefined") return "light";
  const value = window.localStorage.getItem(THEME_KEY);
  return value === "dark" || value === "light" || value === "system" ? value : "light";
}

function readAccent(): AccentName {
  if (typeof window === "undefined") return "green";
  const value = window.localStorage.getItem(ACCENT_KEY);
  return value && value in ACCENTS ? (value as AccentName) : "green";
}

function resolve(mode: ThemeMode): "light" | "dark" {
  if (mode !== "system") return mode;
  if (typeof window === "undefined") return "light";
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function apply(mode: ThemeMode, accent: AccentName) {
  if (typeof document === "undefined") return;
  const resolved = resolve(mode);
  document.documentElement.dataset.theme = resolved;
  const palette = ACCENTS[accent];
  document.documentElement.style.setProperty("--sb-user-bubble", palette.bubble);
  document.documentElement.style.setProperty("--sb-user-bubble-hover", palette.hover);
  document.documentElement.style.setProperty("--sb-success", palette.strong);
}

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>("light");
  const [accent, setAccentState] = useState<AccentName>("green");

  useEffect(() => {
    const nextMode = readMode();
    const nextAccent = readAccent();
    setModeState(nextMode);
    setAccentState(nextAccent);
    apply(nextMode, nextAccent);
  }, []);

  useEffect(() => {
    if (mode !== "system" || typeof window === "undefined") return;
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const listener = () => apply("system", accent);
    media.addEventListener("change", listener);
    return () => media.removeEventListener("change", listener);
  }, [mode, accent]);

  const setMode = useCallback((next: ThemeMode) => {
    setModeState(next);
    if (typeof window !== "undefined") window.localStorage.setItem(THEME_KEY, next);
    apply(next, accent);
  }, [accent]);

  const setAccent = useCallback((next: AccentName) => {
    setAccentState(next);
    if (typeof window !== "undefined") window.localStorage.setItem(ACCENT_KEY, next);
    apply(mode, next);
  }, [mode]);

  const value = useMemo<ThemeContextValue>(
    () => ({ mode, resolved: resolve(mode), accent, setMode, setAccent }),
    [mode, accent, setMode, setAccent],
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeContextValue {
  const value = useContext(ThemeContext);
  if (!value) {
    return {
      mode: "light",
      resolved: "light",
      accent: "green",
      setMode: () => undefined,
      setAccent: () => undefined,
    };
  }
  return value;
}

export const ACCENT_OPTIONS = ACCENTS;
