import { test, expect, type Page } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";

const envPath = process.env.SUPERBOT_E2E_ENV_FILE || "";
const shotsDir = path.join("test-results", "parity");

function readCredentials(): { email: string; password: string } | null {
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

test.describe("superbot parity smoke", () => {
  test.skip(!readCredentials(), "SUPERBOT_E2E_ENV_FILE with admin credentials is required");

  test("login, shell, settings, tasks and account switcher", async ({ page }) => {
    const credentials = readCredentials()!;
    const consoleErrors: string[] = [];
    page.on("console", (message) => {
      if (message.type() !== "error") return;
      const text = message.text();
      if (/package\.json|favicon|Download the React DevTools/i.test(text)) return;
      consoleErrors.push(text);
    });
    fs.mkdirSync(shotsDir, { recursive: true });

    await page.goto("/login");
    await page.locator('input[type="email"]').fill(credentials.email);
    await page.locator('input[type="password"]').fill(credentials.password);
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await expect(page).not.toHaveURL(/\/login/);
    await expect(page.locator("button[aria-label='账户菜单']")).toBeVisible();
    consoleErrors.length = 0;
    await page.screenshot({ path: path.join(shotsDir, "main.png"), fullPage: false });

    await page.goto("/settings");
    await expect(page.getByText("通用", { exact: true }).first()).toBeVisible();
    await page.screenshot({ path: path.join(shotsDir, "settings.png"), fullPage: false });
    await page.goto("/tasks");
    await expect(page.getByRole("heading", { name: "任务" })).toBeVisible();
    await page.screenshot({ path: path.join(shotsDir, "tasks.png"), fullPage: false });

    await page.goto("/");
    await expect(page.locator("#superbot-composer")).toBeVisible();
    await page.locator("button[aria-label='账户菜单']").click();
    const switchAccount = page.getByRole("menuitem", { name: "切换账户" });
    await expect(switchAccount).toBeVisible();
    await switchAccount.click();
    await expect(page.getByRole("dialog").getByText("切换账户", { exact: true })).toBeVisible();
    await page.screenshot({ path: path.join(shotsDir, "accounts.png"), fullPage: false });

    expect(consoleErrors, consoleErrors.join("\n")).toHaveLength(0);
  });
});
