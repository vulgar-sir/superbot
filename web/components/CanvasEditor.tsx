"use client";

import { useLanguage } from "./LanguageProvider";
import { useEffect, useRef, useState } from "react";
import { cn } from "@/lib/format";
import { ArrowDown, ArrowUp, ArrowUpRight, Circle, Download, Eraser, Image as ImageIcon, MousePointer2, Pencil, Pentagon, Redo2, Square, Trash2, Triangle, Type, Undo2 } from "lucide-react";

const COLORS = ["#111827", "#2563eb", "#059669", "#dc2626", "#d97706", "#7c3aed"];
const CANVAS_WIDTH = 1280;
const CANVAS_HEIGHT = 800;

type Point = { x: number; y: number };

type CanvasObject =
  | { id: string; name?: string; type: "path"; points: Point[]; color: string; size: number; rotation?: number; fill?: string; dashed?: boolean; opacity?: number }
  | { id: string; name?: string; type: "rect" | "ellipse" | "arrow" | "triangle"; x: number; y: number; w: number; h: number; color: string; size: number; rotation?: number; fill?: string; dashed?: boolean; opacity?: number }
  | { id: string; name?: string; type: "text"; x: number; y: number; text: string; color: string; size: number; rotation?: number; fill?: string; dashed?: boolean; opacity?: number; bold?: boolean; italic?: boolean; fontSize?: number }
  | { id: string; name?: string; type: "image"; x: number; y: number; w: number; h: number; src: string; color: string; size: number; rotation?: number; opacity?: number; dashed?: boolean; fill?: string }
  | { id: string; name?: string; type: "polygon"; points: Point[]; color: string; size: number; rotation?: number; opacity?: number; dashed?: boolean; fill?: string };

type DocumentContent = { version: 1; objects: CanvasObject[]; background?: string };

type Tool = "select" | "pen" | "rect" | "ellipse" | "arrow" | "triangle" | "polygon" | "text";

function parseContent(value: string): DocumentContent {
  if (!value) return { version: 1, objects: [] };
  try {
    const parsed = JSON.parse(value) as DocumentContent;
    if (parsed && Array.isArray(parsed.objects)) return parsed;
  } catch {
    // legacy PNG data URL
  }
  return { version: 1, objects: [], background: value.startsWith("data:image/") ? value : undefined };
}

function bounds(object: CanvasObject) {
  if (object.type === "path" || object.type === "polygon") {
    const xs = object.points.map((point) => point.x);
    const ys = object.points.map((point) => point.y);
    return { x: Math.min(...xs), y: Math.min(...ys), w: Math.max(...xs) - Math.min(...xs), h: Math.max(...ys) - Math.min(...ys) };
  }
  if (object.type === "text") {
    const font = object.fontSize ?? object.size * 5;
    return { x: object.x, y: object.y - font, w: object.text.length * font * 0.62, h: font * 1.2 };
  }
  return { x: Math.min(object.x, object.x + object.w), y: Math.min(object.y, object.y + object.h), w: Math.abs(object.w), h: Math.abs(object.h) };
}

function translate(object: CanvasObject, dx: number, dy: number): CanvasObject {
  if (object.type === "path" || object.type === "polygon") {
    return { ...object, points: object.points.map((point) => ({ x: point.x + dx, y: point.y + dy })) };
  }
  return { ...object, x: object.x + dx, y: object.y + dy };
}

