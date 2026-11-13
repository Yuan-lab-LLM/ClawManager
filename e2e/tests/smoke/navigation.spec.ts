import { test, expect } from "../../fixtures/test.js";
import { users } from "../../fixtures/users.js";

test.describe("authenticated admin navigation", () => {
  test.use({ storageState: users.admin.storageState });
  test("@p0 shell identifies the current page and persists edge collapse", async ({ page }) => {
    await page.goto("/instances");
    await expect(page.getByRole("navigation", { name: "Current page" })).toHaveText(/My Instances/i);
    await expect(page.getByRole("navigation", { name: "Main navigation" }).getByRole("link", { name: /My Instances/i })).toHaveAttribute("aria-current", "page");
    const collapsed = await page.evaluate(() => localStorage.getItem("sidebar_collapsed") === "true");
    await page.getByRole("button", { name: collapsed ? "Expand sidebar" : "Collapse sidebar", exact: true }).click();
    await page.reload();
    await expect(page.getByRole("button", { name: collapsed ? "Collapse sidebar" : "Expand sidebar", exact: true })).toBeVisible();
    expect(await page.evaluate(() => localStorage.getItem("sidebar_collapsed"))).toBe(String(!collapsed));
  });
  test("@p0 authenticated admin reaches workspace and admin routes", async ({ page }) => {
    for (const [route, title] of [["/dashboard", /workspace/i], ["/instances", /my instances/i], ["/settings", /settings/i], ["/admin", /console/i], ["/admin/runtime-pods", /^runtime$/i], ["/admin/settings", /system settings/i]] as const) {
      await page.goto(route);
      await expect(page.getByRole("navigation", { name: "Current page" })).toHaveText(title);
      if (route === "/settings") await expect(page.getByRole("heading", { name: /change password/i })).toBeVisible();
      if (route === "/admin") {
        await expect(page.getByRole("navigation", { name: "Main navigation" }).getByRole("link", { name: /^runtime$/i })).toBeVisible();
        await expect(page.getByRole("navigation", { name: "Main navigation" }).getByRole("link", { name: /^settings$/i })).toBeVisible();
      }
    }
  });
  test("@p0 short desktop sidebar keeps lower routes and protection categories reachable", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 300 });
    await page.goto('/admin');
    const nav = page.getByRole('navigation', { name: 'Main navigation' });
    const settings = nav.getByRole('link', { name: /^settings$/i });
    await settings.scrollIntoViewIfNeeded();
    await expect(settings).toBeInViewport();
    expect(await nav.evaluate(el => el.scrollTop)).toBeGreaterThan(0);
    await settings.click();
    await expect(page).toHaveURL(/\/admin\/settings$/);
    const protection = page.locator('.cm-management-sidebar a[href="/admin/secplane"]');
    await protection.focus();
    const category = page.locator('.cm-management-sidebar .cm-management-popup a').first();
    await expect(category).toBeInViewport();
    await category.click();
    await expect(page).not.toHaveURL(/\/admin\/settings$/);
  });
  test("@p0 protection popup stays within a 400px viewport and reaches its last category", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 400 });
    await page.goto('/admin');
    await page.locator('.cm-management-sidebar a[href="/admin/secplane"]').focus();
    const popup = page.locator('.cm-sidebar-protection-popup');
    await expect(popup).toBeVisible();
    const bounds = await popup.boundingBox();
    expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(388);
    const last = popup.getByRole('link').last();
    const destination = await last.getAttribute('href');
    await last.scrollIntoViewIfNeeded();
    await expect(last).toBeInViewport({ ratio: 1 });
    await last.click();
    await expect(page).toHaveURL(new RegExp(`${destination}$`));
    await page.goto('/admin');
    await page.locator('.cm-management-sidebar a[href="/admin/secplane"]').focus();
    await expect(popup).toBeVisible();
    await page.setViewportSize({ width: 1280, height: 300 });
    await expect(popup).not.toBeVisible();
    const trigger = page.locator('.cm-management-sidebar a[href="/admin/secplane"]');
    await trigger.evaluate(el => el.blur());
    await trigger.focus();
    await expect(popup).toBeVisible();
    await page.getByRole('navigation', { name: 'Main navigation' }).evaluate(el => { el.scrollTop = el.scrollTop ? 0 : 20; });
    await expect(popup).not.toBeVisible();
  });
  test("@p0 drawer brand closes and restores navigation focus on same and different routes", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto('/dashboard');
    const trigger = page.getByRole('button', { name: 'Open navigation' });
    for (const route of ['/dashboard', '/instances']) {
      await page.goto(route);
      await trigger.click();
      await page.getByRole('dialog').getByRole('link', { name: /ClawManager/ }).click();
      await expect(page).toHaveURL(/\/dashboard$/);
      await expect(page.getByRole('dialog')).not.toBeVisible();
      await expect(trigger).toBeFocused();
    }
  });
  test("@p0 mobile drawer supports keyboard close and focus return", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/instances");
    const trigger = page.getByRole("button", { name: "Open navigation" });
    await trigger.click();
    await expect(page.getByRole("dialog", { name: "Main navigation" })).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(trigger).toBeFocused();
    await trigger.click();
    await page.getByRole("dialog").getByRole("link", { name: /^settings$/i }).click();
    await expect(page).toHaveURL(/\/settings$/);
    await expect(trigger).toBeFocused();
  });
});
test.describe("unauthenticated navigation", () => {
  test.use({ storageState: undefined });
  test("@p0 unauthenticated users are redirected to login", async ({ page }) => {
    await page.goto("/instances");
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.getByRole("heading", { name: /sign in to clawmanager/i })).toBeVisible();
  });
});
