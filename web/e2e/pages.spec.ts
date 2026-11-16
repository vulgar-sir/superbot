import { test, expect, type Page } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";

const envPath = process.env.SUPERBOT_E2E_ENV_FILE || "";
const shotsDir = path.join("test-results", "parity");

function credentials() {
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
  await expect(page.locator("button[aria-label='账户菜单']")).toBeVisible();
}

test.describe("superbot management pages", () => {
  test.skip(!credentials(), "SUPERBOT_E2E_ENV_FILE with admin credentials is required");

  test("bot access and project ACL are operable", async ({ page }) => {
    await login(page);

    await page.goto("/tasks");
    await page.getByRole("button", { name: "项目", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "项目管理" });
    await expect(dialog).toBeVisible();
    await dialog.getByPlaceholder("新项目名称").fill("E2E 项目");
    await dialog.getByRole("button", { name: "创建", exact: true }).click();
    await expect(dialog.getByText("E2E 项目", { exact: true })).toBeVisible();
    await dialog.getByText("E2E 项目", { exact: true }).click();
    await expect(dialog.getByText("成员权限")).toBeVisible();
    await dialog.getByRole("button", { name: "删除项目" }).click();
    await expect(dialog.getByText("还没有项目")).toBeVisible();
    await dialog.getByRole("button", { name: "关闭", exact: true }).last().click();

    await page.goto("/");
    const editBot = page.getByRole("button", { name: "编辑 Bot 资料" });
    await expect(editBot).toHaveCount(1);
    await editBot.evaluate((element) => (element as HTMLElement).click());
    await page.getByRole("button", { name: "访问", exact: true }).click();
    await expect(page.getByText("owner/admin 默认可编辑")).toBeVisible();
    await expect(page.getByRole("dialog").getByText(/admin@superbot\.local/).first()).toBeVisible();
  });

  test("task lifecycle creates, completes and deletes", async ({ page }) => {
    await login(page);
    await page.goto("/tasks");
    await page.getByPlaceholder("新任务标题").fill("E2E 任务");
    await page.getByPlaceholder("描述（可选）").fill("Playwright 生命周期验证");
    await page.getByRole("main").getByRole("button", { name: "创建", exact: true }).click();
    await expect(page.getByText("E2E 任务", { exact: true })).toBeVisible();
    const taskRow = page.getByText("E2E 任务", { exact: true }).locator("xpath=ancestor::div[contains(@class,'items-center')][1]");
    await taskRow.getByRole("combobox").selectOption("done");
    await expect(page.getByText("E2E 任务", { exact: true })).toHaveClass(/line-through/);
    await page.getByRole("button", { name: "删除任务" }).first().click();
    await expect(page.getByText("E2E 任务", { exact: true })).toHaveCount(0);
  });

  test("search and sidebar create menu are reachable", async ({ page }) => {
    await login(page);
    await page.locator("button[aria-label='搜索']").first().click();
    const search = page.getByPlaceholder("搜索 Bot、聊天、设置…");
    await expect(search).toBeVisible();
    await search.fill("生产");
    await expect(page.getByText("生产调试助手").first()).toBeVisible();
    await page.keyboard.press("Escape");

    await page.locator("button[aria-label='新建']").first().click();
    await expect(page.getByText("创建新 Bot", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "取消", exact: true }).click();
    const sidebar = page.locator('aside[aria-label="Bot"] .sb-sidebar-scroll');
    await sidebar.evaluate((element) => {
      element.dispatchEvent(new MouseEvent("contextmenu", {
        bubbles: true,
        cancelable: true,
        button: 2,
        clientX: 120,
        clientY: 500,
      }));
    });
    await expect(page.getByRole("menuitem", { name: "新建 Team Bot" })).toBeVisible();
    await page.keyboard.press("Escape");
  });

  test("documents can be created and deleted", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E 文档", content: "document body", kind: "document" },
    });
    expect(created.ok()).toBeTruthy();
    await page.goto("/documents");
    await expect(page.getByText("E2E 文档", { exact: true }).first()).toBeVisible();
    await page.getByRole("button", { name: "删除文档" }).click();
    await expect(page.getByText("E2E 文档", { exact: true })).toHaveCount(0);
  });

  test("calendar event can be created and deleted", async ({ page }) => {
    await login(page);
    const start = new Date(Date.now() + 3_600_000).toISOString();
    const end = new Date(Date.now() + 7_200_000).toISOString();
    const created = await page.request.post("/api/calendar", {
      data: { title: "E2E 日程", starts_at: start, ends_at: end, location: "测试地点" },
    });
    expect(created.ok()).toBeTruthy();
    await page.goto("/calendar");
    await expect(page.getByText("E2E 日程", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "删除日程" }).click();
    await expect(page.getByText("E2E 日程", { exact: true })).toHaveCount(0);
  });

  test("canvas can be drawn and saved", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Canvas", content: "", kind: "canvas" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.goto("/documents");
    const canvas = page.locator("canvas");
    await expect(canvas).toBeVisible();
    const box = await canvas.boundingBox();
    expect(box).not.toBeNull();
    await page.mouse.move(box!.x + 40, box!.y + 40);
    await page.mouse.down();
    await page.mouse.move(box!.x + 220, box!.y + 160, { steps: 8 });
    await page.mouse.up();
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByText("文档已保存")).toBeVisible();
    const saved = await page.request.get(`/api/documents/${id}`);
    expect((await saved.json()).document.content).toContain('"type":"path"');
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("canvas object can be selected, moved and deleted", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Canvas Obj", content: "", kind: "canvas" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.goto("/documents");
    const canvas = page.locator("canvas");
    const box = await canvas.boundingBox();
    expect(box).not.toBeNull();
    const at = (x: number, y: number) => ({ x: box!.x + (x / 1280) * box!.width, y: box!.y + (y / 800) * box!.height });
    await page.getByRole("button", { name: "矩形" }).click();
    const start = at(40, 40);
    const end = at(240, 180);
    await page.mouse.move(start.x, start.y);
    await page.mouse.down();
    await page.mouse.move(end.x, end.y, { steps: 6 });
    await page.mouse.up();
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByText("文档已保存")).toBeVisible();
    let saved = await page.request.get(`/api/documents/${id}`);
    let content = JSON.parse((await saved.json()).document.content);
    expect(content.objects[0].type).toBe("rect");
    await page.getByRole("button", { name: "选择" }).click();
    const from = at(140, 70);
    const to = at(220, 120);
    await page.mouse.move(from.x, from.y);
    await page.mouse.down();
    await page.mouse.move(to.x, to.y, { steps: 6 });
    await page.mouse.up();
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await page.getByRole("button", { name: "删除选中" }).click();
    await page.waitForTimeout(300);
    const savedResponse = page.waitForResponse(
      (response) => response.url().includes(`/api/documents/${id}/collab`) && response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await savedResponse;
    saved = await page.request.get(`/api/documents/${id}`);
    content = JSON.parse((await saved.json()).document.content);
    expect(content.objects).toHaveLength(0);
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("canvas text object and PNG export work", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Canvas Text", content: "", kind: "canvas" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.goto("/documents");
    page.on("dialog", (dialog) => dialog.accept("Hello Canvas"));
    await page.getByRole("button", { name: "文本" }).click();
    const canvas = page.locator("canvas");
    const box = await canvas.boundingBox();
    expect(box).not.toBeNull();
    await page.mouse.click(box!.x + box!.width * 0.2, box!.y + box!.height * 0.3);
    const savedResponse = page.waitForResponse(
      (response) => response.url().includes(`/api/documents/${id}/collab`) && response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await savedResponse;
    const saved = await page.request.get(`/api/documents/${id}`);
    const content = JSON.parse((await saved.json()).document.content);
    expect(content.objects[0].type).toBe("text");
    expect(content.objects[0].text).toBe("Hello Canvas");
    const download = page.waitForEvent("download");
    await page.getByRole("button", { name: "导出 PNG" }).click();
    expect((await download).suggestedFilename()).toBe("canvas.png");
    const svgDownload = page.waitForEvent("download");
    await page.getByRole("button", { name: "导出 SVG" }).click();
    expect((await svgDownload).suggestedFilename()).toBe("canvas.svg");
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("canvas object layer can move down", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Canvas Layer", content: "", kind: "canvas" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.goto("/documents");
    const canvas = page.locator("canvas");
    const box = await canvas.boundingBox();
    expect(box).not.toBeNull();
    const at = (x: number, y: number) => ({ x: box!.x + (x / 1280) * box!.width, y: box!.y + (y / 800) * box!.height });
    for (const [startX, startY] of [[40, 40], [100, 100]]) {
      await page.getByRole("button", { name: "矩形" }).click();
      const start = at(startX, startY);
      const end = at(startX + 200, startY + 140);
      await page.mouse.move(start.x, start.y);
      await page.mouse.down();
      await page.mouse.move(end.x, end.y, { steps: 5 });
      await page.mouse.up();
    }
    await page.getByRole("button", { name: "选择" }).click();
    const inside = at(150, 150);
    await page.mouse.click(inside.x, inside.y);
    await page.getByRole("button", { name: "下移一层" }).click();
    const savedResponse = page.waitForResponse(
      (response) => response.url().includes(`/api/documents/${id}/collab`) && response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await savedResponse;
    const saved = await page.request.get(`/api/documents/${id}`);
    const content = JSON.parse((await saved.json()).document.content);
    expect(content.objects[0].x).toBe(100);
    expect(content.objects[1].x).toBe(40);
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("canvas object rotation can be edited", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Canvas Rotate", content: "", kind: "canvas" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.goto("/documents");
    const canvas = page.locator("canvas");
    const box = await canvas.boundingBox();
    expect(box).not.toBeNull();
    await page.getByRole("button", { name: "矩形" }).click();
    const start = { x: box!.x + box!.width * 0.1, y: box!.y + box!.height * 0.1 };
    const end = { x: box!.x + box!.width * 0.3, y: box!.y + box!.height * 0.3 };
    await page.mouse.move(start.x, start.y);
    await page.mouse.down();
    await page.mouse.move(end.x, end.y, { steps: 5 });
    await page.mouse.up();
    await page.getByRole("button", { name: "选择" }).click();
    await page.mouse.click(box!.x + box!.width * 0.2, box!.y + box!.height * 0.2);
    await page.getByLabel("旋转").fill("30");
    const savedResponse = page.waitForResponse(
      (response) => response.url().includes(`/api/documents/${id}/collab`) && response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await savedResponse;
    const saved = await page.request.get(`/api/documents/${id}`);
    const content = JSON.parse((await saved.json()).document.content);
    expect(content.objects[0].rotation).toBe(30);
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("canvas object can be resized by handle", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Canvas Resize", content: "", kind: "canvas" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.goto("/documents");
    const canvas = page.locator("canvas");
    const box = await canvas.boundingBox();
    expect(box).not.toBeNull();
    const at = (x: number, y: number) => ({ x: box!.x + (x / 1280) * box!.width, y: box!.y + (y / 800) * box!.height });
    await page.getByRole("button", { name: "矩形" }).click();
    const start = at(100, 100);
    const end = at(300, 240);
    await page.mouse.move(start.x, start.y);
    await page.mouse.down();
    await page.mouse.move(end.x, end.y, { steps: 5 });
    await page.mouse.up();
    await page.getByRole("button", { name: "选择" }).click();
    await page.mouse.click(at(200, 170).x, at(200, 170).y);
    await expect(page.getByText("选中：rect")).toBeVisible();
    const selectedBox = await canvas.boundingBox();
    expect(selectedBox).not.toBeNull();
    const at2 = (x: number, y: number) => ({ x: selectedBox!.x + (x / 1280) * selectedBox!.width, y: selectedBox!.y + (y / 800) * selectedBox!.height });
    const objectX = Number(await page.getByLabel("X", { exact: true }).inputValue());
    const objectY = Number(await page.getByLabel("Y", { exact: true }).inputValue());
    const objectW = Number(await page.getByLabel("宽", { exact: true }).inputValue());
    const objectH = Number(await page.getByLabel("高", { exact: true }).inputValue());
    const handle = at2(objectX + objectW, objectY + objectH);
    const target = at2(objectX + objectW + 80, objectY + objectH + 50);
    await page.mouse.move(handle.x, handle.y);
    await page.mouse.down();
    await page.mouse.move(target.x, target.y, { steps: 6 });
    await page.mouse.up();
    const savedResponse = page.waitForResponse(
      (response) => response.url().includes(`/api/documents/${id}/collab`) && response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await savedResponse;
    const saved = await page.request.get(`/api/documents/${id}`);
    const content = JSON.parse((await saved.json()).document.content);
    expect(content.objects[0].w).toBeGreaterThan(objectW + 30);
    expect(content.objects[0].h).toBeGreaterThan(objectH + 30);
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("canvas rotation handle changes rotation", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Canvas Rotate Handle", content: "", kind: "canvas" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.goto("/documents");
    const canvas = page.locator("canvas");
    const box = await canvas.boundingBox();
    expect(box).not.toBeNull();
    const at = (x: number, y: number) => ({ x: box!.x + (x / 1280) * box!.width, y: box!.y + (y / 800) * box!.height });
    await page.getByRole("button", { name: "矩形" }).click();
    const start = at(200, 200);
    const end = at(400, 340);
    await page.mouse.move(start.x, start.y);
    await page.mouse.down();
    await page.mouse.move(end.x, end.y, { steps: 5 });
    await page.mouse.up();
    await page.getByRole("button", { name: "选择" }).click();
    await page.mouse.click(at(300, 270).x, at(300, 270).y);
    await expect(page.getByText("选中：rect")).toBeVisible();
    const selectedBox = await canvas.boundingBox();
    const at2 = (x: number, y: number) => ({ x: selectedBox!.x + (x / 1280) * selectedBox!.width, y: selectedBox!.y + (y / 800) * selectedBox!.height });
    const objectY = Number(await page.getByLabel("Y", { exact: true }).inputValue());
    const objectH = Number(await page.getByLabel("高", { exact: true }).inputValue());
    const objectX = Number(await page.getByLabel("X", { exact: true }).inputValue());
    const objectW = Number(await page.getByLabel("宽", { exact: true }).inputValue());
    const rotate = at2(objectX + objectW / 2, objectY - 30);
    const target = at2(objectX + objectW + 80, objectY + objectH / 2);
    await page.mouse.move(rotate.x, rotate.y);
    await page.mouse.down();
    await page.mouse.move(target.x, target.y, { steps: 6 });
    await page.mouse.up();
    const savedResponse = page.waitForResponse(
      (response) => response.url().includes(`/api/documents/${id}/collab`) && response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await savedResponse;
    const saved = await page.request.get(`/api/documents/${id}`);
    const content = JSON.parse((await saved.json()).document.content);
    expect(Math.abs(content.objects[0].rotation)).toBeGreaterThan(10);
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("canvas undo and redo work", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Canvas Undo", content: "", kind: "canvas" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.goto("/documents");
    const canvas = page.locator("canvas");
    const box = await canvas.boundingBox();
    expect(box).not.toBeNull();
    await page.getByRole("button", { name: "矩形" }).click();
    const start = { x: box!.x + box!.width * 0.1, y: box!.y + box!.height * 0.1 };
    const end = { x: box!.x + box!.width * 0.3, y: box!.y + box!.height * 0.3 };
    await page.mouse.move(start.x, start.y);
    await page.mouse.down();
    await page.mouse.move(end.x, end.y, { steps: 5 });
    await page.mouse.up();
    await page.getByRole("button", { name: "撤销" }).click();
    await page.getByRole("button", { name: "重做" }).click();
    const savedResponse = page.waitForResponse(
      (response) => response.url().includes(`/api/documents/${id}/collab`) && response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await savedResponse;
    const saved = await page.request.get(`/api/documents/${id}`);
    const content = JSON.parse((await saved.json()).document.content);
    expect(content.objects).toHaveLength(1);
    expect(content.objects[0].type).toBe("rect");
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("canvas layer panel can rename an object", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Canvas Layers", content: "", kind: "canvas" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.goto("/documents");
    const canvas = page.locator("canvas");
    const box = await canvas.boundingBox();
    expect(box).not.toBeNull();
    await page.getByRole("button", { name: "矩形" }).click();
    await page.mouse.move(box!.x + box!.width * 0.1, box!.y + box!.height * 0.1);
    await page.mouse.down();
    await page.mouse.move(box!.x + box!.width * 0.3, box!.y + box!.height * 0.3, { steps: 5 });
    await page.mouse.up();
    await page.getByRole("button", { name: "图层", exact: true }).click();
    page.on("dialog", (dialog) => dialog.accept("我的图层"));
    await page.getByRole("button", { name: "图层重命名" }).first().click();
    const savedResponse = page.waitForResponse(
      (response) => response.url().includes(`/api/documents/${id}/collab`) && response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await savedResponse;
    const saved = await page.request.get(`/api/documents/${id}`);
    const content = JSON.parse((await saved.json()).document.content);
    expect(content.objects[0].name).toBe("我的图层");
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("canvas object styles can be edited", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Canvas Style", content: "", kind: "canvas" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.goto("/documents");
    const canvas = page.locator("canvas");
    const box = await canvas.boundingBox();
    expect(box).not.toBeNull();
    await page.getByRole("button", { name: "矩形" }).click();
    const start = { x: box!.x + box!.width * 0.1, y: box!.y + box!.height * 0.1 };
    const end = { x: box!.x + box!.width * 0.3, y: box!.y + box!.height * 0.3 };
    await page.mouse.move(start.x, start.y);
    await page.mouse.down();
    await page.mouse.move(end.x, end.y, { steps: 4 });
    await page.mouse.up();
    await page.getByRole("button", { name: "选择" }).click();
    await page.mouse.click(box!.x + box!.width * 0.2, box!.y + box!.height * 0.2);
    await page.getByLabel("透明度").fill("0.4");
    await page.getByLabel("虚线").check();
    await page.getByLabel("填充").fill("#ff0000");
    const savedResponse = page.waitForResponse(
      (response) => response.url().includes(`/api/documents/${id}/collab`) && response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await savedResponse;
    const saved = await page.request.get(`/api/documents/${id}`);
    const content = JSON.parse((await saved.json()).document.content);
    expect(content.objects[0].opacity).toBe(0.4);
    expect(content.objects[0].dashed).toBe(true);
    expect(content.objects[0].fill).toBe("#ff0000");
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("canvas arrow and triangle can be drawn", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Canvas Shapes", content: "", kind: "canvas" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.goto("/documents");
    const canvas = page.locator("canvas");
    const box = await canvas.boundingBox();
    expect(box).not.toBeNull();
    const at = (x: number, y: number) => ({ x: box!.x + (x / 1280) * box!.width, y: box!.y + (y / 800) * box!.height });
    for (const tool of ["箭头", "三角形"]) {
      await page.getByRole("button", { name: tool }).click();
      const start = at(100, 100);
      const end = at(300, 240);
      await page.mouse.move(start.x, start.y);
      await page.mouse.down();
      await page.mouse.move(end.x, end.y, { steps: 4 });
      await page.mouse.up();
    }
    const savedResponse = page.waitForResponse(
      (response) => response.url().includes(`/api/documents/${id}/collab`) && response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await savedResponse;
    const saved = await page.request.get(`/api/documents/${id}`);
    const content = JSON.parse((await saved.json()).document.content);
    expect(content.objects.map((object: { type: string }) => object.type).sort()).toEqual(["arrow", "triangle"]);
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("canvas image can be uploaded", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Canvas Image", content: "", kind: "canvas" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.goto("/documents");
    const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAFgwJ/lK3u+QAAAABJRU5ErkJggg==", "base64");
    await page.locator('input[type="file"]').setInputFiles({ name: "image.png", mimeType: "image/png", buffer: png });
    await expect(page.getByText("选中：image")).toBeVisible();
    const savedResponse = page.waitForResponse(
      (response) => response.url().includes(`/api/documents/${id}/collab`) && response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await savedResponse;
    const saved = await page.request.get(`/api/documents/${id}`);
    const content = JSON.parse((await saved.json()).document.content);
    expect(content.objects[0].type).toBe("image");
    expect(content.objects[0].src).toMatch(/^data:image\/png/);
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("canvas polygon can be drawn", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Canvas Polygon", content: "", kind: "canvas" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.goto("/documents");
    const canvas = page.locator("canvas");
    const box = await canvas.boundingBox();
    expect(box).not.toBeNull();
    const at = (x: number, y: number) => ({ x: box!.x + (x / 1280) * box!.width, y: box!.y + (y / 800) * box!.height });
    await page.getByRole("button", { name: "多边形" }).click();
    for (const point of [[200, 120], [380, 200], [260, 360]]) {
      const target = at(point[0], point[1]);
      await page.mouse.click(target.x, target.y);
    }
    await page.keyboard.press("Enter");
    const savedResponse = page.waitForResponse(
      (response) => response.url().includes(`/api/documents/${id}/collab`) && response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await savedResponse;
    const saved = await page.request.get(`/api/documents/${id}`);
    const content = JSON.parse((await saved.json()).document.content);
    expect(content.objects[0].type).toBe("polygon");
    expect(content.objects[0].points).toHaveLength(3);
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("canvas polygon vertex can be dragged", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Canvas Vertex", content: "", kind: "canvas" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.goto("/documents");
    const canvas = page.locator("canvas");
    const box = await canvas.boundingBox();
    expect(box).not.toBeNull();
    const at = (x: number, y: number) => ({ x: box!.x + (x / 1280) * box!.width, y: box!.y + (y / 800) * box!.height });
    await page.getByRole("button", { name: "多边形" }).click();
    for (const point of [[200, 120], [380, 200], [260, 360]]) {
      const target = at(point[0], point[1]);
      await page.mouse.click(target.x, target.y);
    }
    await page.keyboard.press("Enter");
    await page.getByRole("button", { name: "选择" }).click();
    await page.mouse.click(at(280, 220).x, at(280, 220).y);
    await expect(page.getByText("选中：polygon")).toBeVisible();
    const selectedBox = await canvas.boundingBox();
    expect(selectedBox).not.toBeNull();
    const at2 = (x: number, y: number) => ({ x: selectedBox!.x + (x / 1280) * selectedBox!.width, y: selectedBox!.y + (y / 800) * selectedBox!.height });
    const from = at2(200, 120);
    const to = at2(150, 80);
    await page.mouse.move(from.x, from.y);
    await page.mouse.down();
    await page.mouse.move(to.x, to.y, { steps: 6 });
    await page.mouse.up();
    const savedResponse = page.waitForResponse(
      (response) => response.url().includes(`/api/documents/${id}/collab`) && response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await savedResponse;
    const saved = await page.request.get(`/api/documents/${id}`);
    const content = JSON.parse((await saved.json()).document.content);
    expect(content.objects[0].points[0].x).toBeLessThan(180);
    expect(content.objects[0].points[0].y).toBeLessThan(110);
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("canvas text styles can be edited", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Canvas Text Style", content: "", kind: "canvas" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.goto("/documents");
    page.on("dialog", (dialog) => dialog.accept("Hello Style"));
    await page.getByRole("button", { name: "文本" }).click();
    const canvas = page.locator("canvas");
    const box = await canvas.boundingBox();
    expect(box).not.toBeNull();
    await page.mouse.click(box!.x + box!.width * 0.2, box!.y + box!.height * 0.2);
    await expect(page.getByText("选中：text")).toBeVisible();
    await page.getByLabel("字号").fill("40");
    await page.getByLabel("粗体").check();
    await page.getByLabel("斜体").check();
    const savedResponse = page.waitForResponse(
      (response) => response.url().includes(`/api/documents/${id}/collab`) && response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await savedResponse;
    const saved = await page.request.get(`/api/documents/${id}`);
    const content = JSON.parse((await saved.json()).document.content);
    expect(content.objects[0].fontSize).toBe(40);
    expect(content.objects[0].bold).toBe(true);
    expect(content.objects[0].italic).toBe(true);
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("document collaborator update arrives over SSE", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Collab", content: "local", kind: "document" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.goto("/documents");
    await expect(page.getByText("协作 1 人")).toBeVisible();
    const remote = await page.request.post(`/api/documents/${id}/collab`, {
      data: { title: "E2E Collab", content: "remote update", kind: "document", client_id: "other-client" },
    });
    expect(remote.ok()).toBeTruthy();
    await expect(page.getByPlaceholder("开始写文档，或粘贴 Canvas 内容…")).toHaveValue("remote update");
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("collaborator cursor appears on canvas", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Cursor", content: "", kind: "canvas" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.goto("/documents");
    await expect(page.getByText("协作 1 人")).toBeVisible();
    await page.waitForTimeout(500);
    const cursor = await page.request.post(`/api/documents/${id}/cursor`, {
      data: { client_id: "other-client", x: 100, y: 100 },
    });
    expect(cursor.ok()).toBeTruthy();
    await expect(page.getByText(/other-client/)).toBeVisible();
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("document save reports conflict and merges remote changes", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Conflict", content: "line1\nline2\nline3", kind: "document" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.route("**/api/documents/*/events*", (route) => route.abort());
    await page.goto("/documents");
    const editor = page.getByPlaceholder("开始写文档，或粘贴 Canvas 内容…");
    await expect(editor).toHaveValue("line1\nline2\nline3");
    const external = await page.request.post(`/api/documents/${id}/collab`, {
      data: { title: "E2E Conflict", content: "line1\nline2\nremote3", kind: "document", client_id: "external" },
    });
    expect(external.ok()).toBeTruthy();
    await editor.fill("local1\nline2\nline3");
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByText("已自动合并远端修改，请保存")).toBeVisible();
    await expect(editor).toHaveValue("local1\nline2\nremote3");
    const mergedSave = page.waitForResponse(
      (response) => response.url().includes(`/api/documents/${id}/collab`) && response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await mergedSave;
    const saved = await page.request.get(`/api/documents/${id}`);
    expect((await saved.json()).document.content).toBe("local1\nline2\nremote3");
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("document conflict keeps markers for overlapping edits", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Overlap", content: "alpha\nbeta", kind: "document" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.route("**/api/documents/*/events*", (route) => route.abort());
    await page.goto("/documents");
    const editor = page.getByPlaceholder("开始写文档，或粘贴 Canvas 内容…");
    await expect(editor).toHaveValue("alpha\nbeta");
    const external = await page.request.post(`/api/documents/${id}/collab`, {
      data: { title: "E2E Overlap", content: "remote-alpha\nbeta", kind: "document", client_id: "external" },
    });
    expect(external.ok()).toBeTruthy();
    await editor.fill("local-alpha\nbeta");
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByText(/发现 1 处冲突标记/)).toBeVisible();
    const value = await editor.inputValue();
    expect(value).toContain("<<<<<<< LOCAL");
    expect(value).toContain(">>>>>>> REMOTE");
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("canvas conflict merges objects by id", async ({ page }) => {
    await login(page);
    const rect = (id: string, x: number) => ({ id, type: "rect", x, y: 40, w: 100, h: 80, color: "#111827", size: 2 });
    const base = JSON.stringify({ version: 1, objects: [rect("base-a", 40), rect("base-b", 400)] });
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Canvas Merge", content: base, kind: "canvas" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.route("**/api/documents/*/events*", (route) => route.abort());
    await page.goto("/documents");
    const canvas = page.locator("canvas");
    const box = await canvas.boundingBox();
    expect(box).not.toBeNull();
    await page.getByRole("button", { name: "矩形" }).click();
    const start = { x: box!.x + box!.width * 0.5, y: box!.y + box!.height * 0.5 };
    const end = { x: box!.x + box!.width * 0.62, y: box!.y + box!.height * 0.62 };
    await page.mouse.move(start.x, start.y);
    await page.mouse.down();
    await page.mouse.move(end.x, end.y, { steps: 5 });
    await page.mouse.up();
    const remote = JSON.stringify({ version: 1, objects: [rect("base-a", 80), rect("base-b", 400), rect("remote-c", 700)] });
    const external = await page.request.post(`/api/documents/${id}/collab`, {
      data: { title: "E2E Canvas Merge", content: remote, kind: "canvas", client_id: "external" },
    });
    expect(external.ok()).toBeTruthy();
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByText("已自动合并远端修改，请保存")).toBeVisible();
    const mergedSave = page.waitForResponse(
      (response) => response.url().includes(`/api/documents/${id}/collab`) && response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await mergedSave;
    const saved = await page.request.get(`/api/documents/${id}`);
    const content = JSON.parse((await saved.json()).document.content);
    expect(content.objects).toHaveLength(4);
    expect(content.objects.find((object: { id: string }) => object.id === "base-a").x).toBe(80);
    expect(content.objects.some((object: { id: string }) => object.id === "remote-c")).toBe(true);
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("document access dialog lists account members", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E Doc ACL", content: "x", kind: "document" },
    });
    expect(created.ok()).toBeTruthy();
    const id = (await created.json()).document.id as string;
    await page.goto("/documents");
    await page.getByRole("button", { name: "访问", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "文档访问权限" });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText("Admin", { exact: true })).toBeVisible();
    await expect(dialog.getByRole("combobox")).toHaveValue("editor");
    await dialog.getByRole("button", { name: "关闭", exact: true }).last().click();
    await page.getByRole("button", { name: "删除文档" }).click();
    await page.request.delete(`/api/documents/${id}`, { data: {} });
  });

  test("meeting transcript can be saved and opened", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/meetings", {
      data: { title: "E2E 会议", transcript: "张三：周五上线。李四：负责部署。" },
    });
    expect(created.ok()).toBeTruthy();
    await page.goto("/meetings");
    await page.getByText("E2E 会议", { exact: true }).click();
    await expect(page.getByText("尚未生成摘要")).toBeVisible();
    await page.getByRole("button", { name: "删除会议" }).click();
    await expect(page.getByText("E2E 会议", { exact: true })).toHaveCount(0);
  });

  test("meeting audio upload fills transcript via STT API", async ({ page }) => {
    await login(page);
    await page.route("**/api/voice/transcribe", (route) =>
      route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ text: "mock audio transcript" }) }),
    );
    await page.goto("/meetings");
    await page.locator('input[type="file"]').setInputFiles({
      name: "meeting.webm",
      mimeType: "audio/webm",
      buffer: Buffer.from("fake-audio"),
    });
    await expect(page.getByPlaceholder("粘贴会议 transcript…")).toHaveValue(/mock audio transcript/);
  });

  test("document history can be restored", async ({ page }) => {
    await login(page);
    const created = await page.request.post("/api/documents", {
      data: { title: "E2E 版本", content: "version one", kind: "document" },
    });
    expect(created.ok()).toBeTruthy();
    await page.goto("/documents");
    const editor = page.getByPlaceholder("开始写文档，或粘贴 Canvas 内容…");
    await editor.fill("version two");
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByText("文档已保存")).toBeVisible();
    await page.getByRole("button", { name: "历史", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "历史版本" });
    await expect(dialog.getByText("E2E 版本", { exact: true })).toBeVisible();
    await dialog.getByRole("button", { name: "恢复", exact: true }).first().click();
    await expect(editor).toHaveValue("version one");
    await page.getByRole("button", { name: "删除文档" }).click();
  });

  test("all management pages render without console errors", async ({ page }) => {
    const errors: string[] = [];
    page.on("console", (message) => {
      if (message.type() !== "error") return;
      const text = message.text();
      if (/package\.json|favicon|Download the React DevTools/i.test(text)) return;
      errors.push(text);
    });
    fs.mkdirSync(shotsDir, { recursive: true });
    await login(page);
    errors.length = 0;

    const pages = [
      ["activity", "动态"],
      ["bots", "Bot"],
      ["teams", "群聊"],
      ["routines", "例程"],
      ["extensions", "连接应用"],
      ["tasks", "任务"],
      ["documents", "文档与 Canvas"],
      ["calendar", "日历"],
      ["email", "邮件"],
      ["store", "Bot Store"],
      ["audit", "审计日志"],
      ["meetings", "会议记录"],
      ["settings", "设置"],
    ] as const;

    for (const [route, heading] of pages) {
      await page.goto(`/${route}`);
      await expect(page.getByRole("heading", { level: 1, name: heading })).toBeVisible();
      await page.screenshot({ path: path.join(shotsDir, `${route}.png`), fullPage: false });
    }

    expect(errors, errors.join("\n")).toHaveLength(0);
  });

  test("help dialog follows the selected language and shows the build version", async ({ page }) => {
    await login(page);
    // The provider persists every language change to the account. Keep that
    // side effect inside the test so later tests are not switched to English;
    // the persistence path itself is covered by the language settings tests.
    await page.route("**/api/settings/language", async (route) => {
      if (route.request().method() === "PUT") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ language: "en" }),
        });
        return;
      }
      await route.continue();
    });
    await page.evaluate(() => localStorage.setItem("superbot-language", "en"));
    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.locator("button[aria-label='Account menu']")).toBeVisible();

    await expect(async () => {
      await page.locator("button[aria-label='Account menu']").click();
      await expect(page.getByRole("menuitem", { name: "Support", exact: true })).toBeVisible({ timeout: 1500 });
    }).toPass({ timeout: 20000 });
    await page.getByRole("menuitem", { name: "Support", exact: true }).click({ force: true });
    const shortcutItem = page.getByRole("menuitem", { name: "Keyboard shortcuts & install", exact: true });
    await shortcutItem.waitFor({ state: "visible", timeout: 5000 });
    await shortcutItem.dispatchEvent("click");

    await expect(page.getByText("Help & support", { exact: true })).toBeVisible();
    await expect(page.getByText("Focus the composer", { exact: true })).toBeVisible();
    await expect(page.getByText("v0.1.2 · self-hosted", { exact: true })).toBeVisible();
    await expect(page.getByText("安装为应用")).toHaveCount(0);
    await page.screenshot({ path: path.join(shotsDir, "help-en.png"), fullPage: false });
  });

  test.afterAll(async ({ playwright }) => {
    // Safety net: restore the account language even if the test above failed
    // before its own cleanup ran.
    const auth = credentials();
    if (!auth) return;
    const api = await playwright.request.newContext({
      baseURL: process.env.SUPERBOT_E2E_BASE_URL || "http://202.189.8.109:32608",
    });
    try {
      await api.post("/api/auth/login", { data: auth });
      await api.put("/api/settings/language", { data: { language: "zh" } });
    } finally {
      await api.dispose();
    }
  });
});
