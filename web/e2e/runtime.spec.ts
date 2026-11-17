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

  test("group routines are listed in the group's info panel", async ({ page }) => {
    await login(page);
    const lead = await createBot(page.request, "例程群主");
    const member = await createBot(page.request, "例程成员");
    const teamResponse = await page.request.post("/api/teams", {
      data: {
        name: `验收UI-例程群-${tag}`,
        mode: "group",
        config: { primary_bot_id: lead.id },
        bot_ids: [lead.id, member.id],
      },
    });
    const team = (await teamResponse.json()).team as { id: string };
    let routineId = "";
    try {
      const routineName = `验收UI例程-${tag}`;
      const created = await page.request.post("/api/routines", {
        data: { team_id: team.id, name: routineName, cron: "0 9 * * *", prompt: "汇报今天的进度。", enabled: false },
      });
      expect(created.ok(), await created.text()).toBeTruthy();
      routineId = ((await created.json()).routine as { id: string }).id;

      const conv = await conversationForTeam(page.request, team.id);
      await page.goto(`/?conv=${conv}`);
      const section = page.locator('section[data-section="group-routines"]');
      if (!(await section.isVisible().catch(() => false))) {
        await page.getByRole("button", { name: /详情|信息|Details/ }).first().click().catch(() => undefined);
      }
      await expect(section.getByText(routineName)).toBeVisible({ timeout: 20_000 });
      await expect(section.getByText("已停用")).toBeVisible();
      await page.screenshot({ path: path.join(shotsDir, "group-routines.png") });
    } finally {
      if (routineId) await page.request.delete(`/api/routines/${routineId}`, { data: {} });
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

  test("copy conversation id works over plain HTTP", async ({ page }) => {
    await login(page);
    const bot = await createBot(page.request, "复制");
    try {
      const created = await page.request.post("/api/conversations", { data: { kind: "direct", bot_id: bot.id } });
      const conv = (await created.json()).conversation.id as string;
      await page.goto(`/?conv=${conv}`);
      // The deployment is served over HTTP: no secure context, so the async
      // Clipboard API is missing and the fallback path is what gets tested.
      expect(await page.evaluate(() => window.isSecureContext)).toBe(false);
      await page.evaluate(() => {
        (window as unknown as { __copied: string | null }).__copied = null;
        document.addEventListener(
          "copy",
          () => {
            (window as unknown as { __copied: string | null }).__copied = document.getSelection()?.toString() ?? null;
          },
          true,
        );
      });
      await page.locator("aside").getByText(bot.name, { exact: true }).first().click({ button: "right" });
      await page.getByRole("menuitem", { name: "复制对话 ID" }).click();
      await expect(page.getByText("对话 ID 已复制")).toBeVisible();
      await expect(page.getByText("复制失败")).toHaveCount(0);
      const copied = await page.evaluate(() => (window as unknown as { __copied: string | null }).__copied);
      expect(copied).toBe(conv);
    } finally {
      await page.request.delete(`/api/bots/${bot.id}`, { data: {} });
    }
  });

  test("the primary bot can be replaced from the menu, its profile and settings", async ({ page }) => {
    test.setTimeout(180_000);
    await login(page);
    type ListedBot = {
      id: string;
      name: string;
      title: string;
      description: string;
      avatar: string;
      avatar_shape: string;
      avatar_color: string;
      section_id: string | null;
      position: number;
      pinned: boolean;
      created_by_bot_id: string | null;
    };
    const listBots = async () => ((await (await page.request.get("/api/bots")).json()).bots ?? []) as ListedBot[];
    const primaryId = async () => (await (await page.request.get("/api/auth/me")).json()).user.primary_bot_id as string;
    const original = await primaryId();
    // Handing over the role moves the old primary's own Bots out of their
    // sections; remember where they were so the account is left as found.
    const placements = (await listBots()).filter((bot) => bot.created_by_bot_id === original);
    const bot = await createBot(page.request, "主");
    const primaryCard = page.locator(".sb-primary-card").first();
    try {
      await page.goto("/");
      // 1. Sidebar menu of an ordinary Bot.
      await page.locator("aside").getByText(bot.name, { exact: true }).first().click({ button: "right" });
      await page.getByRole("menuitem", { name: "设为主 Bot" }).click();
      await expect(page.getByText(`${bot.name} 已成为主 Bot`)).toBeVisible();
      await expect.poll(primaryId).toBe(bot.id);
      await expect(primaryCard).toContainText(bot.name);

      // The primary card's own menu offers no delete, hide, pin or move.
      await primaryCard.click({ button: "right" });
      await expect(page.getByRole("menuitem", { name: "编辑 Bot 资料" })).toBeVisible();
      await expect(page.getByRole("menuitem", { name: "删除", exact: true })).toHaveCount(0);
      await expect(page.getByRole("menuitem", { name: "从侧边栏隐藏" })).toHaveCount(0);
      await expect(page.getByRole("menuitem", { name: "设为主 Bot" })).toHaveCount(0);

      // 2. The primary Bot's own profile can hand the role back.
      await page.getByRole("menuitem", { name: "编辑 Bot 资料" }).click();
      const replace = page.getByRole("combobox", { name: "更换主 Bot" });
      await expect(replace).toBeVisible();
      await page.screenshot({ path: path.join(shotsDir, "primary-replace-dialog.png") });
      await replace.selectOption(original);
      await expect.poll(primaryId).toBe(original);
      // The dialog stays on this Bot, which can now take the role back.
      await expect(page.getByRole("button", { name: "设为主 Bot", exact: true })).toBeVisible();
      await expect(replace).toHaveCount(0);
      await page.keyboard.press("Escape");

      // 3. Settings → General.
      await page.goto("/settings");
      const select = page.getByRole("combobox", { name: "主 Bot", exact: true });
      await expect(select).toHaveValue(original);
      await select.selectOption(bot.id);
      await expect.poll(primaryId).toBe(bot.id);
      await page.screenshot({ path: path.join(shotsDir, "primary-settings.png") });
      await select.selectOption(original);
      await expect.poll(primaryId).toBe(original);
    } finally {
      if ((await primaryId()) !== original) {
        await page.request.post("/api/account/primary-bot", { data: { bot_id: original } });
      }
      const now = new Map((await listBots()).map((item) => [item.id, item]));
      for (const before of placements) {
        const current = now.get(before.id);
        if (!current || current.section_id === before.section_id) continue;
        await page.request.patch(`/api/bots/${before.id}/profile`, {
          data: {
            name: current.name,
            title: current.title,
            description: current.description,
            avatar: current.avatar,
            avatar_shape: current.avatar_shape,
            avatar_color: current.avatar_color,
            section_id: before.section_id,
            position: before.position,
            pinned: current.pinned,
          },
        });
      }
      await page.request.delete(`/api/bots/${bot.id}`, { data: {} });
    }
    expect(await primaryId()).toBe(original);
  });

  test("a new bot comes with its own computer on standby", async ({ page }) => {
    test.setTimeout(180_000);
    await login(page);
    const bot = await createBot(page.request, "电脑");
    try {
      const created = await page.request.post("/api/conversations", { data: { kind: "direct", bot_id: bot.id } });
      const conv = (await created.json()).conversation.id as string;
      await page.goto(`/?conv=${conv}`);
      // The computer is created with the bot but not started.
      const readComputer = async () => (await (await page.request.get(`/api/bots/${bot.id}/computer`)).json()).computer;
      const first = await readComputer();
      expect(first.last_action?.kind).toBe("provision");
      await expect
        .poll(async () => (await readComputer()).last_action?.status, { timeout: 30_000 })
        .toBe("done");
      const standby = await readComputer();
      expect(standby.profile).toBe("browser");
      expect(standby.status).toBe("stopped");
      const card = page.locator("button", { hasText: /^Bot 的电脑/ });
      await expect(card).toContainText("待机中", { timeout: 15_000 });
      await card.click();
      await expect(page.getByText("电脑在待机")).toBeVisible();
      await page.getByRole("button", { name: "启动电脑", exact: true }).click();
      // The live view appears once the computer is up.
      await expect(page.locator('iframe[title="Bot computer live view"]')).toBeVisible({ timeout: 120_000 });
      await page.screenshot({ path: path.join(shotsDir, "computer-live.png") });
      const computer = (await (await page.request.get(`/api/bots/${bot.id}/computer`)).json()).computer;
      expect(computer.profile).toBe("browser");
      expect(computer.status).toBe("running");
      await page.keyboard.press("Escape");
      await page.goto(`/?conv=${conv}`);
      await expect(page.locator("button", { hasText: /^Bot 的电脑/ })).toContainText("运行中", { timeout: 30_000 });

      // The files tab switches between the bot's workspace and the shared drive.
      await page.locator("button.sb-tab", { hasText: "文件" }).click();
      await expect(page.getByText("这个 Bot 的私有工作区")).toBeVisible();
      await page.getByRole("tab", { name: "共享盘" }).click();
      await expect(page.getByRole("tab", { name: "共享盘" })).toHaveAttribute("aria-selected", "true");
      await expect(page.getByText("同一账户下所有 Bot 共用的盘")).toBeVisible();
      await expect(page.locator(".animate-spin")).toHaveCount(0, { timeout: 15_000 });
      await page.waitForTimeout(300); // let the pill colour transition finish
      await page.screenshot({ path: path.join(shotsDir, "shared-drive.png") });
    } finally {
      await page.request.post(`/api/bots/${bot.id}/computer/stop`, { data: {} });
      await page.request.delete(`/api/bots/${bot.id}`, { data: {} });
    }
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
