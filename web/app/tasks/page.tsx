"use client";

import { useCallback, useEffect, useMemo, useState, type DragEvent } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ArrowLeft, CheckCircle2, GripVertical, LayoutGrid, List, Loader2, Plus, Trash2 } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import type { Bot, ProjectItem, TaskItem, User } from "@/lib/types";
import { Rail } from "@/components/Rail";
import { useToast } from "@/components/ui/Toast";
import { cn, timeAgo } from "@/lib/format";
import { Modal } from "@/components/ui/Modal";
import { useLanguage } from "@/components/LanguageProvider";

const statuses = [
  { key: "todo", labelKey: "tasks.statusTodo" },
  { key: "in_progress", labelKey: "tasks.statusInProgress" },
  { key: "blocked", labelKey: "tasks.statusBlocked" },
  { key: "done", labelKey: "tasks.statusDone" },
  { key: "canceled", labelKey: "tasks.statusCanceled" },
] as const;

type TaskStatus = TaskItem["status"];

export default function TasksPage() {
  const router = useRouter();
  const { t } = useLanguage();
  const { toast } = useToast();
  const [user, setUser] = useState<User | null>(null);
  const [tasks, setTasks] = useState<TaskItem[]>([]);
  const [bots, setBots] = useState<Bot[]>([]);
  const [projects, setProjects] = useState<ProjectItem[]>([]);
  const [projectID, setProjectID] = useState("");
  const [activeAccountID, setActiveAccountID] = useState("");
  const [projectOpen, setProjectOpen] = useState(false);
  const [newProjectName, setNewProjectName] = useState("");
  const [selectedProject, setSelectedProject] = useState<ProjectItem | null>(null);
  const [accountMembers, setAccountMembers] = useState<Array<{ id: string; email: string; display_name: string; role: string }>>([]);
  const [projectACL, setProjectACL] = useState<Array<{ user_id: string; role: string }>>([]);
  const [projectBusy, setProjectBusy] = useState(false);
  const [filter, setFilter] = useState<string>("all");
  const [view, setView] = useState<"list" | "board">("list");
  const [dragOver, setDragOver] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [assignee, setAssignee] = useState("");
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [me, taskResponse, botResponse, projectResponse, accountResponse] = await Promise.all([
        api.me(),
        api.listTasks(),
        api.listBots(),
        api.listProjects(),
        api.listAccounts(),
      ]);
      setUser(me.user);
      setActiveAccountID(accountResponse.active_account_id);
      setTasks(taskResponse.tasks);
      setBots(botResponse.bots);
      setProjects(projectResponse.projects);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        router.replace("/login");
        return;
      }
      toast(err instanceof Error ? err.message : "加载任务失败", "danger");
    } finally {
      setLoading(false);
    }
  }, [router, toast]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    try {
      if (window.localStorage.getItem("superbot-tasks-view") === "board") {
        setView("board");
      }
    } catch {
      // Storage can be unavailable in private windows; keep the list view.
    }
  }, []);

  const changeView = (next: "list" | "board") => {
    setView(next);
    try {
      window.localStorage.setItem("superbot-tasks-view", next);
    } catch {
      // Keep the in-memory choice when storage is unavailable.
    }
  };

  const visible = useMemo(
    () => (filter === "all" ? tasks : tasks.filter((task) => task.status === filter)),
    [filter, tasks],
  );

  const create = async () => {
    if (!title.trim()) return;
    setSaving(true);
    try {
      const result = await api.createTask({
        title: title.trim(),
        description: description.trim(),
        assignee_bot_id: assignee || null,
        project_id: projectID || null,
      });
      setTasks((prev) => [result.task, ...prev]);
      setTitle("");
      setDescription("");
      setAssignee("");
      setProjectID("");
      toast("任务已创建", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "创建任务失败", "danger");
    } finally {
      setSaving(false);
    }
  };

  const persistStatus = async (task: TaskItem, status: TaskStatus, previous: TaskStatus) => {
    setTasks((prev) => prev.map((item) => (item.id === task.id ? { ...item, status } : item)));
    try {
      const result = await api.updateTask(task.id, {
        title: task.title,
        description: task.description,
        status,
        priority: task.priority,
        assignee_bot_id: task.assignee_bot_id,
        due_at: task.due_at,
      });
      setTasks((prev) => prev.map((item) => (item.id === task.id ? result.task : item)));
    } catch (err) {
      setTasks((prev) =>
        prev.map((item) => (item.id === task.id ? { ...item, status: previous } : item)),
      );
      toast(err instanceof ApiError ? err.message : "更新任务失败", "danger");
    }
  };

  const updateStatus = (task: TaskItem, status: TaskStatus) => {
    if (task.status === status) return;
    void persistStatus(task, status, task.status);
  };

  const dropOnStatus = (status: TaskStatus, event: DragEvent) => {
    event.preventDefault();
    setDragOver(null);
    const taskID = event.dataTransfer.getData("text/plain");
    const task = tasks.find((item) => item.id === taskID);
    if (task) updateStatus(task, status);
  };

  const remove = async (task: TaskItem) => {
    try {
      await api.deleteTask(task.id);
      setTasks((prev) => prev.filter((item) => item.id !== task.id));
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "删除任务失败", "danger");
    }
  };

  const openProjects = async () => {
    setProjectOpen(true);
    setSelectedProject(null);
    setProjectACL([]);
    if (!activeAccountID) return;
    try {
      const result = await api.listAccountMembers(activeAccountID);
      setAccountMembers(result.members);
    } catch {
      setAccountMembers([]);
    }
  };

  const selectProject = async (project: ProjectItem) => {
    setSelectedProject(project);
    try {
      const result = await api.listProjectACL(project.id);
      setProjectACL(result.acl);
    } catch {
      setProjectACL([]);
    }
  };

  const updateProjectRole = async (userId: string, role: string) => {
    if (!selectedProject) return;
    setProjectBusy(true);
    try {
      if (role === "none") {
        await api.deleteProjectACL(selectedProject.id, userId);
        setProjectACL((prev) => prev.filter((row) => row.user_id !== userId));
      } else {
        await api.setProjectACL(selectedProject.id, userId, role);
        setProjectACL((prev) => [...prev.filter((row) => row.user_id !== userId), { user_id: userId, role }]);
      }
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "更新项目权限失败", "danger");
    } finally {
      setProjectBusy(false);
    }
  };

  if (!user) {
    return (
      <div className="flex h-screen items-center justify-center bg-[var(--sb-bg)]">
        <Loader2 className="sb-spin h-5 w-5 text-[var(--sb-text-3)]" />
      </div>
    );
  }

  return (
    <div className="flex h-screen w-full overflow-hidden bg-[var(--sb-bg)]">
      <Rail user={user} onLogout={() => void api.logout().finally(() => router.replace("/login"))} />
      <main className="flex min-w-0 flex-1 flex-col pb-16 md:pb-0">
        <header className="sb-page-header flex h-[61px] shrink-0 items-center justify-between gap-3 border-b border-[var(--sb-border)] px-6">
          <div className="flex items-center gap-3">
            <Link href="/" className="sb-icon-btn md:hidden" aria-label={t("tasks.backToChat")}>
              <ArrowLeft className="h-4 w-4" />
            </Link>
            <h1 className="text-[17px] font-semibold">{t("tasks.title")}</h1>
            <span className="rounded-full bg-[var(--sb-hover)] px-2 py-0.5 text-[11px] text-[var(--sb-text-2)]">
              {tasks.length}
            </span>
            <button type="button" onClick={() => void openProjects()} className="sb-btn" data-variant="secondary" data-size="sm">
              {t("tasks.projects")}
            </button>
          </div>
          <div className="flex items-center gap-3">
            {view === "list" && (
              <div className="flex items-center gap-1 text-[12px]">
                {["all", ...statuses.map((item) => item.key)].map((key) => (
                  <button
                    key={key}
                    type="button"
                    onClick={() => setFilter(key)}
                    className={cn(
                      "h-7 rounded-full px-2.5",
                      filter === key
                        ? "bg-[var(--sb-text)] text-[var(--sb-pane)]"
                        : "text-[var(--sb-text-2)] hover:bg-[var(--sb-hover)]",
                    )}
                  >
                    {key === "all" ? t("tasks.all") : t(statuses.find((item) => item.key === key)?.labelKey ?? "tasks.all")}
                  </button>
                ))}
              </div>
            )}
            <div className="flex items-center gap-0.5 rounded-full bg-[var(--sb-hover)] p-0.5">
              <button
                type="button"
                onClick={() => changeView("list")}
                aria-label={t("tasks.listView")}
                title={t("tasks.listView")}
                className={cn(
                  "flex h-6 w-7 items-center justify-center rounded-full transition",
                  view === "list"
                    ? "bg-[var(--sb-pane)] text-[var(--sb-text)] shadow-sm"
                    : "text-[var(--sb-text-3)] hover:text-[var(--sb-text)]",
                )}
              >
                <List className="h-3.5 w-3.5" />
              </button>
              <button
                type="button"
                onClick={() => changeView("board")}
                aria-label={t("tasks.boardView")}
                title={t("tasks.boardView")}
                className={cn(
                  "flex h-6 w-7 items-center justify-center rounded-full transition",
                  view === "board"
                    ? "bg-[var(--sb-pane)] text-[var(--sb-text)] shadow-sm"
                    : "text-[var(--sb-text-3)] hover:text-[var(--sb-text)]",
                )}
              >
                <LayoutGrid className="h-3.5 w-3.5" />
              </button>
            </div>
          </div>
        </header>

        <div className="flex-1 overflow-y-auto">
          <div className={cn("mx-auto w-full px-6 py-7", view === "board" ? "max-w-none" : "max-w-[900px]")}>
            <section className="rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-4">
              <div className="flex flex-wrap items-center gap-2">
                <input
                  value={title}
                  onChange={(event) => setTitle(event.target.value)}
                  placeholder={t("tasks.newTitle")}
                  className="field min-w-[220px] flex-1"
                />
                <select value={projectID} onChange={(event) => setProjectID(event.target.value)} className="field w-[160px]">
                  <option value="">{t("tasks.noProject")}</option>
                  {projects.map((project) => (
                    <option key={project.id} value={project.id}>{project.name}</option>
                  ))}
                </select>
                <select value={assignee} onChange={(event) => setAssignee(event.target.value)} className="field w-[180px]">
                  <option value="">{t("tasks.noAssignee")}</option>
                  {bots.map((bot) => (
                    <option key={bot.id} value={bot.id}>{bot.name}</option>
                  ))}
                </select>
                <button type="button" onClick={() => void create()} disabled={saving || !title.trim()} className="sb-btn" data-variant="primary" data-size="sm">
                  {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
                  {t("tasks.create")}
                </button>
              </div>
              <input
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                placeholder={t("tasks.description")}
                className="field mt-2 w-full"
              />
            </section>

            {loading ? (
              <div className="flex justify-center py-16"><Loader2 className="sb-spin h-5 w-5 text-[var(--sb-text-3)]" /></div>
            ) : view === "board" ? (
              <div className="mt-4 flex gap-3 overflow-x-auto pb-4">
                {statuses.map((status) => {
                  const items = tasks.filter((task) => task.status === status.key);
                  return (
                    <section
                      key={status.key}
                      onDragOver={(event) => {
                        event.preventDefault();
                        event.dataTransfer.dropEffect = "move";
                        setDragOver(status.key);
                      }}
                      onDragLeave={(event) => {
                        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
                          setDragOver((current) => (current === status.key ? null : current));
                        }
                      }}
                      onDrop={(event) => dropOnStatus(status.key, event)}
                      className={cn(
                        "flex w-[248px] min-w-[248px] flex-col rounded-2xl border bg-[var(--sb-pane)] p-2.5 transition",
                        dragOver === status.key
                          ? "border-[var(--sb-success)] bg-[var(--sb-success)]/5"
                          : "border-[var(--sb-border)]",
                      )}
                    >
                      <div className="flex items-center justify-between px-1.5 pb-2">
                        <span className="text-[12px] font-medium">{t(status.labelKey)}</span>
                        <span className="text-[11px] text-[var(--sb-text-4)]">{items.length}</span>
                      </div>
                      <div className="flex flex-1 flex-col gap-2">
                        {items.map((task) => {
                          const bot = bots.find((item) => item.id === task.assignee_bot_id);
                          const project = projects.find((item) => item.id === task.project_id);
                          return (
                            <article
                              key={task.id}
                              draggable
                              onDragStart={(event) => {
                                event.dataTransfer.setData("text/plain", task.id);
                                event.dataTransfer.effectAllowed = "move";
                              }}
                              onDragEnd={() => setDragOver(null)}
                              className="group cursor-grab rounded-xl border border-[var(--sb-border)] bg-[var(--sb-bg)] p-3 transition hover:border-[var(--sb-border-strong)] hover:shadow-sm active:cursor-grabbing"
                            >
                              <div className="flex items-start gap-2">
                                <GripVertical className="mt-0.5 h-3.5 w-3.5 shrink-0 text-[var(--sb-text-4)]" />
                                <div className="min-w-0 flex-1">
                                  <div className={cn("text-[13px] leading-[19px]", task.status === "done" && "text-[var(--sb-text-3)] line-through")}>
                                    {task.title}
                                  </div>
                                  {task.description && (
                                    <p className="mt-1 line-clamp-2 text-[11.5px] leading-[17px] text-[var(--sb-text-2)]">
                                      {task.description}
                                    </p>
                                  )}
                                  <div className="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-[10.5px] text-[var(--sb-text-3)]">
                                    {project && <span>{project.name}</span>}
                                    {bot && <span>@{bot.name}</span>}
                                    <span>{timeAgo(task.created_at)}</span>
                                  </div>
                                </div>
                                <button
                                  type="button"
                                  onClick={() => void remove(task)}
                                  className="shrink-0 text-[var(--sb-text-4)] opacity-100 transition hover:text-[var(--sb-danger)] md:opacity-0 md:group-hover:opacity-100"
                                  aria-label={t("tasks.deleteTask")}
                                >
                                  <Trash2 className="h-3 w-3" />
                                </button>
                              </div>
                            </article>
                          );
                        })}
                        {items.length === 0 && (
                          <div className="rounded-xl border border-dashed border-[var(--sb-border)] px-3 py-8 text-center text-[11px] text-[var(--sb-text-4)]">
                            {t("tasks.dropHere")}
                          </div>
                        )}
                      </div>
                    </section>
                  );
                })}
              </div>
            ) : visible.length === 0 ? (
              <div className="py-20 text-center text-[13px] text-[var(--sb-text-2)]">{t("tasks.empty")}</div>
            ) : (
              <div className="mt-4 overflow-hidden rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)]">
                {visible.map((task, index) => {
                  const bot = bots.find((item) => item.id === task.assignee_bot_id);
                  const project = projects.find((item) => item.id === task.project_id);
                  return (
                    <div key={task.id} className={cn("flex items-center gap-3 px-5 py-3.5", index > 0 && "border-t border-[var(--sb-border)]")}>
                      <CheckCircle2 className={cn("h-4 w-4 shrink-0", task.status === "done" ? "text-[var(--sb-success)]" : "text-[var(--sb-text-4)]")} />
                      <div className="min-w-0 flex-1">
                        <div className={cn("truncate text-[14px]", task.status === "done" && "text-[var(--sb-text-3)] line-through")}>{task.title}</div>
                        <div className="mt-0.5 flex flex-wrap items-center gap-2 text-[12px] text-[var(--sb-text-3)]">
                          {project && <span>{t("tasks.projectLabel", { name: project.name })}</span>}
                          {bot && <span>@{bot.name}</span>}
                          <span>{timeAgo(task.created_at)}</span>
                          {task.description && <span className="truncate">{task.description}</span>}
                        </div>
                      </div>
                      <select
                        value={task.status}
                        onChange={(event) => updateStatus(task, event.target.value as TaskStatus)}
                        className="h-8 rounded-lg bg-[var(--sb-hover)] px-2 text-[12px] outline-none"
                      >
                        {statuses.map((status) => (
                          <option key={status.key} value={status.key}>{t(status.labelKey)}</option>
                        ))}
                      </select>
                      <button type="button" onClick={() => void remove(task)} className="text-[var(--sb-text-4)] hover:text-[var(--sb-danger)]" aria-label={t("tasks.deleteTask")}>
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </div>
      </main>

      <Modal
        open={projectOpen}
        onClose={() => setProjectOpen(false)}
        title={t("tasks.projectManage")}
        subtitle={t("tasks.projectSubtitle")}
        size="md"
        footer={<button type="button" className="sb-btn" data-variant="secondary" onClick={() => setProjectOpen(false)}>关闭</button>}
      >
        <div className="flex items-center gap-2">
          <input
            className="sb-field flex-1"
            value={newProjectName}
            onChange={(event) => setNewProjectName(event.target.value)}
            placeholder={t("tasks.newProject")}
          />
          <button
            type="button"
            disabled={projectBusy || !newProjectName.trim()}
            onClick={async () => {
              setProjectBusy(true);
              try {
                const result = await api.createProject({ name: newProjectName.trim() });
                setProjects((prev) => [result.project, ...prev]);
                setProjectID(result.project.id);
                setNewProjectName("");
                await selectProject(result.project);
                toast("项目已创建", "success");
              } catch (err) {
                toast(err instanceof ApiError ? err.message : "创建项目失败", "danger");
              } finally {
                setProjectBusy(false);
              }
            }}
            className="sb-btn"
            data-variant="primary"
            data-size="sm"
          >
            创建
          </button>
        </div>

        <div className="mt-4 space-y-1">
          {projects.map((project) => (
            <div key={project.id} className="flex items-center gap-2 rounded-xl border border-[var(--sb-border)] px-3 py-2">
              <button type="button" onClick={() => void selectProject(project)} className="min-w-0 flex-1 truncate text-left text-[13px]">
                {project.name}
              </button>
              <span className="shrink-0 text-[11px] text-[var(--sb-text-3)]">
                {project.visibility === "owner" ? t("tasks.visibilityOwner") : t("tasks.visibilityAccount")}
              </span>
              <button
                type="button"
                onClick={async () => {
                  try {
                    await api.deleteProject(project.id);
                    setProjects((prev) => prev.filter((item) => item.id !== project.id));
                    if (projectID === project.id) setProjectID("");
                    if (selectedProject?.id === project.id) setSelectedProject(null);
                    toast("项目已删除", "success");
                  } catch (err) {
                    toast(err instanceof ApiError ? err.message : "删除项目失败", "danger");
                  }
                }}
                className="text-[var(--sb-text-4)] hover:text-[var(--sb-danger)]"
                aria-label={t("tasks.deleteProject")}
              >
                <Trash2 className="h-3.5 w-3.5" />
              </button>
            </div>
          ))}
          {projects.length === 0 && <div className="py-6 text-center text-[12px] text-[var(--sb-text-3)]">{t("tasks.noProjects")}</div>}
        </div>

        {selectedProject && (
          <div className="mt-4 border-t border-[var(--sb-border)] pt-3">
            <div className="mb-2 text-[12px] text-[var(--sb-text-2)]">{t("tasks.memberPerms", { name: selectedProject.name })}</div>
            <div className="space-y-1">
              {accountMembers.map((member) => {
                const acl = projectACL.find((row) => row.user_id === member.id);
                const accountEditor = member.role === "owner" || member.role === "admin";
                return (
                  <div key={member.id} className="flex items-center gap-2 rounded-lg border border-[var(--sb-border)] px-2.5 py-2">
                    <span className="min-w-0 flex-1 truncate text-[12px]">{member.display_name || member.email}</span>
                    <select
                      disabled={projectBusy || accountEditor}
                      value={accountEditor ? "editor" : acl?.role ?? "none"}
                      onChange={(event) => void updateProjectRole(member.id, event.target.value)}
                      className="h-7 rounded-md bg-[var(--sb-hover)] px-1.5 text-[11px] outline-none"
                    >
                      {accountEditor && <option value="editor">{t("tasks.roleAccount")}</option>}
                      {!accountEditor && <option value="none">{t("tasks.roleNone")}</option>}
                      {!accountEditor && <option value="viewer">{t("tasks.roleViewer")}</option>}
                      {!accountEditor && <option value="editor">{t("tasks.roleEditor")}</option>}
                      {!accountEditor && <option value="runner">{t("tasks.roleRunner")}</option>}
                    </select>
                  </div>
                );
              })}
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}
