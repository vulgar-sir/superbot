import type {
  Activity,
  AgentThreadDetail,
  Account,
  Bot,
  BotTemplateItem,
  CalendarEvent,
  MeetingItem,
  DocumentItem,
  DocumentVersion,
  BotFileEntry,
  CreditLedgerEntry,
  Device,
  StoreTemplate,
  AuditLogEntry,
  EmailDraft,
  EmailMessage,
  EmailThread,
  BotMcpServer,
  BotRoutine,
  Connector,
  GitHubDeviceStart,
  LibraryItem,
  LibraryItemDetail,
  ConversationSummary,
  GoalAction,
  GoalResponse,
  SideChat,
  Memory,
  MemoryImportResult,
  MemoryTidyReport,
  UsageData,
  VaultItem,
  VaultReveal,
  BotSkill,
  Conversation,
  ComputerInfo,
  EventEnvelope,
  McpServer,
  Notification,
  NotificationPreferences,
  TeamReview,
  SubagentTask,
  ProjectItem,
  TaskItem,
  Message,
  MessageReaction,
  Model,
  PermissionRule,
  ToolApproval,
  Provider,
  Routine,
  Run,
  SidebarSection,
  Skill,
  Team,
  ToolInvocation,
  User,
} from "./types";
export class ApiError extends Error {
  status: number;
  code: string;
  // Parsed JSON body of the failed response, when it had one.
  body?: unknown;

  constructor(status: number, code: string, message: string, body?: unknown) {
    super(message);
    this.status = status;
    this.code = code;
    this.body = body;
  }
}

