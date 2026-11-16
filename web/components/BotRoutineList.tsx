"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Clock, Loader2, MoreHorizontal, Pencil, Play, Plus, Trash2 } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import type { Bot, BotRoutine } from "@/lib/types";
import { Menu } from "./ui/Menu";
import { Modal } from "./ui/Modal";
import { useConfirm } from "./ui/Confirm";
import { useToast } from "./ui/Toast";

const presets = [
  { label: "每天 9:00", value: "0 9 * * *" },
  { label: "工作日 9:00", value: "0 9 * * 1-5" },
  { label: "每小时", value: "@every 1h" },
  { label: "每 30 分钟", value: "@every 30m" },
  { label: "邮件到达", value: "email:" },
];

const extraTimezones = [
  "UTC",
  "Asia/Shanghai",
  "Asia/Tokyo",
  "Asia/Singapore",
  "Europe/London",
  "Europe/Berlin",
  "America/New_York",
  "America/Los_Angeles",
];

type EditorForm = {
  mode: "recurring" | "once";
  name: string;
  schedule: string;
  onceAt: string;
  timezone: string;
  prompt: string;
  timing: "exact" | "flexible";
  enabled: boolean;
  paused: boolean;
};

function browserTimezone(): string {
  try {
    return (
      window.localStorage.getItem("superbot-timezone") ||
      Intl.DateTimeFormat().resolvedOptions().timeZone ||
      "UTC"
    );
  } catch {
    return "UTC";
  }
}

function pad(value: number): string {
  return String(value).padStart(2, "0");
}