function newID() {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") return crypto.randomUUID();
  return `obj-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

const HANDLES = ["nw", "n", "ne", "e", "se", "s", "sw", "w"] as const;

function handlePosition(box: { x: number; y: number; w: number; h: number }, handle: string): Point {
  const x = handle.includes("w") ? box.x : handle.includes("e") ? box.x + box.w : box.x + box.w / 2;
  const y = handle.includes("n") ? box.y : handle.includes("s") ? box.y + box.h : box.y + box.h / 2;
  return { x, y };
}

function handleAt(box: { x: number; y: number; w: number; h: number }, point: Point): string | null {
  if (Math.abs(point.x - (box.x + box.w / 2)) <= 14 && Math.abs(point.y - (box.y - 30)) <= 14) return "rotate";
  for (const handle of HANDLES) {
    const position = handlePosition(box, handle);
    if (Math.abs(position.x - point.x) <= 24 && Math.abs(position.y - point.y) <= 24) return handle;
  }
  return null;
}

function hit(object: CanvasObject, point: Point) {
  const box = bounds(object);
  return point.x >= box.x - 6 && point.x <= box.x + box.w + 6 && point.y >= box.y - 6 && point.y <= box.y + box.h + 6;
}

type RemoteCursor = { id: string; x: number; y: number; actor?: string };

export function CanvasEditor({
  value,
  onChange,
  remoteCursors = [],
  onCursor,
}: {
  value: string;
  onChange: (value: string) => void;
  remoteCursors?: RemoteCursor[];
  onCursor?: (x: number, y: number) => void;
}) {
  const { t } = useLanguage();
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const contentRef = useRef<DocumentContent>(parseContent(value));
  const lastEmitted = useRef(value);
  const drawing = useRef(false);
  const startPoint = useRef<Point | null>(null);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const backgroundImage = useRef<HTMLImageElement | null>(null);
  const imageCache = useRef<Map<string, HTMLImageElement>>(new Map());
  const imageInputRef = useRef<HTMLInputElement | null>(null);
  const history = useRef<string[]>([value]);
  const historyIndex = useRef(0);
  const resizing = useRef<{ id: string; handle: string; start: Point; box: { x: number; y: number; w: number; h: number }; vertexIndex?: number } | null>(null);
  const [tool, setTool] = useState<Tool>("pen");
  const [layersOpen, setLayersOpen] = useState(false);
  const [polygonDraft, setPolygonDraft] = useState<Point[]>([]);
  const [, setRevision] = useState(0);
  const [color, setColor] = useState(COLORS[0]);
  const [size, setSize] = useState(3);

  const render = () => {
    const canvas = canvasRef.current;
    const context = canvas?.getContext("2d");
    if (!canvas || !context) return;
    context.fillStyle = "#ffffff";
    context.fillRect(0, 0, canvas.width, canvas.height);
    if (backgroundImage.current) context.drawImage(backgroundImage.current, 0, 0, canvas.width, canvas.height);
    for (const object of contentRef.current.objects) {
      context.save();
      const box = bounds(object);
      const rotation = ((object.rotation || 0) * Math.PI) / 180;
      if (rotation !== 0) {
        const cx = box.x + box.w / 2;
        const cy = box.y + box.h / 2;
        context.translate(cx, cy);
        context.rotate(rotation);
        context.translate(-cx, -cy);
      }
      context.globalAlpha = object.opacity ?? 1;
      context.strokeStyle = object.color;
      context.lineWidth = object.size;
      context.lineCap = "round";
      context.lineJoin = "round";
      context.setLineDash(object.dashed ? [10, 6] : []);
      if (object.type === "path") {
        context.beginPath();
        object.points.forEach((point, index) => {
          if (index === 0) context.moveTo(point.x, point.y);
          else context.lineTo(point.x, point.y);
        });
        context.stroke();
      } else if (object.type === "rect") {
        if (object.fill) {
          context.fillStyle = object.fill;
          context.fillRect(object.x, object.y, object.w, object.h);
        }
        context.strokeRect(object.x, object.y, object.w, object.h);
      } else if (object.type === "ellipse") {
        context.beginPath();
        context.ellipse(object.x + object.w / 2, object.y + object.h / 2, Math.abs(object.w / 2), Math.abs(object.h / 2), 0, 0, Math.PI * 2);
        if (object.fill) {
          context.fillStyle = object.fill;
          context.fill();
        }
        context.stroke();
      } else if (object.type === "polygon") {
        context.beginPath();
        object.points.forEach((point, index) => {
          if (index === 0) context.moveTo(point.x, point.y);
          else context.lineTo(point.x, point.y);
        });
        context.closePath();
        if (object.fill) {
          context.fillStyle = object.fill;
          context.fill();
        }
        context.stroke();
      } else if (object.type === "image") {
        const image = imageCache.current.get(object.src);
        if (image) {
          context.drawImage(image, object.x, object.y, object.w, object.h);
        } else {
          const loaded = new Image();
          loaded.onload = () => {
            imageCache.current.set(object.src, loaded);
            render();
          };
          loaded.src = object.src;
          imageCache.current.set(object.src, loaded);
        }
      } else if (object.type === "triangle") {
        context.beginPath();
        context.moveTo(object.x + object.w / 2, object.y);
        context.lineTo(object.x + object.w, object.y + object.h);
        context.lineTo(object.x, object.y + object.h);
        context.closePath();
        if (object.fill) {
          context.fillStyle = object.fill;
          context.fill();
        }
        context.stroke();
      } else if (object.type === "arrow") {
        const x2 = object.x + object.w;
        const y2 = object.y + object.h;
        context.beginPath();
        context.moveTo(object.x, object.y);
        context.lineTo(x2, y2);
        context.stroke();
        const angle = Math.atan2(object.h, object.w);
        const length = 14;
        context.beginPath();
        context.moveTo(x2, y2);
        context.lineTo(x2 - length * Math.cos(angle - Math.PI / 6), y2 - length * Math.sin(angle - Math.PI / 6));
        context.lineTo(x2 - length * Math.cos(angle + Math.PI / 6), y2 - length * Math.sin(angle + Math.PI / 6));
        context.closePath();
        context.fillStyle = object.color;
        context.fill();
      } else if (object.type === "text") {
        const font = object.fontSize ?? object.size * 5;
        context.fillStyle = object.color;
        context.font = `${object.italic ? "italic " : ""}${object.bold ? "bold " : ""}${font}px sans-serif`;
        context.fillText(object.text, object.x, object.y);
      }
      context.restore();
    }
    if (polygonDraft.length > 0) {
      context.save();
      context.strokeStyle = color;
      context.lineWidth = size;
      context.setLineDash([6, 4]);
      context.beginPath();
      polygonDraft.forEach((point, index) => {
        if (index === 0) context.moveTo(point.x, point.y);
        else context.lineTo(point.x, point.y);
      });
      context.stroke();
      context.restore();
    }
    if (selectedID) {
      const object = contentRef.current.objects.find((item) => item.id === selectedID);
      if (object) {
        const box = bounds(object);
        context.save();
        context.strokeStyle = "#2563eb";
        context.setLineDash([5, 4]);
        context.lineWidth = 1;
        context.strokeRect(box.x - 4, box.y - 4, box.w + 8, box.h + 8);
        for (const handle of HANDLES) {
          const position = handlePosition(box, handle);
          context.fillStyle = "#ffffff";
          context.fillRect(position.x - 5, position.y - 5, 10, 10);
          context.strokeStyle = "#2563eb";
          context.strokeRect(position.x - 5, position.y - 5, 10, 10);
        }
        if (object.type === "polygon") {
          for (const vertex of object.points) {
            context.beginPath();
            context.arc(vertex.x, vertex.y, 6, 0, Math.PI * 2);
            context.fillStyle = "#ffffff";
            context.fill();
            context.strokeStyle = "#2563eb";
            context.stroke();
          }
        }
        const centerX = box.x + box.w / 2;
        context.beginPath();
        context.moveTo(centerX, box.y);
        context.lineTo(centerX, box.y - 30);
        context.stroke();
        context.beginPath();
        context.arc(centerX, box.y - 30, 6, 0, Math.PI * 2);
        context.fillStyle = "#ffffff";
        context.fill();
        context.stroke();
        context.restore();
      }
    }
  };

  const emit = () => {
    const encoded = JSON.stringify(contentRef.current);
    const current = history.current[historyIndex.current];
    if (encoded !== current) {
      history.current = [...history.current.slice(0, historyIndex.current + 1), encoded].slice(-60);
      historyIndex.current = history.current.length - 1;
    }
    lastEmitted.current = encoded;
    onChange(encoded);
    render();
  };

  const applyHistory = (index: number) => {
    if (index < 0 || index >= history.current.length) return;
    historyIndex.current = index;
    const encoded = history.current[index];
    lastEmitted.current = encoded;
    contentRef.current = parseContent(encoded);
    setSelectedID(null);
    onChange(encoded);
    render();
  };

  useEffect(() => {
    if (value === lastEmitted.current) return;
    const canvas = canvasRef.current;
    if (!canvas) return;
    contentRef.current = parseContent(value);
    history.current = [value];
    historyIndex.current = 0;
    setSelectedID(null);
    if (contentRef.current.background) {
      const image = new Image();
      image.onload = () => {
        backgroundImage.current = image;
        render();
      };
      image.src = contentRef.current.background;
    } else {
      backgroundImage.current = null;
      render();
    }
  }, [value]);

  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "z") {
        event.preventDefault();
        applyHistory(event.shiftKey ? historyIndex.current + 1 : historyIndex.current - 1);
        return;
      }
      if (event.key === "Enter" && polygonDraft.length >= 3) {
        event.preventDefault();
        finishPolygon();
        return;
      }
      if (event.key === "Escape" && polygonDraft.length > 0) {
        setPolygonDraft([]);
        render();
        return;
      }
      if ((event.key === "Delete" || event.key === "Backspace") && selectedID) {
        contentRef.current.objects = contentRef.current.objects.filter((object) => object.id !== selectedID);
        setSelectedID(null);
        emit();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [selectedID, polygonDraft, color, size]);

  const point = (event: React.PointerEvent<HTMLCanvasElement>): Point => {
    const canvas = canvasRef.current!;
    const rect = canvas.getBoundingClientRect();
    return {
      x: ((event.clientX - rect.left) / rect.width) * canvas.width,
      y: ((event.clientY - rect.top) / rect.height) * canvas.height,
    };
  };

  const start = (event: React.PointerEvent<HTMLCanvasElement>) => {
    event.currentTarget.setPointerCapture(event.pointerId);
    const next = point(event);
    drawing.current = true;
    startPoint.current = next;
    if (tool === "polygon") {
      setPolygonDraft((prev) => [...prev, next]);
      drawing.current = false;
      render();
      return;
    }
    if (tool === "select") {
      const selected = contentRef.current.objects.find((object) => object.id === selectedID);
      if (selected && selected.type === "polygon") {
        const vertexIndex = selected.points.findIndex((point) => Math.abs(point.x - next.x) <= 12 && Math.abs(point.y - next.y) <= 12);
        if (vertexIndex >= 0) {
          resizing.current = { id: selected.id, handle: "vertex", start: next, box: bounds(selected), vertexIndex };
          return;
        }
      }
      if (selected) {
        const box = bounds(selected);
        const handle = handleAt(box, next);
        if (handle) {
          resizing.current = { id: selected.id, handle, start: next, box };
          return;
        }
      }
      const found = [...contentRef.current.objects].reverse().find((object) => hit(object, next));
      setSelectedID(found?.id ?? null);
      startPoint.current = next;
      render();
      return;
    }
    const id = newID();
    if (tool === "text") {
      const text = window.prompt(t("canvas.enterText"), t("canvas.defaultText"));
      if (text?.trim()) {
        contentRef.current.objects.push({ id, type: "text", x: next.x, y: next.y, text: text.trim(), color, size });
        setSelectedID(id);
        emit();
      }
      drawing.current = false;
      return;
    }
    if (tool === "pen") {
      contentRef.current.objects.push({ id, type: "path", points: [next], color, size });
    } else if (tool === "arrow" || tool === "triangle") {
      contentRef.current.objects.push({ id, type: tool, x: next.x, y: next.y, w: 0, h: 0, color, size });
    } else {
      contentRef.current.objects.push({ id, type: tool, x: next.x, y: next.y, w: 0, h: 0, color, size });
    }
    setSelectedID(id);
    render();
  };

  const move = (event: React.PointerEvent<HTMLCanvasElement>) => {
    const next = point(event);
    onCursor?.(next.x, next.y);
    if (!drawing.current) return;
    if (resizing.current) {
      const state = resizing.current;
      const object = contentRef.current.objects.find((item) => item.id === state.id);
      if (!object) return;
      if (state.handle === "vertex") {
        if (object.type !== "polygon" || state.vertexIndex === undefined) return;
        object.points[state.vertexIndex] = next;
        render();
        return;
      }
      if (state.handle === "rotate") {
        const cx = state.box.x + state.box.w / 2;
        const cy = state.box.y + state.box.h / 2;
        object.rotation = (Math.atan2(next.y - cy, next.x - cx) * 180) / Math.PI + 90;
        render();
        return;
      }
      if (object.type === "path" || object.type === "text" || object.type === "polygon") return;
      let { x, y, w, h } = state.box;
      const dx = next.x - state.start.x;
      const dy = next.y - state.start.y;
      if (state.handle.includes("w")) { x += dx; w -= dx; }
      if (state.handle.includes("e")) { w += dx; }
      if (state.handle.includes("n")) { y += dy; h -= dy; }
      if (state.handle.includes("s")) { h += dy; }
      if (w < 4) { x += w; w = 4; }
      if (h < 4) { y += h; h = 4; }
      object.x = x;
      object.y = y;
      object.w = w;
      object.h = h;
      render();
      return;
    }
    if (tool === "select") {
      const id = selectedID;
      const object = contentRef.current.objects.find((item) => item.id === id);
      if (!object || !startPoint.current) return;
      const dx = next.x - startPoint.current.x;
      const dy = next.y - startPoint.current.y;
      if (dx === 0 && dy === 0) return;
      const index = contentRef.current.objects.indexOf(object);
      contentRef.current.objects[index] = translate(object, dx, dy);
      startPoint.current = next;
      render();
      return;
    }
    const object = contentRef.current.objects[contentRef.current.objects.length - 1];
    if (!object) return;
    if (object.type === "path") {
      object.points.push(next);
    } else if (object.type !== "text" && object.type !== "polygon") {
      object.w = next.x - object.x;
      object.h = next.y - object.y;
    }
    render();
  };

  const stop = () => {
    if (!drawing.current) return;
    drawing.current = false;
    startPoint.current = null;
    const wasResizing = Boolean(resizing.current);
    resizing.current = null;
    if (wasResizing) {
      emit();
      setRevision((value) => value + 1);
      return;
    }
    emit();
  };

  const selectedObject = selectedID
    ? contentRef.current.objects.find((object) => object.id === selectedID) ?? null
    : null;
  const selectedBox = selectedObject ? bounds(selectedObject) : null;

  const updateSelected = (patch: Record<string, number | string | boolean>) => {
    if (!selectedID) return;
    const index = contentRef.current.objects.findIndex((object) => object.id === selectedID);
    if (index < 0) return;
    const remaining = { ...patch };
    let object = contentRef.current.objects[index];
    if (object.type === "path" && (remaining.x !== undefined || remaining.y !== undefined)) {
      const box = bounds(object);
      const dx = typeof remaining.x === "number" ? remaining.x - box.x : 0;
      const dy = typeof remaining.y === "number" ? remaining.y - box.y : 0;
      object = translate(object, dx, dy);
      delete remaining.x;
      delete remaining.y;
    }
    contentRef.current.objects[index] = { ...object, ...remaining } as CanvasObject;
    emit();
    setRevision((value) => value + 1);
  };

  const finishPolygon = () => {
    if (polygonDraft.length < 3) return;
    const id = newID();
    contentRef.current.objects.push({
      id, type: "polygon", points: polygonDraft, color, size,
      fill: "", dashed: false, opacity: 1,
    });
    setPolygonDraft([]);
    setSelectedID(id);
    emit();
  };

  const removeSelected = () => {
    if (!selectedID) return;
    contentRef.current.objects = contentRef.current.objects.filter((object) => object.id !== selectedID);
    setSelectedID(null);
    emit();
  };

  const moveObject = (id: string, delta: number) => {
    const index = contentRef.current.objects.findIndex((object) => object.id === id);
    const target = index + delta;
    if (index < 0 || target < 0 || target >= contentRef.current.objects.length) return;
    const [object] = contentRef.current.objects.splice(index, 1);
    contentRef.current.objects.splice(target, 0, object);
    emit();
    setRevision((value) => value + 1);
  };

  const moveLayer = (delta: number) => {
    if (selectedID) moveObject(selectedID, delta);
  };

  const renameObject = (id: string) => {
    const object = contentRef.current.objects.find((item) => item.id === id);
    if (!object) return;
    const name = window.prompt(t("canvas.layerName"), object.name || object.type);
    if (!name?.trim()) return;
    object.name = name.trim();
    emit();
    setRevision((value) => value + 1);
  };

  const typeLabel = (object: CanvasObject) => {
    switch (object.type) {
      case "path": return t("canvas.pen");
      case "rect": return t("canvas.rect");
      case "ellipse": return t("canvas.ellipse");
      case "arrow": return t("canvas.arrow");
      case "triangle": return t("canvas.triangle");
      case "polygon": return t("canvas.polygon");
      case "image": return t("canvas.image");
      default: return t("canvas.text");
    }
  };

  const addImage = (file: File) => {
    const reader = new FileReader();
    reader.onload = () => {
      const src = String(reader.result || "");
      if (!src) return;
      const image = new Image();
      image.onload = () => {
        const maxWidth = 480;
        const scale = Math.min(1, maxWidth / image.width);
        const id = newID();
        contentRef.current.objects.push({
          id, type: "image", x: 120, y: 100,
          w: Math.max(80, image.width * scale), h: Math.max(60, image.height * scale),
          src, color: "#111827", size: 1,
        });
        imageCache.current.set(src, image);
        setSelectedID(id);
        emit();
      };
      image.src = src;
    };
    reader.readAsDataURL(file);
  };

  const exportSVG = () => {
    const parts: string[] = [`<svg xmlns="http://www.w3.org/2000/svg" width="${CANVAS_WIDTH}" height="${CANVAS_HEIGHT}"><rect width="100%" height="100%" fill="#fff"/>`];
    for (const object of contentRef.current.objects) {
      const box = bounds(object);
      const cx = box.x + box.w / 2;
      const cy = box.y + box.h / 2;
      const transform = object.rotation ? ` transform="rotate(${object.rotation} ${cx} ${cy})"` : "";
      const opacity = object.opacity !== undefined && object.opacity !== 1 ? ` opacity="${object.opacity}"` : "";
      const dash = object.dashed ? ` stroke-dasharray="10 6"` : "";
      const style = `fill="${object.fill || "none"}" stroke="${object.color}" stroke-width="${object.size}"${dash}${opacity}`;
      if (object.type === "path") {
        parts.push(`<polyline points="${object.points.map((point) => `${point.x},${point.y}`).join(" ")}" ${style} stroke-linecap="round" stroke-linejoin="round"${transform}/>`);
      } else if (object.type === "rect") {
        parts.push(`<rect x="${object.x}" y="${object.y}" width="${object.w}" height="${object.h}" ${style}${transform}/>`);
      } else if (object.type === "ellipse") {
        parts.push(`<ellipse cx="${object.x + object.w / 2}" cy="${object.y + object.h / 2}" rx="${Math.abs(object.w / 2)}" ry="${Math.abs(object.h / 2)}" ${style}${transform}/>`);
      } else if (object.type === "image") {
        parts.push(`<image href="${object.src}" x="${object.x}" y="${object.y}" width="${object.w}" height="${object.h}"${transform}/>`);
      } else if (object.type === "polygon") {
        parts.push(`<polygon points="${object.points.map((point) => `${point.x},${point.y}`).join(" ")}" ${style}${transform}/>`);
      } else if (object.type === "triangle") {
        parts.push(`<polygon points="${object.x + object.w / 2},${object.y} ${object.x + object.w},${object.y + object.h} ${object.x},${object.y + object.h}" ${style}${transform}/>`);
      } else if (object.type === "arrow") {
        const x2 = object.x + object.w;
        const y2 = object.y + object.h;
        parts.push(`<line x1="${object.x}" y1="${object.y}" x2="${x2}" y2="${y2}" ${style}${transform}/><circle cx="${x2}" cy="${y2}" r="4" fill="${object.color}"${transform}/>`);
      } else if (object.type === "text") {
        const font = object.fontSize ?? object.size * 5;
        const weight = object.bold ? ` font-weight="bold"` : "";
        const italic = object.italic ? ` font-style="italic"` : "";
        parts.push(`<text x="${object.x}" y="${object.y}" fill="${object.color}" font-size="${font}" font-family="sans-serif"${weight}${italic}${transform}>${object.text.replace(/&/g, "&amp;").replace(/</g, "&lt;")}</text>`);
      }
    }
    parts.push("</svg>");
    const blob = new Blob([parts.join("")], { type: "image/svg+xml" });
    const link = document.createElement("a");
    link.download = "canvas.svg";
    link.href = URL.createObjectURL(blob);
    link.click();
    URL.revokeObjectURL(link.href);
  };

  const clear = () => {
    contentRef.current = { version: 1, objects: [] };
    backgroundImage.current = null;
    setSelectedID(null);
    emit();
  };

  const tools: Array<{ key: Tool; icon: typeof Pencil; label: string }> = [
    { key: "select", icon: MousePointer2, label: t("canvas.select") },
    { key: "pen", icon: Pencil, label: t("canvas.pen") },
    { key: "rect", icon: Square, label: t("canvas.rect") },
    { key: "ellipse", icon: Circle, label: t("canvas.ellipse") },
    { key: "arrow", icon: ArrowUpRight, label: t("canvas.arrow") },
    { key: "triangle", icon: Triangle, label: t("canvas.triangle") },
    { key: "polygon", icon: Pentagon, label: t("canvas.polygon") },
    { key: "text", icon: Type, label: t("canvas.text") },
  ];

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b border-[var(--sb-border)] px-4 py-2">
        {tools.map((entry) => {
          const Icon = entry.icon;
          return (
            <button
              key={entry.key}
              type="button"
              onClick={() => setTool(entry.key)}
              aria-label={entry.label}
              title={entry.label}
              className="flex h-7 w-7 items-center justify-center rounded-md"
              style={{ background: tool === entry.key ? "var(--sb-hover)" : "transparent" }}
            >
              <Icon className="h-4 w-4" />
            </button>
          );
        })}
        <span className="mx-1 h-4 w-px bg-[var(--sb-border)]" />
        {COLORS.map((value) => (
          <button
            key={value}
            type="button"
            onClick={() => setColor(value)}
            aria-label={t("canvas.color", { value })}
            className="h-5 w-5 rounded-full border-2"
            style={{ background: value, borderColor: color === value ? "var(--sb-text)" : "transparent" }}
          />
        ))}
        <input type="range" min={1} max={20} value={size} onChange={(event) => setSize(Number(event.target.value))} className="ml-1 w-20" />
        <button type="button" onClick={() => moveLayer(1)} className="sb-btn" data-variant="secondary" data-size="sm" aria-label={t("canvas.moveUpAria")}>
          <ArrowUp className="h-3.5 w-3.5" />
          {t("canvas.moveUp")}
        </button>
        <button type="button" onClick={() => moveLayer(-1)} className="sb-btn" data-variant="secondary" data-size="sm" aria-label={t("canvas.moveDownAria")}>
          <ArrowDown className="h-3.5 w-3.5" />
          {t("canvas.moveDown")}
        </button>
        <input
          ref={imageInputRef}
          type="file"
          accept="image/*"
          className="hidden"
          onChange={(event) => {
            const file = event.target.files?.[0];
            event.target.value = "";
            if (file) addImage(file);
          }}
        />
        <button type="button" onClick={() => imageInputRef.current?.click()} className="sb-btn" data-variant="secondary" data-size="sm" aria-label={t("canvas.image")}>
          {t("canvas.image")}
        </button>
        <button type="button" onClick={() => setLayersOpen((value) => !value)} className="sb-btn" data-variant="secondary" data-size="sm" aria-label={t("canvas.layers")}>
          {t("canvas.layers")}
        </button>
        <button type="button" onClick={() => applyHistory(historyIndex.current - 1)} className="sb-btn" data-variant="secondary" data-size="sm" aria-label={t("canvas.undo")}>
          <Undo2 className="h-3.5 w-3.5" />
          {t("canvas.undo")}
        </button>
        <button type="button" onClick={() => applyHistory(historyIndex.current + 1)} className="sb-btn" data-variant="secondary" data-size="sm" aria-label={t("canvas.redo")}>
          <Redo2 className="h-3.5 w-3.5" />
          {t("canvas.redo")}
        </button>
        <button type="button" onClick={removeSelected} className="sb-btn" data-variant="secondary" data-size="sm" aria-label={t("canvas.deleteSelected")}>
          <Trash2 className="h-3.5 w-3.5" />
          {t("canvas.deleteSelected")}
        </button>
        <button type="button" onClick={clear} className="sb-btn" data-variant="secondary" data-size="sm">
          <Eraser className="h-3.5 w-3.5" />
          {t("canvas.clear")}
        </button>
        <button
          type="button"
          onClick={() => {
            const canvas = canvasRef.current;
            if (!canvas) return;
            const link = document.createElement("a");
            link.download = "canvas.png";
            link.href = canvas.toDataURL("image/png");
            link.click();
          }}
          className="sb-btn" data-variant="secondary" data-size="sm" aria-label={t("canvas.exportPng")}
        >
          <Download className="h-3.5 w-3.5" />
          {t("canvas.exportPng")}
        </button>
        <button type="button" onClick={exportSVG} className="sb-btn" data-variant="secondary" data-size="sm" aria-label={t("canvas.exportSvg")}>
          <Download className="h-3.5 w-3.5" />
          {t("canvas.exportSvg")}
        </button>
        <span className="ml-auto text-[10px] text-[var(--sb-text-3)]">{t("canvas.hint")}</span>
      </div>
      {selectedObject && (
        <div className="flex flex-wrap items-center gap-3 border-b border-[var(--sb-border)] px-4 py-2 text-[11px] text-[var(--sb-text-2)]">
          <span>{t("canvas.selected", { type: selectedObject.type })}</span>
          <label className="flex items-center gap-1">X
            <input type="number" className="h-7 w-[64px] rounded-md bg-[var(--sb-hover)] px-1.5" value={Math.round(selectedBox?.x ?? 0)} onChange={(event) => updateSelected({ x: Number(event.target.value) })} />
          </label>
          <label className="flex items-center gap-1">Y
            <input type="number" className="h-7 w-[64px] rounded-md bg-[var(--sb-hover)] px-1.5" value={Math.round(selectedBox?.y ?? 0)} onChange={(event) => updateSelected({ y: Number(event.target.value) })} />
          </label>
          {selectedObject.type !== "path" && selectedObject.type !== "text" && selectedObject.type !== "polygon" && (
            <>
              <label className="flex items-center gap-1">{t("canvas.width")}
                <input type="number" className="h-7 w-[64px] rounded-md bg-[var(--sb-hover)] px-1.5" value={Math.round(selectedObject.w)} onChange={(event) => updateSelected({ w: Number(event.target.value) })} />
              </label>
              <label className="flex items-center gap-1">{t("canvas.height")}
                <input type="number" className="h-7 w-[64px] rounded-md bg-[var(--sb-hover)] px-1.5" value={Math.round(selectedObject.h)} onChange={(event) => updateSelected({ h: Number(event.target.value) })} />
              </label>
            </>
          )}
          <label className="flex items-center gap-1">{t("canvas.rotation")}
            <input type="number" className="h-7 w-[64px] rounded-md bg-[var(--sb-hover)] px-1.5" value={Math.round(selectedObject.rotation ?? 0)} onChange={(event) => updateSelected({ rotation: Number(event.target.value) })} />
          </label>
          <label className="flex items-center gap-1">{t("canvas.opacity")}
            <input type="number" step="0.1" min="0" max="1" className="h-7 w-[64px] rounded-md bg-[var(--sb-hover)] px-1.5" value={selectedObject.opacity ?? 1} onChange={(event) => updateSelected({ opacity: Number(event.target.value) })} />
          </label>
          <label className="flex items-center gap-1">
            <input type="checkbox" checked={Boolean(selectedObject.dashed)} onChange={(event) => updateSelected({ dashed: event.target.checked })} />
            {t("canvas.dashed")}
          </label>
          {selectedObject.type === "text" && (
            <>
              <label className="flex items-center gap-1">{t("canvas.fontSize")}
                <input type="number" className="h-7 w-[64px] rounded-md bg-[var(--sb-hover)] px-1.5" value={Math.round(selectedObject.fontSize ?? selectedObject.size * 5)} onChange={(event) => updateSelected({ fontSize: Number(event.target.value) })} />
              </label>
              <label className="flex items-center gap-1">
                <input type="checkbox" checked={Boolean(selectedObject.bold)} onChange={(event) => updateSelected({ bold: event.target.checked })} />
                {t("canvas.bold")}
              </label>
              <label className="flex items-center gap-1">
                <input type="checkbox" checked={Boolean(selectedObject.italic)} onChange={(event) => updateSelected({ italic: event.target.checked })} />
                {t("canvas.italic")}
              </label>
              <button type="button" onClick={() => {
                if (selectedObject.type !== "text") return;
                const text = window.prompt(t("canvas.editText"), selectedObject.text);
                if (text?.trim()) updateSelected({ text: text.trim() });
              }} className="text-[10px] text-[var(--sb-text-3)]">{t("canvas.editText")}</button>
            </>
          )}
          <label className="flex items-center gap-1">{t("canvas.fill")}
            <input type="color" value={selectedObject.fill || "#ffffff"} onChange={(event) => updateSelected({ fill: event.target.value })} className="h-6 w-7 rounded border-0 bg-transparent p-0" />
            <button type="button" onClick={() => updateSelected({ fill: "" })} className="text-[10px] text-[var(--sb-text-3)]">无</button>
          </label>
        </div>
      )}
      {layersOpen && (
        <div className="max-h-[190px] overflow-y-auto border-b border-[var(--sb-border)] px-4 py-2">
          {contentRef.current.objects.length === 0 ? (
            <div className="py-2 text-[11px] text-[var(--sb-text-3)]">{t("canvas.noLayers")}</div>
          ) : (
            <div className="space-y-1">
              {[...contentRef.current.objects].reverse().map((object) => (
                <div
                  key={object.id}
                  onClick={() => setSelectedID(object.id)}
                  className={cn(
                    "flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-[11px]",
                    selectedID === object.id ? "bg-[var(--sb-hover)]" : "hover:bg-[var(--sb-hover)]",
                  )}
                >
                  <span className="min-w-0 flex-1 truncate">{object.name || typeLabel(object)}</span>
                  <span className="text-[10px] text-[var(--sb-text-3)]">{typeLabel(object)}</span>
                  <button type="button" aria-label={t("canvas.layerUp")} onClick={(event) => { event.stopPropagation(); moveObject(object.id, 1); }} className="sb-icon-btn h-6 w-6"><ArrowUp className="h-3 w-3" /></button>
                  <button type="button" aria-label={t("canvas.layerDown")} onClick={(event) => { event.stopPropagation(); moveObject(object.id, -1); }} className="sb-icon-btn h-6 w-6"><ArrowDown className="h-3 w-3" /></button>
                  <button type="button" aria-label={t("canvas.layerRename")} onClick={(event) => { event.stopPropagation(); renameObject(object.id); }} className="px-1 text-[10px] text-[var(--sb-text-3)]">重命名</button>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
      <div className="relative flex min-h-0 flex-1 items-center justify-center overflow-auto bg-[var(--sb-hover)] p-4">
        <canvas
          ref={canvasRef}
          width={CANVAS_WIDTH}
          height={CANVAS_HEIGHT}
          onPointerDown={start}
          onPointerMove={move}
          onPointerUp={stop}
          onPointerCancel={stop}
          className="max-h-full w-full max-w-[1000px] touch-none rounded-xl bg-white shadow-sm"
        />
        {remoteCursors.map((cursor) => (
          <div
            key={cursor.id}
            className="pointer-events-none absolute z-10 -translate-x-1 -translate-y-1"
            style={{ left: `${(cursor.x / CANVAS_WIDTH) * 100}%`, top: `${(cursor.y / CANVAS_HEIGHT) * 100}%` }}
          >
            <div className="h-2 w-2 rounded-full bg-[#2563eb]" />
            <div className="mt-0.5 whitespace-nowrap rounded bg-[#2563eb] px-1 py-0.5 text-[9px] text-white">
              {cursor.actor ? `${cursor.actor.split("@")[0]}:${cursor.id}` : cursor.id}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
