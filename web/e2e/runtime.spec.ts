import { test, expect, type Page, type APIRequestContext } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";

// Browser checks for the runtime features (group goal, side questions,
// checkpoint marker, memory panel). They run against a live deployment with
// its real model provider and clean up everything they create.

const envPath = process.env.SUPERBOT_E2E_ENV_FILE || "";
const shotsDir = path.join("test-results", "runtime");
const tag = Math.random().toString(36).slice(2, 8);

function credentials(): { email: string; password: string } | null {
  if (!envPath || !fs.existsSync(envPath)) return null;
  const values: Record<string, string> = {};
  for (const line of fs.readFileSync(envPath, "utf8").split(/\r?\n/)) {
    const match = line.match(/^([A-Za-z_][A-Za-z0-9_]*)=(.*)$/);
    if (match) values[match[1]] = match[2].replace(/^"|"$/g, "");
  }
  const email = values.SUPERBOT_ADMIN_EMAIL || "";
  const password = values.SUPERBOT_ADMIN_PASSWORD || "";
  return email && password ? { email, password } : null;
}

async function login(page: Page) {
  const auth = credentials()!;
  await page.goto("/login");
  await page.locator('input[type="email"]').fill(auth.email);
  await page.locator('input[type="password"]').fill(auth.password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page).not.toHaveURL(/\/login/);
}

async function providerAndModel(request: APIRequestContext) {
  const providers = (await (await request.get("/api/providers")).json()).providers as Array<{
    id: string;
    type: string;
    enabled: boolean;
  }>;
  const provider = providers.find((item) => item.type === "llm" && item.enabled)!;
  const models = (await (await request.get(`/api/providers/${provider.id}/models`)).json()).models as Array<{ name: string }>;
  return { providerId: provider.id, model: models[0].name };
}

