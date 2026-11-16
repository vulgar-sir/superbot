import { test, expect, type Page } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";

const envPath = process.env.SUPERBOT_E2E_ENV_FILE || "";
const shotsDir = path.join("test-results", "parity");

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

async function bots(page: Page): Promise<Array<{ id: string; name: string; is_room?: boolean }>> {
  const response = await page.request.get("/api/bots");
  const data = await response.json();
  return data.bots ?? data;
}

test.describe("superbot functional matrix", () => {
  test.skip(!credentials(), "SUPERBOT_E2E_ENV_FILE with admin credentials is required");

  test("vault item can be created, revealed, edited and deleted", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/vault", {
      data: { kind: "login", name: "E2E Vault", username: "e2e-user", password: "e2e-pass", url: "https://example.com" },
    });
    expect(created.ok()).toBeTruthy();
    const item = (await created.json()).item;
    try {
      const reveal = await page.request.get(`/api/vault/${item.id}/reveal`);
      const revealed = await reveal.json();
      expect(revealed.password).toBe("e2e-pass");
      const edited = await page.request.patch(`/api/vault/${item.id}`, {
        data: { kind: "login", name: "E2E Vault edited", password: "" },
      });
      expect((await edited.json()).item.name).toBe("E2E Vault edited");

      await page.goto("/settings");
      await page.locator('a[href="#vault"]').click();
      await expect(page.getByText("E2E Vault edited", { exact: true }).first()).toBeVisible();
      await page.screenshot({ path: path.join(shotsDir, "matrix-vault.png"), fullPage: false });
    } finally {
      await page.request.delete(`/api/vault/${item.id}`, { data: {} });
    }
    const list = await page.request.get("/api/vault");
    const items = (await list.json()).items as Array<{ name: string }>;
    expect(items.some((entry) => entry.name.startsWith("E2E Vault"))).toBe(false);
  });

  test("user memory can be created, edited and deleted", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/memories", { data: { content: "E2E 用户记忆" } });
    expect(created.ok()).toBeTruthy();
    const memory = (await created.json()).memory;
    try {
      const updated = await page.request.put(`/api/memories/${memory.id}`, { data: { content: "E2E 用户记忆-更新" } });
      expect((await updated.json()).memory.content).toBe("E2E 用户记忆-更新");
      const list = await page.request.get("/api/memories");
      const contents = ((await list.json()).memories as Array<{ content: string }>).map((entry) => entry.content);
      expect(contents).toContain("E2E 用户记忆-更新");
    } finally {
      await page.request.delete(`/api/memories/${memory.id}`, { data: {} });
    }
    const list = await page.request.get("/api/memories");
    const contents = ((await list.json()).memories as Array<{ content: string }>).map((entry) => entry.content);
    expect(contents).not.toContain("E2E 用户记忆-更新");
  });

  test("bot routine can be created, edited and deleted", async ({ page }) => {
    await login(page);
    const bot = (await bots(page)).find((entry) => !entry.is_room)!;
    const created = await page.request.post("/api/bot-routines", {
      data: { bot_id: bot.id, name: "E2E 例程", schedule: "0 9 * * *", prompt: "E2E routine body", enabled: true },
    });
    expect(created.ok()).toBeTruthy();
    const routine = (await created.json()).routine;
    try {
      const updated = await page.request.patch(`/api/bot-routines/${routine.id}`, {
        data: { name: "E2E 例程-更新", enabled: false },
      });
      const payload = (await updated.json()).routine;
      expect(payload.name).toBe("E2E 例程-更新");
      expect(payload.enabled).toBe(false);
      const list = await page.request.get("/api/bot-routines");
      expect(((await list.json()).routines as Array<{ name: string }>).some((entry) => entry.name === "E2E 例程-更新")).toBe(true);
    } finally {
      await page.request.delete(`/api/bot-routines/${routine.id}`, { data: {} });
    }
  });

  test("team members can be added and removed", async ({ page }) => {
    await login(page);
    const available = (await bots(page)).filter((entry) => !entry.is_room).slice(0, 2);
    expect(available.length).toBeGreaterThanOrEqual(2);
    const created = await page.request.post("/api/teams", {
      data: {
        name: "E2E 矩阵群聊",
        description: "matrix spec",
        mode: "group",
        config: {},
        bot_ids: [available[0].id],
      },
    });
    expect(created.ok()).toBeTruthy();
    const team = (await created.json()).team;
    try {
      const added = await page.request.put(`/api/teams/${team.id}/bots`, {
        data: { bots: available.map((bot, index) => ({ bot_id: bot.id, position: index })) },
      });
      expect((await added.json()).team.bots).toHaveLength(2);
      const removed = await page.request.put(`/api/teams/${team.id}/bots`, {
        data: { bots: [{ bot_id: available[0].id, position: 0 }] },
      });
      expect((await removed.json()).team.bots).toHaveLength(1);
    } finally {
      await page.request.delete(`/api/teams/${team.id}`, { data: {} });
    }
  });

  test("bot avatar can be uploaded and removed", async ({ page }) => {
    await login(page);
    // Use a throwaway bot: deleting an avatar removes its file, so the test
    // must never touch a real bot that may already have an uploaded image.
    const created = await page.request.post("/api/bots", { data: { name: "E2E Avatar Bot" } });
    expect(created.ok()).toBeTruthy();
    const bot = (await created.json()).bot;
    const png = Buffer.from(
      "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
      "base64",
    );
    try {
      const uploaded = await page.request.post(`/api/bots/${bot.id}/avatar`, {
        multipart: { file: { name: "matrix.png", mimeType: "image/png", buffer: png } },
      });
      expect(uploaded.ok()).toBeTruthy();
      expect((await uploaded.json()).bot.avatar).toContain("/api/bots/");
      const removed = await page.request.delete(`/api/bots/${bot.id}/avatar`, { data: {} });
      expect(removed.ok()).toBeTruthy();
    } finally {
      await page.request.delete(`/api/bots/${bot.id}`, { data: {} });
    }
  });

  test("primary bot can be switched and restored", async ({ page }) => {
    await login(page);
    const listed = (await bots(page)).filter((entry) => !entry.is_room);
    const meBefore = await (await page.request.get("/api/auth/me")).json();
    const original = meBefore.user.primary_bot_id as string;
    const target = listed.find((entry) => entry.id !== original);
    expect(target).toBeTruthy();
    try {
      const switched = await page.request.post("/api/account/primary-bot", { data: { bot_id: target!.id } });
      expect(switched.ok()).toBeTruthy();
      const meAfter = await (await page.request.get("/api/auth/me")).json();
      expect(meAfter.user.primary_bot_id).toBe(target!.id);
    } finally {
      await page.request.post("/api/account/primary-bot", { data: { bot_id: original } });
    }
    const meRestored = await (await page.request.get("/api/auth/me")).json();
    expect(meRestored.user.primary_bot_id).toBe(original);
  });

  test("notification API and capability states are honest", async ({ page }) => {
    await login(page);
    const notifications = await page.request.get("/api/notifications");
    expect(notifications.ok()).toBeTruthy();
    const payload = await notifications.json();
    expect(Array.isArray(payload.notifications)).toBe(true);

    const capabilities = await page.request.get("/api/capabilities");
    const data = await capabilities.json();
    const keys = (data.capabilities as Array<{ key: string; status: string }>).map((entry) => entry.key);
    expect(keys).toContain("webauthn");
    expect(keys).toContain("web_push");
    expect(keys).toContain("memory_semantic");
    const push = (data.capabilities as Array<{ key: string; status: string }>).find((entry) => entry.key === "web_push");
    expect(push?.status).toBe("available");
  });
});
