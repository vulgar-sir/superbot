export interface User {
  id: string;
  email: string;
  display_name: string;
  role: "admin" | "member";
  primary_bot_id: string | null;
}

export interface Provider {
  id: string;
  type: "llm" | "embedding" | "search";
  kind: string;
  name: string;
  base_url: string;
  has_key: boolean;
  config: Record<string, unknown>;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface Model {
  id: string;
  provider_id: string;
  name: string;
  context_window: number;
  max_output: number;
  supports_tools: boolean;
  supports_vision: boolean;
  supports_reasoning: boolean;
  price_in_micros_per_mtok: number;
  price_cache_micros_per_mtok: number;
  price_out_micros_per_mtok: number;
}

export interface Bot {
  id: string;
  name: string;
  slug: string;
  avatar: string;
  description: string;
  system_prompt: string;
  provider_id: string | null;
  model: string;
  params: Record<string, unknown>;
  capabilities: Record<string, unknown>;
  tool_policy: Record<string, unknown>;
  memory_enabled: boolean;
  computer_profile: "none" | "browser" | "desktop";
  kind: "personal" | "team";
  visibility?: "owner" | "account";
  created_by_user_id?: string | null;
  created_by_bot_id: string | null;
  section_id: string | null;
  position: number;
  title: string;
  avatar_shape: string;
  avatar_color: string;
  pinned: boolean;
  last_activity_at: string | null;
  proactivity: Record<string, unknown>;
  hidden: boolean;
  marked_unread: boolean;
  latest_activity?: ActivityPreview | null;
  unread_count?: number;
  created_at: string;
  updated_at: string;
}

export interface ActivityPreview {
  kind: string;
  summary: string;
  agent_thread_id: string | null;
  created_at: string;
}

export interface Activity {
  id: string;
  bot_id: string;
  bot_name: string;
  kind: string;
  summary: string;
  conversation_id: string | null;
  message_id: string | null;
  agent_thread_id: string | null;
  read: boolean;
  created_at: string;
}

export interface AgentThread {
  id: string;
  conversation_id: string;
  bot_a_id: string;
  bot_b_id: string;
  origin_conversation_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface AgentThreadDetail {
  thread: AgentThread;
  conversation: Conversation;
  messages: Message[];
  bot_a: Bot;
  bot_b: Bot;
}

export interface Conversation {
  id: string;
  team_id: string | null;
  direct_bot_id: string | null;
  // "agent": the private thread of two bots, shown read-only.
  kind: "team" | "direct" | "agent";
  title: string;
  status: string;
  budget_micros: number;
  spent_micros: number;
  last_event_seq: number;
  created_at: string;
  updated_at: string;
}

export interface TeamBot {
  bot: Bot;
  position: number;
  role: string;
}

export interface Team {
  id: string;
  name: string;
  description: string;
  mode: string;
  config: {
    max_rounds?: number;
    coordinator_provider_id?: string;
    coordinator_model?: string;
    [key: string]: unknown;
  };
  bots: TeamBot[];
  section_id: string | null;
  position: number;
  pinned: boolean;
  room_conversation_id: string | null;
  hidden: boolean;
  marked_unread: boolean;
  created_at: string;
  updated_at: string;
}

export interface SidebarSection {
  id?: string;
  name: string;
  position: number;
  collapsed: boolean;
  bots: Bot[];
  groups: Team[];
}

export interface SidebarData {
  sections: SidebarSection[];
  unassigned: SidebarSection;
}

export interface RouteDecision {
  round: number;
  action: string;
  next_bot_id: string;
  next_bot_name: string;
  instruction: string;
  reason: string;
  coordinator_model: string;
  cost_micros: number;
}

export interface Skill {
  id: string;
  name: string;
  description: string;
  description_en?: string;
  version: string;
  source: string;
  status: "draft" | "active" | "disabled";
  created_by: "user" | "bot";
  visibility: "owner" | "account";
  created_by_user_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface BotSkill {
  skill: Skill;
  enabled: boolean;
}

export interface Connector {
  id: string;
  provider: string;
  name: string;
  auth_type: string;
  status: string;
  config: Record<string, unknown>;
  created_at: string;
}

export interface GitHubDeviceStart {
  device_code: string;
  user_code: string;
  verification_uri: string;
  expires_in: number;
  interval: number;
}

export interface McpServer {
  id: string;
  name: string;
  transport: string;
  url: string;
  command?: string;
  args?: string[];
  has_env?: boolean;
  status: "draft" | "active" | "disabled";
  created_by: "user" | "bot";
  has_headers: boolean;
  created_at: string;
  updated_at: string;
}

export interface BotMcpServer {
  mcp_server: McpServer;
  enabled: boolean;
}

export interface ComputerInfo {
  bot_id: string;
  profile: string;
  status: string;
  desired_state: string;
  vnc_url: string;
  last_action?: {
    id: string;
    kind: "update" | "reset" | "snapshot" | "restore" | "provision";
    status: "pending" | "running" | "done" | "failed";
    error: string;
    created_at: string;
    processed_at: string | null;
  } | null;
  updated_at: string;
  disk_bytes?: number;
  disk_measured_at?: string;
}

export interface UsageDaily {
  day: string;
  tokens: number;
  cost_micros: number;
}

export interface UsageByBot {
  bot_id: string | null;
  bot_name: string;
  prompt_tokens: number;
  completion_tokens: number;
  cost_micros: number;
}

export interface UsageData {
  from: string;
  to: string;
  totals: {
    prompt_tokens: number;
    cached_tokens: number;
    completion_tokens: number;
    cost_micros: number;
  };
  by_bot: UsageByBot[];
  by_model: Array<{
    model: string;
    prompt_tokens: number;
    completion_tokens: number;
    cost_micros: number;
  }>;
  daily: UsageDaily[];
  weekly_budget_micros: number;
  weekly_spent_micros: number;
  week_reset_at: string;
}

export interface Memory {
  id: string;
  content: string;
  scope_type: string;
  created_at: string;
  updated_at?: string;
  source?: "manual" | "tool" | "review" | "import" | "tidy";
  pinned?: boolean;
  archived_at?: string | null;
  // Set on an archived memory a newer one replaced.
  superseded_by?: string | null;
  // No search vector yet (embedding service was down); indexed in the background.
  pending?: boolean;
}

// Result of saving one memory by hand.
export interface MemoryCreateResult {
  memory: Memory;
  // The content matched an existing memory, which was refreshed instead.
  duplicate?: boolean;
  // Stored, but not searchable by meaning until the background indexing runs.
  pending?: boolean;
  // Older memories retired because the new one replaces them.
  superseded?: Memory[];
  // Similar memories left as they were; the user may retire outdated ones.
  related?: Memory[];
  pinned_conflicts?: Memory[];
}

export interface MemoryImportResult {
  imported: number;
  // Lines that matched an existing memory and only refreshed it.
  duplicates?: number;
  // Lines refused because they look like credentials.
  rejected?: number;
  memories: Memory[];
}

export interface MemoryTidyReport {
  scope_type: string;
  scope_id: string;
  scanned: number;
  archived: Array<{
    id: string;
    content: string;
    kept_id: string;
    reason: string;
    similarity: number;
  }> | null;
}

export interface ConversationSummary {
  id: string;
  conversation_id: string;
  up_to_message_id: string | null;
  up_to_seq: number;
  content: string;
  tokens: number;
  source?: "manual" | "auto" | "fallback";
  created_at: string;
}

export type GoalStatus = "drafting" | "active" | "paused" | "complete" | "stopped";

export interface GoalTask {
  bot_id: string;
  bot_name: string;
  task: string;
  status: "todo" | "doing" | "done";
  note: string;
}

export interface GoalStage {
  title: string;
  acceptance: string;
  status: "pending" | "active" | "passed";
  tasks: GoalTask[];
  reworks: number;
  evidence: string;
  feedback: string;
}

export interface GoalEvent {
  at: string;
  actor: string;
  action: string;
  detail: string;
}

export interface Goal {
  status: GoalStatus;
  objective: string;
  acceptance: string;
  constraints: string[];
  stages: GoalStage[];
  // 0-based index of the stage in progress; equals stages.length once every
  // stage has passed.
  current: number;
  evidence: string;
  nudges: number;
  events: GoalEvent[];
}

export interface GoalMember {
  id: string;
  name: string;
}

export interface GoalResponse {
  goal: Goal | null;
  lead_bot_id?: string;
  members: GoalMember[];
  updated_at?: string | null;
  message?: string;
}

export interface GoalAction {
  action:
    | "draft"
    | "start"
    | "revise"
    | "assign"
    | "report"
    | "pass"
    | "rework"
    | "complete"
    | "pause"
    | "resume"
    | "stop"
    | "status";
  objective?: string;
  acceptance?: string;
  constraints?: string[];
  stages?: { title: string; acceptance: string }[];
  // 1-based stage number; 0 or omitted means the current stage.
  stage?: number;
  bot?: string;
  task?: string;
  status?: string;
  note?: string;
  evidence?: string;
  feedback?: string;
}

export interface SideChat {
  id: string;
  bot_id: string | null;
  question: string;
  answer: string;
  status: "answering" | "answered" | "failed";
  error: string;
  created_at: string;
  answered_at: string | null;
}

export interface Device {
  id: string;
  name: string;
  platform: string;
  status: "online" | "offline";
  allow_bot: boolean;
  last_seen_at: string | null;
  created_at: string;
}

export interface VaultItem {
  id: string;
  kind: "login" | "payment" | "contact";
  name: string;
  username: string;
  url: string;
  has_password: boolean;
  has_notes: boolean;
  created_at: string;
  updated_at: string;
}

export interface VaultReveal {
  item: VaultItem;
  password: string;
  notes: string;
}

export interface LibraryItem {
  id: string;
  bot_id: string;
  kind: "page" | "document" | "file" | "media";
  title: string;
  description: string;
  mime_type: string;
  conversation_id: string | null;
  message_id: string | null;
  metadata: Record<string, unknown>;
  file_url?: string;
  created_at: string;
  updated_at: string;
}

export interface LibraryItemDetail {
  item: LibraryItem;
  content: string;
}

export interface BotRoutine {
  id: string;
  bot_id: string;
  name: string;
  schedule: string;
  timezone: string;
  timing: "exact" | "flexible";
  enabled: boolean;
  paused: boolean;
  prompt: string;
  last_run_at: string | null;
  next_run_at: string | null;
  trigger_token: string;
  created_at: string;
  updated_at: string;
}

export interface Routine {
  id: string;
  team_id: string;
  name: string;
  cron: string;
  prompt: string;
  enabled: boolean;
  last_run_at: string | null;
  next_run_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface PermissionRule {
  id: string;
  kind: "always_ask" | "allow";
  pattern: string;
  created_at: string;
}

export interface ToolApproval {
  id: string;
  conversation_id: string;
  run_id: string;
  call_id: string;
  tool_name: string;
  args: Record<string, unknown>;
  status: string;
  created_at: string;
}

export interface Attachment {
  id: string;
  filename: string;
  mime_type: string;
  size_bytes: number;
  url: string;
}

export interface MessageReaction {
  emoji: string;
  count: number;
  reacted: boolean;
}

export interface PollOption {
  id: string;
  text: string;
  votes: number;
  voted: boolean;
}

export interface Poll {
  question: string;
  multiple: boolean;
  options: PollOption[];
  total_votes: number;
}

export interface Message {
  id: string;
  ordinal: number;
  cycle_id: string | null;
  sender_type: "user" | "bot" | "system";
  sender_bot_id: string | null;
  content: string;
  metadata: Record<string, unknown>;
  run_id: string | null;
  parent_message_id: string | null;
  reactions: MessageReaction[];
  attachments: Attachment[];
  poll?: Poll | null;
  edited_at?: string | null;
  pinned_at?: string | null;
  created_at: string;
}

export interface Run {
  id: string;
  cycle_id: string;
  bot_id: string;
  status: string;
  step: number;
  usage_in: number;
  usage_out: number;
  cost_micros: number;
  warning: string;
  error: string;
  started_at: string | null;
  finished_at: string | null;
  created_at: string;
}

export interface ToolInvocation {
  id: string;
  call_id: string;
  name: string;
  args: Record<string, unknown>;
  status: string;
  result_artifact: string;
  error: string;
  started_at: string | null;
  finished_at: string | null;
}

export interface EventEnvelope {
  seq: number;
  type: string;
  payload: Record<string, any>;
  created_at: string;
}

export interface StreamState {
  runId: string;
  botId: string;
  text: string;
  reasoning: string;
  // How far into the conversation the bot had read when this reply started
  // (or since): later user messages are shown below it.
  seen: number;
}

export interface ToolActivity {
  callId: string;
  name: string;
  args: Record<string, unknown>;
  result?: string;
  isError?: boolean;
  status: "running" | "done" | "failed";
  widgetHtml?: string;
}

export interface Notification {
  id: string;
  kind: string;
  title: string;
  body: string;
  resource_type: string;
  resource_id: string;
  metadata: Record<string, unknown>;
  read: boolean;
  created_at: string;
}

export interface TeamReview {
  id: string;
  team_bot_id: string;
  kind: string;
  resource_id: string | null;
  title: string;
  description: string;
  content: string;
  status: "pending" | "approved" | "rejected";
  submitted_by_bot_id: string | null;
  reviewed_by_user_id: string | null;
  created_at: string;
  reviewed_at: string | null;
}

export type NotificationSound = "default" | "chime" | "soft" | "none";

export interface NotificationPreferences {
  desktop: boolean;
  sound: NotificationSound;
  volume: number;
  messages: boolean;
  approvals: boolean;
  agents: boolean;
}

export interface SubagentTask {
  id: string;
  parent_conversation_id: string;
  child_conversation_id: string;
  parent_task_id: string | null;
  depth: number;
  objective: string;
  status: "queued" | "running" | "succeeded" | "failed" | "canceled" | "timed_out";
  result_summary: string;
  error: string;
  created_at: string;
  finished_at: string | null;
}

export interface TaskItem {
  id: string;
  project_id: string | null;
  title: string;
  description: string;
  status: "todo" | "in_progress" | "blocked" | "done" | "canceled";
  priority: number;
  assignee_bot_id: string | null;
  due_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface Account {
  id: string;
  name: string;
  role: "owner" | "admin" | "member";
  active: boolean;
  primary_bot_id: string | null;
  created_at: string;
}

export interface ProjectItem {
  id: string;
  name: string;
  description: string;
  visibility: "owner" | "account";
  created_by_user_id: string | null;
  created_at: string;
}

export interface BotTemplateItem {
  id: string;
  name: string;
  description: string;
  version: number;
  bot_id: string | null;
  visibility: "owner" | "account";
  status: "published" | "disabled";
  created_by_user_id: string | null;
  created_at: string;
  updated_at: string;
  template?: Record<string, unknown>;
}

export interface DocumentItem {
  id: string;
  title: string;
  content: string;
  kind: "document" | "canvas";
  revision: number;
  visibility: "owner" | "account";
  created_by_user_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface CalendarEvent {
  id: string;
  title: string;
  description: string;
  location: string;
  starts_at: string;
  ends_at: string;
  created_at: string;
}

export interface MeetingItem {
  id: string;
  title: string;
  transcript?: string;
  summary: string;
  starts_at: string;
  created_at: string;
  updated_at: string;
}

export interface DocumentVersion {
  id: string;
  title: string;
  content: string;
  kind: "document" | "canvas";
  created_at: string;
}

export interface EmailAttachment {
  filename: string;
  mime_type: string;
  size: number;
}

export interface EmailMessage {
  uid: number;
  subject: string;
  from: string;
  date: string;
  message_id?: string;
  in_reply_to?: string[];
  thread_id?: string;
  body?: string;
  attachments?: EmailAttachment[];
}

export interface EmailThread {
  id: string;
  subject: string;
  messages: EmailMessage[];
}

export interface EmailDraft {
  id: string;
  to: string;
  cc: string;
  subject: string;
  body: string;
  created_at: string;
  updated_at: string;
}

export interface AuditLogEntry {
  id: string;
  actor_type: string;
  actor_id: string | null;
  action: string;
  target_type: string;
  target_id: string | null;
  payload: Record<string, unknown>;
  created_at: string;
}

export interface BotFileEntry {
  name: string;
  path: string;
  is_dir: boolean;
  size: number;
  modified: string;
}

export interface CreditLedgerEntry {
  id: string;
  kind: string;
  amount_micros: number;
  balance_after_micros: number;
  note: string;
  created_by_user_id: string | null;
  created_at: string;
}

export interface StoreTemplate {
  id: string;
  name: string;
  description: string;
  version: number;
  install_count: number;
  owner_name: string;
  created_at: string;
}