function toLocalInput(value: string | Date): string {
  const date = typeof value === "string" ? new Date(value) : value;
  if (Number.isNaN(date.getTime())) return "";
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function defaultOnceAt(): string {
  const date = new Date();
  date.setDate(date.getDate() + 1);
  date.setHours(9, 0, 0, 0);
  return toLocalInput(date);
}

function emptyForm(): EditorForm {
  return {
    mode: "recurring",
    name: "",
    schedule: "0 9 * * *",
    onceAt: defaultOnceAt(),
    timezone: browserTimezone(),
    prompt: "",
    timing: "exact",
    enabled: true,
    paused: false,
  };
}

function formFromRoutine(routine: BotRoutine): EditorForm {
  const isOnce = routine.schedule.trim().startsWith("once:");
  return {
    mode: isOnce ? "once" : "recurring",
    name: routine.name,
    schedule: isOnce ? "0 9 * * *" : routine.schedule,
    onceAt: isOnce ? toLocalInput(routine.schedule.trim().slice(5)) : defaultOnceAt(),
    timezone: routine.timezone || browserTimezone(),
    prompt: routine.prompt,
    timing: routine.timing === "flexible" ? "flexible" : "exact",
    enabled: routine.enabled,
    paused: routine.paused,
  };
}

export function BotRoutineList({ bot }: { bot: Bot }) {
  const { toast } = useToast();
  const { confirm, confirmElement } = useConfirm();
  const [routines, setRoutines] = useState<BotRoutine[]>([]);
  const [loading, setLoading] = useState(true);
  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<BotRoutine | null>(null);
  const [saving, setSaving] = useState(false);
  const [menu, setMenu] = useState<{ routine: BotRoutine; rect: DOMRect } | null>(null);
  const [form, setForm] = useState<EditorForm>(emptyForm);
  const localTimezone = useMemo(() => browserTimezone(), []);
  const timezoneOptions = useMemo(
    () => Array.from(new Set([localTimezone, ...extraTimezones])),
    [localTimezone],
  );

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const result = await api.listBotRoutines(bot.id);
      setRoutines(result.routines);
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "加载例程失败", "danger");
    } finally {
      setLoading(false);
    }
  }, [bot.id, toast]);

  useEffect(() => {
    void load();
  }, [load]);

  const toggle = useCallback(
    async (routine: BotRoutine) => {
      const running = routine.enabled && !routine.paused;
      try {
        const result = await api.updateBotRoutine(
          routine.id,
          running ? { paused: true } : { enabled: true, paused: false },
        );
        setRoutines((prev) => prev.map((item) => (item.id === routine.id ? result.routine : item)));
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "更新例程失败", "danger");
      }
    },
    [toast],
  );

  const remove = useCallback(
    async (routine: BotRoutine) => {
      const ok = await confirm({
        title: `删除例程「${routine.name}」？`,
        description: "删除后不会再自动执行。",
        confirmLabel: "删除",
        danger: true,
      });
      if (!ok) return;
      try {
        await api.deleteBotRoutine(routine.id);
        setRoutines((prev) => prev.filter((item) => item.id !== routine.id));
        toast("已删除", "success");
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "删除例程失败", "danger");
      }
    },
    [confirm, toast],
  );

  const openCreate = useCallback(() => {
    setEditing(null);
    setForm(emptyForm());
    setEditorOpen(true);
  }, []);

  const openEdit = useCallback((routine: BotRoutine) => {
    setEditing(routine);
    setForm(formFromRoutine(routine));
    setEditorOpen(true);
  }, []);

  const save = useCallback(async () => {
    const name = form.name.trim();
    const prompt = form.prompt.trim();
    let schedule = form.schedule.trim();
    if (form.mode === "once") {
      const at = new Date(form.onceAt);
      schedule = form.onceAt && !Number.isNaN(at.getTime()) ? `once:${at.toISOString()}` : "";
    }
    if (!name || !schedule || !prompt) {
      toast(
        form.mode === "once" ? "名称、执行时间和任务内容必填" : "名称、调度和任务内容必填",
        "danger",
      );
      return;
    }
    const timezone = form.timezone.trim() || localTimezone;
    try {
      new Intl.DateTimeFormat("en-US", { timeZone: timezone });
    } catch {
      toast("时区无效，请填写 IANA 时区，例如 Asia/Shanghai", "danger");
      return;
    }
    setSaving(true);
    try {
      if (editing) {
        const result = await api.updateBotRoutine(editing.id, {
          name,
          schedule,
          timezone,
          timing: form.timing,
          prompt,
          enabled: form.enabled,
          paused: form.paused,
        });
        setRoutines((prev) =>
          prev.map((item) => (item.id === result.routine.id ? result.routine : item)),
        );
        toast("例程已更新", "success");
      } else {
        const result = await api.createBotRoutine({
          bot_id: bot.id,
          name,
          schedule,
          timezone,
          timing: form.timing,
          prompt,
          enabled: form.enabled,
        });
        setRoutines((prev) => [result.routine, ...prev]);
        toast("例程已创建", "success");
      }
      setEditorOpen(false);
      setEditing(null);
    } catch (err) {
      const fallback = editing ? "更新例程失败" : "创建例程失败";
      toast(err instanceof ApiError ? err.message : fallback, "danger");
    } finally {
      setSaving(false);
    }
  }, [bot.id, editing, form, localTimezone, toast]);

  return (
    <div>
      <div className="flex items-center justify-between">
        <p className="text-[12px] leading-[18px] text-[var(--sb-text-2)]">
          {routines.length > 0 ? `${routines.length} 个例程` : "还没有例程"}
        </p>
        <button
          type="button"
          onClick={openCreate}
          className="sb-btn"
          data-variant="secondary"
          data-size="sm"
        >
          <Plus className="h-3.5 w-3.5" />
          新建例程
        </button>
      </div>

      {loading ? (
        <div className="flex justify-center py-8">
          <Loader2 className="sb-spin h-5 w-5 text-[var(--sb-text-3)]" />
        </div>
      ) : routines.length === 0 ? (
        <div className="sb-empty">
          <Clock className="h-5 w-5" />
          <div className="sb-empty-title">让它定时自己开工</div>
          <div className="sb-empty-desc">支持 cron、@every、一次性执行，以及暂停和灵活时间窗口。</div>
        </div>
      ) : (
        <div className="mt-3 space-y-2">
          {routines.map((routine) => {
            const running = routine.enabled && !routine.paused;
            return (
              <div
                key={routine.id}
                className="flex items-center gap-3 rounded-xl border border-[var(--sb-border)] px-3.5 py-3"
              >
                <Clock className="h-4 w-4 shrink-0 text-[var(--sb-text-2)]" />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[14px] leading-[20px]">{routine.name}</div>
                  <div className="mt-0.5 flex flex-wrap items-center gap-x-2 text-[12px] leading-[16px] text-[var(--sb-text-2)]">
                    <span>{formatSchedule(routine.schedule, routine.timezone)}</span>
                    <span>{routine.timezone}</span>
                    {routine.timing === "flexible" && <span>灵活</span>}
                    <span>
                      {!routine.enabled
                        ? "已停用"
                        : routine.paused
                          ? "已暂停"
                          : routine.next_run_at
                            ? `下次 ${formatNext(routine.next_run_at, routine.timezone)}`
                            : "待执行"}
                    </span>
                    {routine.last_run_at && (
                      <span>上次 {formatNext(routine.last_run_at, routine.timezone)}</span>
                    )}
                  </div>
                </div>
                <button
                  type="button"
                  className="sb-switch"
                  data-on={running ? "true" : undefined}
                  aria-label={running ? "暂停例程" : "恢复例程"}
                  onClick={() => void toggle(routine)}
                />
                <button
                  type="button"
                  className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--sb-text-3)] transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-text)]"
                  onClick={(event) => setMenu({ routine, rect: event.currentTarget.getBoundingClientRect() })}
                  aria-label="更多操作"
                >
                  <MoreHorizontal className="h-4 w-4" />
                </button>
              </div>
            );
          })}
        </div>
      )}

      <Menu
        open={menu !== null}
        anchor={menu ? { rect: menu.rect } : null}
        items={
          menu
            ? [
                {
                  key: "edit",
                  label: "编辑",
                  icon: <Pencil className="h-3.5 w-3.5" />,
                  onSelect: () => openEdit(menu.routine),
                },
                {
                  key: "toggle",
                  label:
                    menu.routine.enabled && !menu.routine.paused ? "暂停" : "恢复",
                  icon: <Clock className="h-3.5 w-3.5" />,
                  onSelect: () => void toggle(menu.routine),
                },
                {
                  key: "test-run",
                  label: "测试运行",
                  icon: <Play className="h-3.5 w-3.5" />,
                  onSelect: async () => {
                    try {
                      await api.testBotRoutine(menu.routine.id);
                      toast("已触发一次测试运行", "success");
                      await load();
                    } catch (err) {
                      toast(err instanceof ApiError ? err.message : "测试运行失败", "danger");
                    }
                  },
                },
                { key: "sep", separator: true },
                {
                  key: "delete",
                  label: "删除",
                  icon: <Trash2 className="h-3.5 w-3.5" />,
                  danger: true,
                  onSelect: () => void remove(menu.routine),
                },
              ]
            : []
        }
        onClose={() => setMenu(null)}
        align="right"
      />

      <Modal
        open={editorOpen}
        onClose={() => {
          setEditorOpen(false);
          setEditing(null);
        }}
        title={editing ? "编辑例程" : "新建例程"}
        subtitle={`到点后会在 ${bot.name} 的私聊里自动开工。`}
        size="md"
        footer={
          <>
            <button
              type="button"
              className="sb-btn"
              data-variant="secondary"
              onClick={() => {
                setEditorOpen(false);
                setEditing(null);
              }}
            >
              取消
            </button>
            <button
              type="button"
              className="sb-btn"
              data-variant="primary"
              onClick={() => void save()}
              disabled={saving}
            >
              {saving && <Loader2 className="sb-spin h-3.5 w-3.5" />}
              {editing ? "保存" : "创建例程"}
            </button>
          </>
        }
      >
        <div className="space-y-4">
          <label className="block">
            <span className="sb-field-label">名称</span>
            <input
              className="sb-field"
              value={form.name}
              onChange={(event) => setForm((prev) => ({ ...prev, name: event.target.value }))}
              placeholder="例如：每日进度汇报"
            />
          </label>
          <div className="grid gap-3 sm:grid-cols-2">
            <label className="block">
              <span className="sb-field-label">调度类型</span>
              <select
                className="sb-field"
                value={form.mode}
                onChange={(event) =>
                  setForm((prev) => ({
                    ...prev,
                    mode: event.target.value as "recurring" | "once",
                  }))
                }
              >
                <option value="recurring">循环（cron / @every）</option>
                <option value="once">一次性（指定时间）</option>
              </select>
            </label>
            <label className="block">
              <span className="sb-field-label">时区</span>
              <div className="flex items-center gap-1.5">
                <input
                  className="sb-field font-[var(--sb-mono)]"
                  list="bot-routine-timezones"
                  value={form.timezone}
                  onChange={(event) =>
                    setForm((prev) => ({ ...prev, timezone: event.target.value }))
                  }
                  placeholder="例如 Asia/Shanghai"
                />
                <button
                  type="button"
                  onClick={() => setForm((prev) => ({ ...prev, timezone: localTimezone }))}
                  className="sb-btn shrink-0"
                  data-variant="secondary"
                  data-size="sm"
                  title={`使用本机时区 ${localTimezone}`}
                >
                  本机
                </button>
              </div>
              <datalist id="bot-routine-timezones">
                {timezoneOptions.map((zone) => (
                  <option key={zone} value={zone} />
                ))}
              </datalist>
            </label>
          </div>

          {form.mode === "once" ? (
            <label className="block">
              <span className="sb-field-label">执行时间</span>
              <input
                type="datetime-local"
                className="sb-field"
                value={form.onceAt}
                min={toLocalInput(new Date())}
                onChange={(event) =>
                  setForm((prev) => ({ ...prev, onceAt: event.target.value }))
                }
              />
              <span className="mt-1 block text-[11px] leading-[16px] text-[var(--sb-text-3)]">
                按当前设备时区选择绝对时间，执行一次后自动停用。
              </span>
            </label>
          ) : (
            <>
              <label className="block">
                <span className="sb-field-label">调度</span>
                <input
                  className="sb-field font-[var(--sb-mono)]"
                  value={form.schedule}
                  onChange={(event) =>
                    setForm((prev) => ({ ...prev, schedule: event.target.value }))
                  }
                  placeholder="cron 或 @every 1h"
                />
                <span className="mt-1 block text-[11px] leading-[16px] text-[var(--sb-text-3)]">
                  cron 按上方时区计算；@every 为固定间隔；email: 或 email:关键词 在新邮件到达时触发。
                </span>
              </label>
              <div className="flex flex-wrap gap-1.5">
                {presets.map((preset) => (
                  <button
                    key={preset.value}
                    type="button"
                    onClick={() => setForm((prev) => ({ ...prev, schedule: preset.value }))}
                    className="rounded-full bg-[var(--sb-hover)] px-2.5 py-1 text-[12px] text-[var(--sb-text-2)] transition hover:bg-[var(--sb-active)] hover:text-[var(--sb-text)]"
                  >
                    {preset.label}
                  </button>
                ))}
              </div>
            </>
          )}

          <label className="block">
            <span className="sb-field-label">任务内容</span>
            <textarea
              className="sb-field"
              value={form.prompt}
              onChange={(event) => setForm((prev) => ({ ...prev, prompt: event.target.value }))}
              placeholder="到点后要它做什么？写清目标、输出格式和边界。"
            />
          </label>
          <label className="block">
            <span className="sb-field-label">时间精度</span>
            <select
              className="sb-field"
              value={form.timing}
              onChange={(event) =>
                setForm((prev) => ({
                  ...prev,
                  timing: event.target.value as "exact" | "flexible",
                }))
              }
            >
              <option value="exact">准点</option>
              <option value="flexible">灵活（±15 分钟）</option>
            </select>
            {form.timing === "flexible" && (
              <span className="mt-1 block text-[11px] leading-[16px] text-[var(--sb-text-3)]">
                灵活：到点即可执行；若超过 15 分钟才被发现，则跳过本次，避免做过期工作。
              </span>
            )}
          </label>

          {editing && (
            <div className="rounded-xl border border-[var(--sb-border)] p-3">
              <div className="text-[13px] leading-[18px]">Webhook 触发</div>
              <div className="text-[11px] leading-[16px] text-[var(--sb-text-3)]">
                POST 到下面的地址会立即运行一次，无需登录（令牌即凭证）。
              </div>
              <div className="mt-2 flex items-center gap-1.5">
                <input
                  readOnly
                  value={`${typeof window !== "undefined" ? window.location.origin : ""}/api/hooks/routines/${editing.trigger_token}`}
                  className="sb-field min-w-0 flex-1 font-[var(--sb-mono)] text-[11px]"
                  onFocus={(event) => event.currentTarget.select()}
                />
                <button
                  type="button"
                  className="sb-btn shrink-0"
                  data-variant="secondary"
                  data-size="sm"
                  onClick={() => {
                    const url = `${window.location.origin}/api/hooks/routines/${editing.trigger_token}`;
                    void navigator.clipboard.writeText(url).then(
                      () => toast("已复制 Webhook 地址", "success"),
                      () => toast("复制失败，请手动选择", "danger"),
                    );
                  }}
                >
                  复制
                </button>
              </div>
            </div>
          )}

          <div className="space-y-2 rounded-xl border border-[var(--sb-border)] p-3">
            <div className="flex items-center justify-between gap-3">
              <div>
                <div className="text-[13px] leading-[18px]">启用例程</div>
                <div className="text-[11px] leading-[16px] text-[var(--sb-text-3)]">
                  关闭后不再排期执行。
                </div>
              </div>
              <button
                type="button"
                className="sb-switch"
                data-on={form.enabled ? "true" : undefined}
                aria-label={form.enabled ? "停用例程" : "启用例程"}
                onClick={() => setForm((prev) => ({ ...prev, enabled: !prev.enabled }))}
              />
            </div>
            {editing && (
              <div className="flex items-center justify-between gap-3 border-t border-[var(--sb-border)] pt-2">
                <div>
                  <div className="text-[13px] leading-[18px]">暂停</div>
                  <div className="text-[11px] leading-[16px] text-[var(--sb-text-3)]">
                    暂停后保留配置，恢复时重新计算下次运行。
                  </div>
                </div>
                <button
                  type="button"
                  className="sb-switch"
                  data-on={form.paused ? "true" : undefined}
                  aria-label={form.paused ? "恢复例程" : "暂停例程"}
                  onClick={() => setForm((prev) => ({ ...prev, paused: !prev.paused }))}
                />
              </div>
            )}
          </div>
        </div>
      </Modal>
      {confirmElement}
    </div>
  );
}

function formatSchedule(schedule: string, timezone?: string): string {
  const trimmed = schedule.trim();
  if (trimmed.startsWith("@every ")) {
    return `每 ${trimmed.slice(7)}`;
  }
  if (trimmed.startsWith("email:")) {
    const filter = trimmed.slice(6).trim();
    return filter ? `邮件到达 · ${filter}` : "邮件到达";
  }
  if (trimmed.startsWith("once:")) {
    const at = formatNext(trimmed.slice(5), timezone);
    return at ? `一次性 · ${at}` : "一次性";
  }
  return trimmed;
}

export function formatNext(value: string, timezone?: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const options: Intl.DateTimeFormatOptions = {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  };
  if (timezone) {
    try {
      return date.toLocaleString("zh-CN", { ...options, timeZone: timezone });
    } catch {
      // fall back to the browser timezone when the routine timezone is invalid
    }
  }
  return date.toLocaleString("zh-CN", options);
}