type RequestOptions = RequestInit & { skipAuthRedirect?: boolean };

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers = new Headers(options.headers);
  if (options.body && !(options.body instanceof FormData) && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const response = await fetch(path, {
    credentials: "include",
    ...options,
    headers,
  });
  if (response.status === 401 && !options.skipAuthRedirect) {
    if (typeof window !== "undefined") {
      window.location.href = "/login";
    }
    throw new ApiError(401, "unauthorized", "登录已失效");
  }
  if (!response.ok) {
    let code = "http_error";
    let message = `请求失败（${response.status}）`;
    let parsed: unknown;
    try {
      const body = await response.json();
      parsed = body;
      if (body?.error?.code) code = body.error.code;
      if (body?.error?.message) message = body.error.message;
    } catch {
      // keep fallback
    }
    throw new ApiError(response.status, code, message, parsed);
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

function jsonBody(value: unknown): string {
  return JSON.stringify(value);
}

export const api = {
  login(email: string, password: string) {
    return request<{ user: User }>("/api/auth/login", {
      method: "POST",
      body: jsonBody({ email, password }),
      skipAuthRedirect: true,
    });
  },
  logout() {
    return request<{ status: string }>("/api/auth/logout", { method: "POST", body: jsonBody({}) });
  },
  me() {
    return request<{ user: User }>("/api/auth/me", { skipAuthRedirect: true });
  },
  setPrimaryBot(botId: string) {
    return request<{ user: User; primary_bot: Bot }>("/api/account/primary-bot", {
      method: "POST",
      body: jsonBody({ bot_id: botId }),
    });
  },
  webauthnCredentials() {
    return request<{
      count: number;
      credentials: Array<{ id: string; name: string; created_at: string }>;
    }>("/api/auth/webauthn/credentials");
  },
  renameWebAuthnCredential(id: string, name: string) {
    return request<{ credential: { id: string; name: string; created_at: string } }>(
      `/api/auth/webauthn/credentials/${id}`,
      { method: "PATCH", body: jsonBody({ name }) },
    );
  },
  deleteWebAuthnCredential(id: string) {
    return request<void>(`/api/auth/webauthn/credentials/${id}`, {
      method: "DELETE",
      body: jsonBody({}),
    });
  },
  changePassword(currentPassword: string, newPassword: string) {
    return request<{ status: string }>("/api/auth/password", {
      method: "POST",
      body: jsonBody({ current_password: currentPassword, new_password: newPassword }),
    });
  },
  listProviders() {
    return request<{ providers: Provider[] }>("/api/providers");
  },
  createProvider(input: {
    type: string;
    kind: string;
    name: string;
    base_url?: string;
    api_key?: string;
    config?: Record<string, unknown>;
    enabled?: boolean;
  }) {
    return request<{ provider: Provider }>("/api/providers", {
      method: "POST",
      body: jsonBody(input),
    });
  },
  updateProvider(
    id: string,
    input: {
      name?: string;
      base_url?: string;
      api_key?: string;
      clear_api_key?: boolean;
      enabled?: boolean;
      config?: Record<string, unknown>;
    },
  ) {
    return request<{ provider: Provider }>(`/api/providers/${id}`, {
      method: "PATCH",
      body: jsonBody(input),
    });
  },
  testProvider(id: string, model?: string) {
    return request<{ ok: boolean; model: string; reply: string }>(`/api/providers/${id}/test`, {
      method: "POST",
      body: jsonBody(model ? { model } : {}),
    });
  },
  deleteProvider(id: string) {
    return request<void>(`/api/providers/${id}`, { method: "DELETE", body: jsonBody({}) });
  },
  listModels(providerId: string) {
    return request<{ models: Model[] }>(`/api/providers/${providerId}/models`);
  },
  createModel(providerId: string, input: Record<string, unknown>) {
    return request<{ model: Model }>(`/api/providers/${providerId}/models`, {
      method: "POST",
      body: jsonBody(input),
    });
  },
  deleteModel(providerId: string, modelId: string) {
    return request<void>(`/api/providers/${providerId}/models/${modelId}`, {
      method: "DELETE",
      body: jsonBody({}),
    });
  },
  getSidebar() {
    return request<{ sections: SidebarSection[]; unassigned: SidebarSection }>("/api/sidebar");
  },
  createSection(name: string) {
    return request<{ section: SidebarSection }>("/api/sections", {
      method: "POST",
      body: jsonBody({ name }),
    });
  },
  updateSection(id: string, input: { name?: string; position?: number; collapsed?: boolean }) {
    return request<{ section: SidebarSection }>(`/api/sections/${id}`, {
      method: "PATCH",
      body: jsonBody(input),
    });
  },
  deleteSection(id: string) {
    return request<void>(`/api/sections/${id}`, { method: "DELETE", body: jsonBody({}) });
  },
  uploadBotAvatar(id: string, file: File) {
    const form = new FormData();
    form.append("file", file);
    return request<{ bot: Bot }>(`/api/bots/${id}/avatar`, { method: "POST", body: form });
  },
  generateBotAvatar(id: string, prompt: string) {
    return request<{ bot: Bot }>(`/api/bots/${id}/avatar/generate`, {
      method: "POST",
      body: jsonBody({ prompt }),
    });
  },
  deleteBotAvatar(id: string) {
    return request<{ bot: Bot }>(`/api/bots/${id}/avatar`, {
      method: "DELETE",
      body: jsonBody({}),
    });
  },
  listUserMemories(includeArchived = false) {
    return request<{ memories: Memory[] }>(
      includeArchived ? "/api/memories?include_archived=1" : "/api/memories",
    );
  },
  pinUserMemory(memoryId: string, pinned: boolean) {
    return request<{ memory: Memory }>(`/api/memories/${memoryId}/pin`, {
      method: "POST",
      body: jsonBody({ pinned }),
    });
  },
  restoreUserMemory(memoryId: string) {
    return request<{ memory: Memory }>(`/api/memories/${memoryId}/restore`, {
      method: "POST",
      body: jsonBody({}),
    });
  },
  tidyUserMemories() {
    return request<{ report: MemoryTidyReport }>("/api/memories/tidy", {
      method: "POST",
      body: jsonBody({}),
    });
  },
  importUserMemories(content: string) {
    return request<MemoryImportResult>("/api/memories/import", {
      method: "POST",
      body: jsonBody({ content }),
    });
  },
  // `duplicate` is true when the content matched an existing memory, which
  // was refreshed instead of saving a second copy.
  createUserMemory(content: string) {
    return request<{ memory: Memory; duplicate?: boolean }>("/api/memories", {
      method: "POST",
      body: jsonBody({ content }),
    });
  },
  updateUserMemory(memoryId: string, content: string) {
    return request<{ memory: Memory }>(`/api/memories/${memoryId}`, {
      method: "PUT",
      body: jsonBody({ content }),
    });
  },
  deleteUserMemory(memoryId: string) {
    return request<void>(`/api/memories/${memoryId}`, {
      method: "DELETE",
      body: jsonBody({}),
    });
  },
  setBotProactivity(id: string, enabled: boolean) {
    return request<{ bot: Bot }>(`/api/bots/${id}/proactivity`, {
      method: "PATCH",
      body: jsonBody({ enabled }),
    });
  },
  updateBotProfile(
    id: string,
    input: {
      name: string;
      title?: string;
      description?: string;
      avatar?: string;
      avatar_shape?: string;
      avatar_color?: string;
      section_id?: string | null;
      position?: number;
      pinned?: boolean;
    },
  ) {
    return request<{ bot: Bot }>(`/api/bots/${id}/profile`, {
      method: "PATCH",
      body: jsonBody(input),
    });
  },
  getBotTemplate(id: string) {
    return request<{ template: Record<string, unknown> }>(`/api/bots/${id}/template`);
  },
  listBotTemplates() {
    return request<{ templates: BotTemplateItem[] }>("/api/templates");
  },
  createBotTemplate(input: { name: string; description?: string; bot_id: string; visibility?: string }) {
    return request<{ template: BotTemplateItem }>("/api/templates", {
      method: "POST",
      body: jsonBody(input),
    });
  },
  getBotTemplateRegistry(id: string) {
    return request<{ template: BotTemplateItem }>(`/api/templates/${id}`);
  },
  deleteBotTemplate(id: string) {
    return request<void>(`/api/templates/${id}`, {
      method: "DELETE",
      body: jsonBody({}),
    });
  },
  listTemplateACL(id: string) {
    return request<{ acl: Array<{ user_id: string; email: string; display_name: string; role: string }> }>(
      `/api/templates/${id}/acl`,
    );
  },
  setTemplateACL(id: string, userId: string, role: string) {
    return request<{ status: string }>(`/api/templates/${id}/acl`, {
      method: "PUT",
      body: jsonBody({ user_id: userId, role }),
    });
  },
  deleteTemplateACL(id: string, userId: string) {
    return request<void>(`/api/templates/${id}/acl/${userId}`, {
      method: "DELETE",
      body: jsonBody({}),
    });
  },
  importBotTemplate(template: Record<string, unknown>) {
    return request<{ bot: Bot }>("/api/bot-templates/import", {
      method: "POST",
      body: jsonBody({ template }),
    });
  },
  listBots() {
    return request<{ bots: Bot[] }>("/api/bots");
  },
  createBot(input: Record<string, unknown>) {
    return request<{ bot: Bot }>("/api/bots", { method: "POST", body: jsonBody(input) });
  },
  updateBot(id: string, input: Record<string, unknown>) {
    return request<{ bot: Bot }>(`/api/bots/${id}`, { method: "PATCH", body: jsonBody(input) });
  },
  deleteBot(id: string) {
    return request<void>(`/api/bots/${id}`, { method: "DELETE", body: jsonBody({}) });
  },
  listConversations() {
    return request<{ conversations: Conversation[] }>("/api/conversations");
  },
  createConversation(input: {
    kind: "direct" | "team";
    bot_id?: string;
    team_id?: string;
    title?: string;
  }) {
    return request<{ conversation: Conversation }>("/api/conversations", {
      method: "POST",
      body: jsonBody(input),
    });
  },
  listTeams() {
    return request<{ teams: Team[] }>("/api/teams");
  },
  createTeam(input: {
    name: string;
    description: string;
    mode?: string;
    config: Record<string, unknown>;
    bot_ids: string[];
  }) {
    return request<{ team: Team }>("/api/teams", { method: "POST", body: jsonBody(input) });
  },
  updateTeam(
    id: string,
    input: { name: string; description: string; mode?: string; config: Record<string, unknown> },
  ) {
    return request<{ team: Team }>(`/api/teams/${id}`, { method: "PATCH", body: jsonBody(input) });
  },
  deleteTeam(id: string) {
    return request<void>(`/api/teams/${id}`, { method: "DELETE", body: jsonBody({}) });
  },
  updateTeamPlacement(
    id: string,
    input: { section_id: string | null; position: number; pinned: boolean },
  ) {
    return request<{ team: Team }>(`/api/teams/${id}/placement`, {
      method: "PATCH",
      body: jsonBody(input),
    });
  },
  setTeamBots(id: string, bots: Array<{ bot_id: string; position: number; role?: string }>) {
    return request<{ team: Team }>(`/api/teams/${id}/bots`, {
      method: "PUT",
      body: jsonBody({ bots }),
    });
  },
  listSkillACL(skillId: string) {
    return request<{ acl: Array<{ user_id: string; email: string; display_name: string; role: string }> }>(
      `/api/skills/${skillId}/acl`,
    );
  },
  setSkillACL(skillId: string, userId: string, role: string) {
    return request<{ status: string }>(`/api/skills/${skillId}/acl`, {
      method: "PUT",
      body: jsonBody({ user_id: userId, role }),
    });
  },
  deleteSkillACL(skillId: string, userId: string) {
    return request<void>(`/api/skills/${skillId}/acl/${userId}`, {
      method: "DELETE",
      body: jsonBody({}),
    });
  },
  listSkills() {
    return request<{ skills: Skill[] }>("/api/skills");
  },
  createSkill(input: { name: string; description: string; content: string }) {
    return request<{ skill: Skill }>("/api/skills", { method: "POST", body: jsonBody(input) });
  },
  approveSkill(id: string) {
    return request<{ skill: Skill }>(`/api/skills/${id}/approve`, {
      method: "POST",
      body: jsonBody({}),
    });
  },
  disableSkill(id: string) {
    return request<{ skill: Skill }>(`/api/skills/${id}/disable`, {
      method: "POST",
      body: jsonBody({}),
    });
  },
  deleteSkill(id: string) {
    return request<void>(`/api/skills/${id}`, { method: "DELETE", body: jsonBody({}) });
  },
  getBotSkills(botId: string) {
    return request<{ skills: BotSkill[] }>(`/api/bots/${botId}/skills`);
  },
  setBotSkills(botId: string, skillIds: string[]) {
    return request<{ skills: BotSkill[] }>(`/api/bots/${botId}/skills`, {
      method: "PUT",
      body: jsonBody({ skill_ids: skillIds }),
    });
  },
  listMcpServers() {
    return request<{ mcp_servers: McpServer[] }>("/api/mcp");
  },
  importMcp(payload: Record<string, unknown>) {
    return request<{ mcp_servers: McpServer[] }>("/api/mcp/import", {
      method: "POST",
      body: jsonBody(payload),
    });
  },
  createMcpServer(input: {
    name: string;
    transport?: string;
    url?: string;
    headers?: Record<string, string>;
    command?: string;
    args?: string[];
    env?: Record<string, string>;
  }) {
    return request<{ mcp_server: McpServer }>("/api/mcp", {
      method: "POST",
      body: jsonBody(input),
    });
  },
  testMcpServer(id: string) {
    return request<{ ok: boolean; tools: string[] }>(`/api/mcp/${id}/test`, {
      method: "POST",
      body: jsonBody({}),
    });
  },
  approveMcpServer(id: string) {
    return request<{ mcp_server: McpServer }>(`/api/mcp/${id}/approve`, {
      method: "POST",
      body: jsonBody({}),
    });
  },
  disableMcpServer(id: string) {
    return request<{ mcp_server: McpServer }>(`/api/mcp/${id}/disable`, {
      method: "POST",
      body: jsonBody({}),
    });
  },
  deleteMcpServer(id: string) {
    return request<void>(`/api/mcp/${id}`, { method: "DELETE", body: jsonBody({}) });
  },
  getBotMcpServers(botId: string) {
    return request<{ mcp_servers: BotMcpServer[] }>(`/api/bots/${botId}/mcp`);
  },
  setBotMcpServers(botId: string, serverIds: string[]) {
    return request<{ mcp_servers: BotMcpServer[] }>(`/api/bots/${botId}/mcp`, {
      method: "PUT",
      body: jsonBody({ mcp_server_ids: serverIds }),
    });
  },
  getComputer(botId: string) {
    return request<{ computer: ComputerInfo }>(`/api/bots/${botId}/computer`);
  },
  startComputer(botId: string, profile?: "browser" | "desktop") {
    return request<{ computer: ComputerInfo }>(`/api/bots/${botId}/computer/start`, {
      method: "POST",
      body: jsonBody(profile ? { profile } : {}),
    });
  },
  stopComputer(botId: string) {
    return request<{ computer: ComputerInfo }>(`/api/bots/${botId}/computer/stop`, {
      method: "POST",
      body: jsonBody({}),
    });
  },
  updateComputer(botId: string) {
    return request<{ action: { id: string; kind: string; status: string } }>(
      `/api/bots/${botId}/computer/update`,
      { method: "POST", body: jsonBody({}) },
    );
  },
  resetComputer(botId: string) {
    return request<{ action: { id: string; kind: string; status: string } }>(
      `/api/bots/${botId}/computer/reset`,
      { method: "POST", body: jsonBody({}) },
    );
  },
  createComputerSnapshot(botId: string) {
    return request<{ status: string }>(`/api/bots/${botId}/computer/snapshot`, {
      method: "POST",
      body: jsonBody({}),
    });
  },
  listComputerSnapshots(botId: string) {
    return request<{ snapshots: Array<{ id: string; label: string; size_bytes: number; created_at: string }> }>(
      `/api/bots/${botId}/computer/snapshots`,
    );
  },
  restoreComputerSnapshot(botId: string, snapshotId: string) {
    return request<{ status: string }>(`/api/bots/${botId}/computer/restore`, {
      method: "POST",
      body: jsonBody({ snapshot_id: snapshotId }),
    });
  },
  deleteComputerSnapshot(botId: string, snapshotId: string) {
    return request<void>(`/api/bots/${botId}/computer/snapshots/${snapshotId}`, {
      method: "DELETE",
      body: jsonBody({}),
    });
  },
  listUsers() {
    return request<{ users: User[] }>("/api/users");
  },
  createUser(input: { email: string; password: string; display_name?: string; role?: string }) {
    return request<{ user: User }>("/api/users", { method: "POST", body: jsonBody(input) });
  },
  deleteUser(id: string) {
    return request<void>(`/api/users/${id}`, { method: "DELETE", body: jsonBody({}) });
  },
  getUsage(from?: string, to?: string) {
    const params = new URLSearchParams();
    if (from) params.set("from", from);
    if (to) params.set("to", to);
    const query = params.toString();
    return request<UsageData>(`/api/usage${query ? `?${query}` : ""}`);
  },
  listConnectors() {
    return request<{ connectors: Connector[] }>("/api/connectors");
  },
  createConnector(input: { provider: string; name: string; token: string; config?: Record<string, unknown> }) {
    return request<{ connector: Connector }>("/api/connectors", {
      method: "POST",
      body: jsonBody(input),
    });
  },
  testConnector(id: string) {
    return request<{ ok: boolean; identity: string }>(`/api/connectors/${id}/test`, {
      method: "POST",
      body: jsonBody({}),
    });
  },
  deleteConnector(id: string) {
    return request<void>(`/api/connectors/${id}`, { method: "DELETE", body: jsonBody({}) });
  },
  startGitHubDevice(clientId: string) {
    return request<GitHubDeviceStart>("/api/connectors/github/device/start", {
      method: "POST",
      body: jsonBody({ client_id: clientId }),
    });
  },
  pollGitHubDevice(clientId: string, deviceCode: string) {
    return request<{ pending: boolean; connector?: Connector }>("/api/connectors/github/device/poll", {
      method: "POST",
      body: jsonBody({ client_id: clientId, device_code: deviceCode }),
    });
  },
  listDevices() {
    return request<{ devices: Device[] }>("/api/devices");
  },
  createDevice(name: string, platform = "") {
    return request<{ device: Device; token: string }>("/api/devices", {
      method: "POST",
      body: jsonBody({ name, platform }),
    });
  },
  deleteDevice(id: string) {
    return request<void>(`/api/devices/${id}`, { method: "DELETE", body: jsonBody({}) });
  },
  setDeviceAllowBot(id: string, allowBot: boolean) {
    return request<{ device: Device }>(`/api/devices/${id}/allow-bot`, {
      method: "PATCH",
      body: jsonBody({ allow_bot: allowBot }),
    });
  },
  screenshotDevice(id: string) {
    return request<{ status: string; url: string; error: string }>(`/api/devices/${id}/screenshot`, {
      method: "POST",
      body: jsonBody({}),
    });
  },
  execDevice(id: string, command: string) {
    return request<{ status: string; output: string; error: string }>(`/api/devices/${id}/exec`, {
      method: "POST",
      body: jsonBody({ command }),
    });
  },
  listVault() {
    return request<{ items: VaultItem[] }>("/api/vault");
  },
  createVault(input: {
    kind: string;
    name: string;
    username?: string;
    password?: string;
    url?: string;
    notes?: string;
  }) {
    return request<{ item: VaultItem }>("/api/vault", { method: "POST", body: jsonBody(input) });
  },
  updateVault(
    id: string,
    input: {
      kind: string;
      name: string;
      username?: string;
      password?: string;
      url?: string;
      notes?: string;
    },
  ) {
    return request<{ item: VaultItem }>(`/api/vault/${id}`, { method: "PATCH", body: jsonBody(input) });
  },
  deleteVault(id: string) {
    return request<void>(`/api/vault/${id}`, { method: "DELETE", body: jsonBody({}) });
  },
  revealVault(id: string) {
    return request<VaultReveal>(`/api/vault/${id}/reveal`);
  },
  listBotMemories(botId: string, includeArchived = false) {
    return request<{ memories: Memory[] }>(
      `/api/bots/${botId}/memories${includeArchived ? "?include_archived=1" : ""}`,
    );
  },
  pinBotMemory(botId: string, memoryId: string, pinned: boolean) {
    return request<{ memory: Memory }>(`/api/bots/${botId}/memories/${memoryId}/pin`, {
      method: "POST",
      body: jsonBody({ pinned }),
    });
  },
  restoreBotMemory(botId: string, memoryId: string) {
    return request<{ memory: Memory }>(`/api/bots/${botId}/memories/${memoryId}/restore`, {
      method: "POST",
      body: jsonBody({}),
    });
  },
  tidyBotMemories(botId: string) {
    return request<{ report: MemoryTidyReport }>(`/api/bots/${botId}/memories/tidy`, {
      method: "POST",
      body: jsonBody({}),
    });
  },
  updateBotMemory(botId: string, memoryId: string, content: string) {
    return request<{ memory: Memory }>(`/api/bots/${botId}/memories/${memoryId}`, {
      method: "PUT",
      body: jsonBody({ content }),
    });
  },
  deleteBotMemory(botId: string, memoryId: string) {
    return request<void>(`/api/bots/${botId}/memories/${memoryId}`, {
      method: "DELETE",
      body: jsonBody({}),
    });
  },
  importBotMemories(botId: string, content: string) {
    return request<MemoryImportResult>(`/api/bots/${botId}/memories/import`, {
      method: "POST",
      body: jsonBody({ content }),
    });
  },
  listBotACL(botId: string) {
    return request<{ acl: Array<{ user_id: string; email: string; display_name: string; role: string }> }>(
      `/api/bots/${botId}/acl`,
    );
  },
  setBotACL(botId: string, userId: string, role: string) {
    return request<{ status: string }>(`/api/bots/${botId}/acl`, {
      method: "PUT",
      body: jsonBody({ user_id: userId, role }),
    });
  },
  deleteBotACL(botId: string, userId: string) {
    return request<void>(`/api/bots/${botId}/acl/${userId}`, {
      method: "DELETE",
      body: jsonBody({}),
    });
  },
  listBotFiles(botId: string, path = "") {
    return request<{ files: BotFileEntry[]; path: string }>(
      `/api/bots/${botId}/files?path=${encodeURIComponent(path)}`,
    );
  },
  botFileURL(botId: string, path: string) {
    return `/api/bots/${botId}/files/content?path=${encodeURIComponent(path)}`;
  },
  // The account's shared drive: one folder every bot sees at /shared.
  listDriveFiles(path = "") {
    return request<{ files: BotFileEntry[]; path: string }>(`/api/drive/files?path=${encodeURIComponent(path)}`);
  },
  driveFileURL(path: string) {
    return `/api/drive/files/content?path=${encodeURIComponent(path)}`;
  },
  listBotLibrary(botId: string) {
    return request<{ items: LibraryItem[] }>(`/api/bots/${botId}/library`);
  },
  getLibraryItem(id: string) {
    return request<LibraryItemDetail>(`/api/library/${id}`);
  },
  deleteLibraryItem(id: string) {
    return request<void>(`/api/library/${id}`, { method: "DELETE", body: jsonBody({}) });
  },
  // Saves any Library item as a file: its stored file, or a document's text as .md.
  libraryDownloadURL(id: string) {
    return `/api/library/${id}/download`;
  },
  attachmentDownloadURL(url: string) {
    return `${url}${url.includes("?") ? "&" : "?"}download=1`;
  },
  listBotRoutines(botId?: string) {
    const query = botId ? `?bot_id=${encodeURIComponent(botId)}` : "";
    return request<{ routines: BotRoutine[] }>(`/api/bot-routines${query}`);
  },
  createBotRoutine(input: {
    bot_id: string;
    name: string;
    schedule: string;
    timezone?: string;
    timing?: string;
    prompt: string;
    enabled?: boolean;
  }) {
    return request<{ routine: BotRoutine }>("/api/bot-routines", {
      method: "POST",
      body: jsonBody(input),
    });
  },
  testBotRoutine(id: string) {
    return request<{ status: string }>(`/api/bot-routines/${id}/test`, {
      method: "POST",
      body: jsonBody({}),
    });
  },
  updateBotRoutine(
    id: string,
    input: {
      name?: string;
      schedule?: string;
      timezone?: string;
      timing?: string;
      prompt?: string;
      enabled?: boolean;
      paused?: boolean;
    },
  ) {
    return request<{ routine: BotRoutine }>(`/api/bot-routines/${id}`, {
      method: "PATCH",
      body: jsonBody(input),
    });
  },
  deleteBotRoutine(id: string) {
    return request<void>(`/api/bot-routines/${id}`, { method: "DELETE", body: jsonBody({}) });
  },
  listRoutines() {
    return request<{ routines: Routine[] }>("/api/routines");
  },
  createRoutine(input: {
    team_id: string;
    name: string;
    cron: string;
    prompt: string;
    enabled: boolean;
  }) {
    return request<{ routine: Routine }>("/api/routines", { method: "POST", body: jsonBody(input) });
  },
  updateRoutine(
    id: string,
    input: { name: string; cron: string; prompt: string; enabled: boolean },
  ) {
    return request<{ routine: Routine }>(`/api/routines/${id}`, {
      method: "PATCH",
      body: jsonBody(input),
    });
  },
  deleteRoutine(id: string) {
    return request<void>(`/api/routines/${id}`, { method: "DELETE", body: jsonBody({}) });
  },
  getConversation(id: string) {
    return request<{ conversation: Conversation; messages: Message[] }>(
      `/api/conversations/${id}`,
    );
  },
  getConversationSummary(id: string) {
    return request<{ summary: ConversationSummary | null }>(
      `/api/conversations/${id}/summary`,
    );
  },
  listConversationSummaries(id: string) {
    return request<{ summaries: ConversationSummary[] }>(`/api/conversations/${id}/summaries`);
  },
  getGoal(conversationId: string) {
    return request<GoalResponse>(`/api/conversations/${conversationId}/goal`);
  },
  goalAction(conversationId: string, action: GoalAction) {
    return request<GoalResponse>(`/api/conversations/${conversationId}/goal/actions`, {
      method: "POST",
      body: jsonBody(action),
    });
  },
  listSideChats(conversationId: string) {
    return request<{ side_chats: SideChat[] }>(`/api/conversations/${conversationId}/side`);
  },
  // Resolves with the saved entry even when the model failed (HTTP 502): the
  // server still returns the side chat, with status "failed" and its error.
  async askSide(conversationId: string, question: string) {
    try {
      return await request<{ side_chat: SideChat }>(`/api/conversations/${conversationId}/side`, {
        method: "POST",
        body: jsonBody({ question }),
      });
    } catch (err) {
      if (err instanceof ApiError && err.status === 502) {
        const sideChat = (err.body as { side_chat?: SideChat } | undefined)?.side_chat;
        if (sideChat && typeof sideChat.id === "string") return { side_chat: sideChat };
      }
      throw err;
    }
  },
  deleteSideChat(conversationId: string, sideId: string) {
    return request<void>(`/api/conversations/${conversationId}/side/${sideId}`, {
      method: "DELETE",
    });
  },
  sendMessage(
    id: string,
    content: string,
    clientMsgId: string,
    parentMessageId?: string | null,
    attachmentIds?: string[],
  ) {
    return request<{
      message: Message;
      cycle_id?: string;
      cycle_status?: string;
      interjection?: boolean;
    }>(`/api/conversations/${id}/messages`, {
      method: "POST",
      body: jsonBody({
        content,
        client_msg_id: clientMsgId,
        parent_message_id: parentMessageId ?? null,
        attachment_ids: attachmentIds ?? [],
      }),
    });
  },
  async uploadAttachment(file: File) {
    const form = new FormData();
    form.append("file", file);
    const response = await fetch("/api/attachments", {
      method: "POST",
      credentials: "include",
      body: form,
    });
    if (!response.ok) {
      let message = `上传失败（${response.status}）`;
      try {
        const body = await response.json();
        if (body?.error?.message) message = body.error.message;
      } catch {
        // keep fallback
      }
      throw new ApiError(response.status, "upload_failed", message);
    }
    const body = (await response.json()) as { attachment: import("./types").Attachment };
    return body.attachment;
  },
  async uploadLibraryFile(botId: string, file: File) {
    const form = new FormData();
    form.append("file", file);
    const response = await fetch(`/api/bots/${botId}/library/files`, {
      method: "POST",
      credentials: "include",
      body: form,
    });
    if (!response.ok) {
      let message = `上传失败（${response.status}）`;
      try {
        const body = await response.json();
        if (body?.error?.message) message = body.error.message;
      } catch {
        // keep fallback
      }
      throw new ApiError(response.status, "upload_failed", message);
    }
    const body = (await response.json()) as { item: import("./types").LibraryItem };
    return body.item;
  },
  toggleMessageReaction(messageId: string, emoji: string, action: "add" | "remove") {
    return request<{ reactions: MessageReaction[] }>(
      `/api/messages/${messageId}/reactions`,
      { method: "POST", body: jsonBody({ emoji, action }) },
    );
  },
  updateMessage(messageId: string, content: string) {
    return request<{ message: Message }>(`/api/messages/${messageId}`, {
      method: "PATCH",
      body: jsonBody({ content }),
    });
  },
  deleteMessage(messageId: string) {
    return request<{ deleted: boolean; id: string }>(`/api/messages/${messageId}`, {
      method: "DELETE",
      body: jsonBody({}),
    });
  },
  setMessagePinned(messageId: string, pinned: boolean) {
    return request<{ message: Message }>(`/api/messages/${messageId}/pin`, {
      method: "POST",
      body: jsonBody({ pinned }),
    });
  },
  votePoll(messageId: string, optionId: string, action: "add" | "remove") {
    return request<{ message: Message }>(`/api/messages/${messageId}/vote`, {
      method: "POST",
      body: jsonBody({ option_id: optionId, action }),
    });
  },
  stopConversation(id: string) {
    return request<{ canceled_cycles: number }>(`/api/conversations/${id}/stop`, {
      method: "POST",
      body: jsonBody({}),
    });
  },
  getAgentThread(id: string) {
    return request<AgentThreadDetail>(`/api/agent-threads/${id}`);
  },
  getPermissions() {
    return request<{ auto_review: boolean; rules: PermissionRule[] }>("/api/permissions");
  },
  updatePermissions(autoReview: boolean) {
    return request<{ auto_review: boolean }>("/api/permissions", {
      method: "PATCH",
      body: jsonBody({ auto_review: autoReview }),
    });
  },
  createPermissionRule(kind: "always_ask" | "allow", pattern: string) {
    return request<{ rule: PermissionRule }>("/api/permissions/rules", {
      method: "POST",
      body: jsonBody({ kind, pattern }),
    });
  },
  deletePermissionRule(id: string) {
    return request<{ status: string }>(`/api/permissions/rules/${id}`, {
      method: "DELETE",
      body: jsonBody({}),
    });
  },
  getOAuthStatus() {
    return request<{ providers: string[] }>("/api/connectors/oauth/status");
  },
  listToolApprovals(conversationId?: string) {
    const query = conversationId
      ? `?conversation_id=${encodeURIComponent(conversationId)}`
      : "";
    return request<{ approvals: ToolApproval[] }>(`/api/tool-approvals${query}`);
  },
  resolveToolApproval(id: string, decision: "allow" | "deny") {
    return request<{ approval: ToolApproval }>(`/api/tool-approvals/${id}`, {
      method: "POST",
      body: jsonBody({ decision }),
    });
  },
  listAccounts() {
    return request<{ accounts: Account[]; active_account_id: string }>("/api/accounts");
  },
  createAccount(name: string) {
    return request<{ account: Account }>("/api/accounts", {
      method: "POST",
      body: jsonBody({ name }),
    });
  },
  listAccountMembers(id: string) {
    return request<{ members: Array<{ id: string; email: string; display_name: string; role: string; created_at: string }> }>(
      `/api/accounts/${id}/members`,
    );
  },
  listAccountInvitations(id: string) {
    return request<{ invitations: Array<{ id: string; email: string; role: string; expires_at: string; created_at: string }> }>(
      `/api/accounts/${id}/invitations`,
    );
  },
  createAccountInvitation(id: string, email: string, role = "member") {
    return request<{ invitation: Record<string, unknown>; token: string; url: string }>(
      `/api/accounts/${id}/invitations`,
      { method: "POST", body: jsonBody({ email, role }) },
    );
  },
  acceptInvitation(token: string) {
    return request<{ account_id: string }>("/api/invitations/accept", {
      method: "POST",
      body: jsonBody({ token }),
    });
  },
  switchAccount(id: string) {
    return request<{ accounts: Account[]; active_account_id: string }>(
      `/api/accounts/${id}/switch`,
      { method: "POST", body: jsonBody({}) },
    );
  },
  listMeetings() {
    return request<{ meetings: MeetingItem[] }>("/api/meetings");
  },
  createMeeting(input: { title: string; transcript: string; starts_at?: string }) {
    return request<{ meeting: MeetingItem }>("/api/meetings", {
      method: "POST",
      body: jsonBody(input),
    });
  },
  getMeeting(id: string) {
    return request<{ meeting: MeetingItem }>(`/api/meetings/${id}`);
  },
  async webrtcOffer(sdp: string) {
    const response = await fetch("/api/voice/webrtc/offer", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ sdp }),
    });
    if (!response.ok) {
      let message = `WebRTC 协商失败（${response.status}）`;
      try {
        const body = await response.json();
        if (body?.error?.message) message = body.error.message;
      } catch {
        // keep fallback
      }
      throw new ApiError(response.status, "webrtc_failed", message);
    }
    return (await response.json()) as { type: string; sdp: string };
  },
  voiceStatus() {
    return request<{
      stt: { configured: boolean; model: string };
      tts: { configured: boolean; model: string; voice: string };
    }>("/api/voice/status", { skipAuthRedirect: true });
  },
  async speakVoice(text: string) {
    const response = await fetch("/api/voice/speak", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text }),
    });
    if (!response.ok) {
      let message = `语音合成失败（${response.status}）`;
      try {
        const body = await response.json();
        if (body?.error?.message) message = body.error.message;
      } catch {
        // keep fallback
      }
      throw new ApiError(response.status, "tts_failed", message);
    }
    return response.blob();
  },
  transcribeVoice(file: File) {
    const form = new FormData();
    form.append("file", file);
    return request<{ text: string }>("/api/voice/transcribe", {
      method: "POST",
      body: form,
    });
  },
  summarizeMeeting(id: string) {
    return request<{ meeting: MeetingItem }>(`/api/meetings/${id}/summarize`, {
      method: "POST",
      body: jsonBody({}),
    });
  },
  deleteMeeting(id: string) {
    return request<void>(`/api/meetings/${id}`, {
      method: "DELETE",
      body: jsonBody({}),
    });
  },
  listCalendarEvents() {
    return request<{ events: CalendarEvent[] }>("/api/calendar");
  },
  createCalendarEvent(input: { title: string; starts_at: string; ends_at: string; description?: string; location?: string }) {
    return request<{ event: CalendarEvent }>("/api/calendar", {
      method: "POST",
      body: jsonBody(input),
    });
  },
  deleteCalendarEvent(id: string) {
    return request<void>(`/api/calendar/${id}`, {
      method: "DELETE",
      body: jsonBody({}),
    });
  },
  listDocuments() {
    return request<{ documents: DocumentItem[] }>("/api/documents");
  },
  createDocument(input: { title: string; content?: string; kind?: string }) {
    return request<{ document: DocumentItem }>("/api/documents", {
      method: "POST",
      body: jsonBody(input),
    });
  },
  getDocument(id: string) {
    return request<{ document: DocumentItem }>(`/api/documents/${id}`);
  },
  listDocumentACL(id: string) {
    return request<{ acl: Array<{ user_id: string; email: string; display_name: string; role: string }> }>(
      `/api/documents/${id}/acl`,
    );
  },
  setDocumentACL(id: string, userId: string, role: string) {
    return request<{ status: string }>(`/api/documents/${id}/acl`, {
      method: "PUT",
      body: jsonBody({ user_id: userId, role }),
    });
  },
  deleteDocumentACL(id: string, userId: string) {
    return request<void>(`/api/documents/${id}/acl/${userId}`, {
      method: "DELETE",
      body: jsonBody({}),
    });
  },
  listDocumentVersions(id: string) {
    return request<{ versions: DocumentVersion[] }>(`/api/documents/${id}/versions`);
  },
  postDocumentCursor(id: string, input: { client_id: string; x: number; y: number }) {
    return request<{ status: string }>(`/api/documents/${id}/cursor`, {
      method: "POST",
      body: jsonBody(input),
    });
  },
  listDocumentCRDT(documentId: string) {
    return request<{ updates: string[] }>(`/api/documents/${documentId}/crdt`);
  },
  seedDocumentCRDT(documentId: string, update: string) {
    return request<{ seeded: boolean; updates: string[] }>(`/api/documents/${documentId}/crdt/seed`, {
      method: "POST",
      body: jsonBody({ update }),
    });
  },
  pushDocumentCRDT(documentId: string, update: string, clientId: string) {
    return request<void>(`/api/documents/${documentId}/crdt`, {
      method: "POST",
      body: jsonBody({ update, client_id: clientId }),
    });
  },
  collabDocument(id: string, input: { title: string; content: string; kind: string; client_id: string; base_revision?: number }) {
    return request<{ document: DocumentItem }>(`/api/documents/${id}/collab`, {
      method: "POST",
      body: jsonBody(input),
    });
  },
  updateDocument(id: string, input: { title: string; content: string; kind: string }) {
    return request<{ document: DocumentItem }>(`/api/documents/${id}`, {
      method: "PATCH",
      body: jsonBody(input),
    });
  },
  deleteDocument(id: string) {
    return request<void>(`/api/documents/${id}`, {
      method: "DELETE",
      body: jsonBody({}),
    });
  },
  listProjects() {
    return request<{ projects: ProjectItem[] }>("/api/projects");
  },
  createProject(input: { name: string; description?: string; visibility?: string }) {
    return request<{ project: ProjectItem }>("/api/projects", {
      method: "POST",
      body: jsonBody(input),
    });
  },
  deleteProject(projectId: string) {
    return request<void>(`/api/projects/${projectId}`, {
      method: "DELETE",
      body: jsonBody({}),
    });
  },
  deleteProjectACL(projectId: string, userId: string) {
    return request<void>(`/api/projects/${projectId}/acl/${userId}`, {
      method: "DELETE",
      body: jsonBody({}),
    });
  },
  listProjectACL(projectId: string) {
    return request<{ acl: Array<{ user_id: string; email: string; display_name: string; role: string }> }>(
      `/api/projects/${projectId}/acl`,
    );
  },
  setProjectACL(projectId: string, userId: string, role: string) {
    return request<{ status: string }>(`/api/projects/${projectId}/acl`, {
      method: "PUT",
      body: jsonBody({ user_id: userId, role }),
    });
  },
  listTasks(status?: string) {
    return request<{ tasks: TaskItem[] }>(
      `/api/tasks${status ? `?status=${encodeURIComponent(status)}` : ""}`,
    );
  },
  createTask(input: Record<string, unknown>) {
    return request<{ task: TaskItem }>("/api/tasks", { method: "POST", body: jsonBody(input) });
  },
  updateTask(id: string, input: Record<string, unknown>) {
    return request<{ task: TaskItem }>(`/api/tasks/${id}`, { method: "PATCH", body: jsonBody(input) });
  },
  deleteTask(id: string) {
    return request<void>(`/api/tasks/${id}`, { method: "DELETE", body: jsonBody({}) });
  },
  listSubagents(conversationId: string) {
    return request<{ subagents: SubagentTask[] }>(
      `/api/subagents?conversation_id=${encodeURIComponent(conversationId)}`,
    );
  },
  stopSubagent(taskId: string) {
    return request<{ status: string }>(`/api/subagents/${taskId}/stop`, {
      method: "POST",
      body: jsonBody({}),
    });
  },
  listNotifications(unreadOnly = false) {
    return request<{ notifications: Notification[]; unread: number }>(
      `/api/notifications${unreadOnly ? "?unread=1" : ""}`,
    );
  },
  unreadNotificationCount() {
    return request<{ unread: number }>("/api/notifications/unread-count");
  },
  markNotificationRead(id: string) {
    return request<{ status: string }>(`/api/notifications/${id}/read`, {
      method: "POST",
      body: jsonBody({}),
    });
  },
  markAllNotificationsRead() {
    return request<{ status: string }>("/api/notifications/read-all", {
      method: "POST",
      body: jsonBody({}),
    });
  },
  listTeamReviews(status?: string) {
    const query = status ? `?status=${encodeURIComponent(status)}` : "";
    return request<{ reviews: TeamReview[] }>(`/api/team-reviews${query}`);
  },
  resolveTeamReview(id: string, decision: "approve" | "reject") {
    return request<{ review: TeamReview }>(`/api/team-reviews/${id}`, {
      method: "POST",
      body: jsonBody({ decision }),
    });
  },
  publishXPost(text: string, replyTo?: string) {
    return request<{ id: string; url: string }>("/api/x/publish", {
      method: "POST",
      body: jsonBody({ text, reply_to: replyTo ?? "" }),
    });
  },
  analyzeCapture(input: { duration: number; description: string; frames: string[] }) {
    return request<{ content: string; frames: number }>("/api/captures/analyze", {
      method: "POST",
      body: jsonBody(input),
    });
  },
  emailStatus() {
    return request<{ configured: boolean; imap?: boolean; smtp?: boolean; from?: string; imap_host?: string }>(
      "/api/email/status",
    );
  },
  emailInbox(limit = 20) {
    return request<{ messages: EmailMessage[]; threads: EmailThread[] }>(`/api/email/inbox?limit=${limit}`);
  },
  sendEmail(input: { to: string; cc?: string; subject: string; body: string }) {
    return request<{ status: string; to: string[] }>("/api/email/send", {
      method: "POST",
      body: jsonBody(input),
    });
  },
  listEmailDrafts() {
    return request<{ drafts: EmailDraft[] }>("/api/email/drafts");
  },
  createEmailDraft(input: { to: string; cc?: string; subject: string; body: string }) {
    return request<{ draft: EmailDraft }>("/api/email/drafts", {
      method: "POST",
      body: jsonBody(input),
    });
  },
  updateEmailDraft(id: string, input: { to: string; cc?: string; subject: string; body: string }) {
    return request<{ draft: EmailDraft }>(`/api/email/drafts/${id}`, {
      method: "PATCH",
      body: jsonBody(input),
    });
  },
  deleteEmailDraft(id: string) {
    return request<void>(`/api/email/drafts/${id}`, {
      method: "DELETE",
      body: jsonBody({}),
    });
  },
  listStoreTemplates() {
    return request<{ templates: StoreTemplate[] }>("/api/store/templates");
  },
  installStoreTemplate(id: string) {
    return request<{ template: Record<string, unknown>; name: string }>(
      `/api/store/templates/${id}/install`,
      { method: "POST", body: jsonBody({}) },
    );
  },
  billing() {
    return request<{
      plan: string;
      credits_micros: number;
      weekly_budget_micros: number;
      weekly_spent_micros: number;
      ledger: CreditLedgerEntry[];
    }>("/api/billing");
  },
  adjustCredits(amountMicros: number, note: string) {
    return request<{ credits_micros: number; entry: CreditLedgerEntry }>("/api/billing/credits", {
      method: "POST",
      body: jsonBody({ amount_micros: amountMicros, note }),
    });
  },
  auditLogs(limit = 100, offset = 0) {
    return request<{ logs: AuditLogEntry[] }>(`/api/audit-logs?limit=${limit}&offset=${offset}`);
  },
  pushStatus() {
    return request<{ available: boolean; public_key?: string; reason?: string }>("/api/push/status", {
      skipAuthRedirect: true,
    });
  },
  subscribePush(input: { endpoint: string; keys: { p256dh: string; auth: string } }) {
    return request<{ status: string }>("/api/push/subscribe", {
      method: "POST",
      body: jsonBody(input),
    });
  },
  unsubscribePush(endpoint: string) {
    return request<void>("/api/push/unsubscribe", {
      method: "POST",
      body: jsonBody({ endpoint }),
    });
  },
  testPush() {
    return request<{ sent: number }>("/api/push/test", { method: "POST", body: jsonBody({}) });
  },
  getLanguage() {
    return request<{ language: string }>("/api/settings/language", {
      skipAuthRedirect: true,
    });
  },
  setLanguage(language: "zh" | "en") {
    return request<{ language: string }>("/api/settings/language", {
      method: "PUT",
      body: jsonBody({ language }),
      skipAuthRedirect: true,
    });
  },
  getTimezone() {
    return request<{ timezone: string }>("/api/settings/timezone");
  },
  updateTimezone(timezone: string) {
    return request<{ timezone: string }>("/api/settings/timezone", {
      method: "PUT",
      body: jsonBody({ timezone }),
    });
  },
  getDataSharing() {
    return request<{ enabled: boolean }>("/api/settings/data-sharing", {
      skipAuthRedirect: true,
    });
  },
  updateDataSharing(enabled: boolean) {
    return request<{ enabled: boolean }>("/api/settings/data-sharing", {
      method: "PUT",
      body: jsonBody({ enabled }),
    });
  },
  reportClientError(input: { kind: string; message: string; stack?: string; url?: string }) {
    return request<void>("/api/diagnostics/client-error", {
      method: "POST",
      body: jsonBody(input),
    });
  },
  capabilities() {
    return request<{ capabilities: Array<{ key: string; status: string; reason?: string }> }>(
      "/api/capabilities",
    );
  },
  getOIDCStatus() {
    return request<{ configured: boolean; issuer?: string }>("/api/auth/oidc/status");
  },
  getNotificationPreferences() {
    return request<{ preferences: NotificationPreferences }>("/api/settings/notifications");
  },
  updateNotificationPreferences(preferences: NotificationPreferences) {
    return request<{ preferences: NotificationPreferences }>("/api/settings/notifications", {
      method: "PUT",
      body: jsonBody(preferences),
    });
  },
  listActivities(botId?: string) {
    const query = botId ? `?bot_id=${encodeURIComponent(botId)}` : "";
    return request<{ activities: Activity[] }>(`/api/activities${query}`);
  },
  markActivitiesUnread(botId: string) {
    return request<{ status: string }>("/api/activities/unread", {
      method: "POST",
      body: jsonBody({ bot_id: botId }),
    });
  },
  createBotShare(botId: string) {
    return request<{ token: string }>(`/api/bots/${botId}/share`, {
      method: "POST",
      body: jsonBody({}),
    });
  },
  getBotShare(token: string) {
    return request<{
      template: Record<string, unknown>;
      profile: {
        name: string;
        title: string;
        description: string;
        avatar_shape: string;
        avatar_color: string;
        skills: number;
        routines: number;
      };
    }>(`/api/shares/${encodeURIComponent(token)}`, { skipAuthRedirect: true });
  },
  setBotHidden(id: string, hidden: boolean) {
    return request<{ bot: Bot }>(`/api/bots/${id}/hidden`, {
      method: "PATCH",
      body: jsonBody({ hidden }),
    });
  },
  setTeamHidden(id: string, hidden: boolean) {
    return request<{ team: Team }>(`/api/teams/${id}/hidden`, {
      method: "PATCH",
      body: jsonBody({ hidden }),
    });
  },
  setTeamUnread(id: string, unread: boolean) {
    return request<{ status: string }>(`/api/teams/${id}/unread`, {
      method: "PATCH",
      body: jsonBody({ unread }),
    });
  },
  markActivitiesRead(botId?: string) {
    return request<{ status: string }>("/api/activities/read", {
      method: "POST",
      body: jsonBody(botId ? { bot_id: botId } : {}),
    });
  },
  search(query: string) {
    return request<{
      activities: Activity[];
      bots: Bot[];
      messages: Array<{
        id: string;
        conversation_id: string;
        conversation_title: string;
        content: string;
        sender_type: string;
        bot_name: string;
        created_at: string;
      }>;
    }>(`/api/search?q=${encodeURIComponent(query)}`);
  },
  getRun(id: string) {
    return request<{
      run: Run;
      tool_invocations: ToolInvocation[];
      events: EventEnvelope[];
    }>(`/api/runs/${id}`);
  },
};

export const SSE_EVENT_TYPES = [
  "message.created",
  "cycle.updated",
  "text_delta",
  "reasoning_delta",
  "tool_call",
  "tool_result",
  "status",
  "usage",
  "resync",
  "error",
  "tool_approval",
  "goal.updated",
] as const;