async function createBot(request: APIRequestContext, name: string, prompt = "你是助理，回答简短。") {
  const { providerId, model } = await providerAndModel(request);
  const response = await request.post("/api/bots", {
    data: {
      name: `验收UI-${name}-${tag}`,
      slug: `ui-${tag}-${Math.random().toString(36).slice(2, 7)}`,
      system_prompt: prompt,
      provider_id: providerId,
      model,
      memory_enabled: false,
      computer_profile: "none",
    },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  return (await response.json()).bot as { id: string; name: string };
}

async function conversationForTeam(request: APIRequestContext, teamId: string): Promise<string> {
  const list = (await (await request.get("/api/conversations")).json()).conversations as Array<{
    id: string;
    team_id?: string | null;
  }>;
  const existing = list.find((item) => item.team_id === teamId);
  if (existing) return existing.id;
  const created = await request.post("/api/conversations", { data: { kind: "team", team_id: teamId } });
  return (await created.json()).conversation.id;
}

test.describe("runtime features in the browser", () => {
  test.skip(!credentials(), "SUPERBOT_E2E_ENV_FILE with admin credentials is required");
  test.setTimeout(240_000);

  test("group goal: draft through the form, see it in the panel, discard", async ({ page }) => {
    await login(page);
    const lead = await createBot(page.request, "群主");
    const member = await createBot(page.request, "成员");
    const teamResponse = await page.request.post("/api/teams", {
      data: {
        name: `验收UI-目标群-${tag}`,
        mode: "group",
        config: { primary_bot_id: lead.id },
        bot_ids: [lead.id, member.id],
      },
    });
    const team = (await teamResponse.json()).team as { id: string };
    try {
      const conv = await conversationForTeam(page.request, team.id);
      await page.goto(`/?conv=${conv}`);
      const section = page.locator("section[data-goal-status]");
      if (!(await section.isVisible().catch(() => false))) {
        await page.getByRole("button", { name: /详情|信息|Details/ }).first().click().catch(() => undefined);
      }
      await expect(section).toHaveAttribute("data-goal-status", "none", { timeout: 20_000 });
      await expect(section.getByText("给群聊设一个目标", { exact: false })).toBeVisible();

      await section.getByRole("button", { name: "设定目标" }).click();
      const dialog = page.getByRole("dialog");
      await dialog.getByPlaceholder("一句话说明要达成什么").fill("为笔记应用写一句口号");
      await dialog.getByPlaceholder("怎样才算完成，必须能被检查").fill("群里公布不超过 20 字的口号");
      await dialog.getByLabel("阶段 1 名称").fill("起草");
      await dialog.getByLabel("阶段 1 完成标准").fill("写手给出一句口号");
      await dialog.getByRole("button", { name: "保存草稿" }).click();

      await expect(section).toHaveAttribute("data-goal-status", "drafting", { timeout: 15_000 });
      await expect(section.getByText("为笔记应用写一句口号")).toBeVisible();
      await expect(section.getByRole("button", { name: "开始" })).toBeVisible();
      const api = (await (await page.request.get(`/api/conversations/${conv}/goal`)).json()) as {
        goal: { status: string; stages: Array<{ title: string; tasks: unknown[] }> };
        lead_bot_id: string;
      };
      expect(api.goal.status).toBe("drafting");
      expect(api.goal.stages[0].title).toBe("起草");
      expect(Array.isArray(api.goal.stages[0].tasks)).toBeTruthy();
      expect(api.lead_bot_id).toBe(lead.id);
      await page.screenshot({ path: path.join(shotsDir, "goal-draft.png") });

      await section.getByRole("button", { name: "放弃" }).click();
      await page.getByRole("dialog").getByRole("button", { name: "放弃" }).click();
      await expect(section).toHaveAttribute("data-goal-status", "stopped", { timeout: 15_000 });
      await expect(section.getByRole("button", { name: "设定新目标" })).toBeVisible();
    } finally {
      await page.request.delete(`/api/teams/${team.id}`, { data: {} });
      await page.request.delete(`/api/bots/${lead.id}`, { data: {} });
      await page.request.delete(`/api/bots/${member.id}`, { data: {} });
    }
  });

  test("side question is answered without touching the conversation", async ({ page }) => {
    await login(page);
    const bot = await createBot(page.request, "旁路");
    try {
      const created = await page.request.post("/api/conversations", { data: { kind: "direct", bot_id: bot.id } });
      const conv = (await created.json()).conversation.id as string;
      const before = ((await (await page.request.get(`/api/conversations/${conv}`)).json()).messages as unknown[]).length;

      await page.goto(`/?conv=${conv}`);
      await page.locator("[data-side-toggle]").click();
      const panel = page.getByRole("complementary", { name: "旁路提问" }).or(page.locator('[aria-label="旁路提问"]').last());
      await expect(page.getByText("不会进入主对话，也不会打断正在进行的任务")).toBeVisible();
      await page.getByLabel("问一个旁路问题…").fill("1 加 1 等于几？只回答数字。");
      await page.getByRole("button", { name: "提问", exact: true }).click();
      await expect(page.getByText("正在回答…")).toBeVisible();
      await expect(panel.getByText("2", { exact: true }).first()).toBeVisible({ timeout: 90_000 });
      await page.screenshot({ path: path.join(shotsDir, "side-answer.png") });

      const after = ((await (await page.request.get(`/api/conversations/${conv}`)).json()).messages as unknown[]).length;
      expect(after).toBe(before);
      const listed = (await (await page.request.get(`/api/conversations/${conv}/side`)).json()).side_chats as Array<{
        status: string;
      }>;
      expect(listed.length).toBe(1);
      expect(listed[0].status).toBe("answered");
    } finally {
      await page.request.delete(`/api/bots/${bot.id}`, { data: {} });
    }
  });

  test("checkpoint marker opens the exact checkpoint", async ({ page }) => {
    const conv = process.env.SUPERBOT_E2E_CHECKPOINT_CONV || "";
    test.skip(!conv, "SUPERBOT_E2E_CHECKPOINT_CONV is not set");
    await login(page);
    await page.goto(`/?conv=${conv}`);
    const marker = page.getByText(/已整理较早的对话/).first();
    await marker.scrollIntoViewIfNeeded();
    await expect(marker).toBeVisible({ timeout: 20_000 });
    await marker.click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByText("Bot 之后只看到这份笔记", { exact: false })).toBeVisible();
    await expect(dialog.getByText("## 目标", { exact: false })).toBeVisible();
    await expect(dialog.getByText("用户原话", { exact: false })).toBeVisible();
    await page.screenshot({ path: path.join(shotsDir, "checkpoint-dialog.png") });
  });

  test("memory panel shows pins, archive and tidy", async ({ page }) => {
    await login(page);
    const content = `验收UI记忆-${tag}：周报用表格格式`;
    const created = await page.request.post("/api/memories", { data: { content, pinned: true } });
    expect(created.ok(), await created.text()).toBeTruthy();
    const memory = (await created.json()).memory as { id: string; pinned: boolean };
    expect(memory.pinned).toBeTruthy();
    const secret = await page.request.post("/api/memories", { data: { content: "数据库 password=Zx9!kLm2Qw8#Rt5" } });
    expect(secret.status()).toBe(400);
    const bot = await createBot(page.request, "记忆面板");
    try {
      const createdConv = await page.request.post("/api/conversations", { data: { kind: "direct", bot_id: bot.id } });
      const conv = (await createdConv.json()).conversation.id as string;
      await page.goto(`/?conv=${conv}`);
      await page.locator('[aria-label="编辑 Bot 资料"], [title="编辑 Bot 资料"]').first().click({ force: true });
      const dialog = page.getByRole("dialog");
      await dialog.getByRole("button", { name: "记忆", exact: true }).or(dialog.getByRole("tab", { name: "记忆" })).first().click();
      await dialog.getByRole("button", { name: "我的记忆" }).or(dialog.getByRole("tab", { name: "我的记忆" })).first().click();
      await expect(page.getByText(content)).toBeVisible({ timeout: 20_000 });
      await expect(page.getByText("显示已归档").first()).toBeVisible();
      await expect(page.getByRole("button", { name: "整理重复" }).first()).toBeVisible();
      await page.screenshot({ path: path.join(shotsDir, "memory-panel.png") });
    } finally {
      await page.request.post(`/api/memories/${memory.id}/pin`, { data: { pinned: false } });
      await page.request.delete(`/api/memories/${memory.id}`, { data: {} });
      await page.request.delete(`/api/bots/${bot.id}`, { data: {} });
    }
  });
});
