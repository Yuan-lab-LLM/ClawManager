import { expect, test } from "../../fixtures/test.js";
import type { Page } from "@playwright/test";
import type { InstanceRecord } from "../../fixtures/apiClient.js";
import { users } from "../../fixtures/users.js";

test.use({ storageState: users.admin.storageState });

type InstanceMode = "lite" | "pro";

interface ApiEnvelope<T> {
  success: boolean;
  data: T;
  error?: string;
}

interface InstanceListResponse {
  instances: InstanceRecord[];
  total: number;
  page: number;
  limit: number;
}

interface PasswordExternalAccessResult {
  access: {
    id: number;
    instance_id: number;
    enabled: boolean;
    auth_mode: string;
  };
  password: string;
  share_url?: string;
}

function modeOf(instance: InstanceRecord): InstanceMode {
  if (instance.instance_mode === "lite" || instance.instance_mode === "pro") {
    return instance.instance_mode;
  }
  return instance.runtime_type === "gateway" ? "lite" : "pro";
}

function activeInstances(instances: InstanceRecord[]) {
  return instances.filter((instance) => instance.status !== "deleting");
}

function escapeRegExp(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function firstMode(instances: InstanceRecord[], mode: InstanceMode) {
  return activeInstances(instances).find((instance) => modeOf(instance) === mode);
}

function runningOpenClawLiteInstances(instances: InstanceRecord[]) {
  return activeInstances(instances).filter(
    (instance) =>
      instance.type === "openclaw" &&
      modeOf(instance) === "lite" &&
      instance.status === "running"
  );
}

async function apiFromPage<T>(
  page: Page,
  path: string,
  options: { method?: string; data?: unknown } = {}
): Promise<T> {
  const result = await page.evaluate(
    async ({ requestPath, method, data }) => {
      const accessToken = window.localStorage.getItem("access_token");
      const headers: Record<string, string> = {};
      if (accessToken) {
        headers.Authorization = `Bearer ${accessToken}`;
      }
      if (data !== undefined) {
        headers["Content-Type"] = "application/json";
      }

      const response = await fetch(requestPath, {
        method,
        headers,
        body: data === undefined ? undefined : JSON.stringify(data)
      });
      const text = await response.text();
      let payload: ApiEnvelope<unknown> | null = null;
      if (text) {
        payload = JSON.parse(text) as ApiEnvelope<unknown>;
      }
      return { ok: response.ok, status: response.status, payload, text };
    },
    {
      requestPath: path,
      method: options.method ?? "GET",
      data: options.data
    }
  );

  if (!result.ok || !result.payload?.success) {
    throw new Error(result.payload?.error || result.text || `request failed with status ${result.status}`);
  }
  return result.payload.data as T;
}

async function adminInstances(page: Page) {
  await page.goto("/dashboard");
  const response = await apiFromPage<InstanceListResponse>(page, "/api/v1/admin/instances?page=1&limit=1000");
  return response.instances;
}

async function userInstances(page: Page) {
  await page.goto("/dashboard");
  const response = await apiFromPage<InstanceListResponse>(page, "/api/v1/instances?page=1&limit=1000");
  return response.instances;
}

async function primeOpenClawGatewayStorage(page: Page, instanceId: number) {
  await page.goto("/dashboard");
  await page.evaluate((staleInstanceId) => {
    for (const key of Object.keys(window.localStorage)) {
      if (/openclaw|clawmanager\.openclaw/i.test(key)) {
        window.localStorage.removeItem(key);
      }
    }

    const gatewayURL = new URL(`/api/v1/instances/${staleInstanceId}/proxy`, window.location.origin);
    gatewayURL.protocol = gatewayURL.protocol === "https:" ? "wss:" : "ws:";
    gatewayURL.search = "";
    gatewayURL.hash = "";
    gatewayURL.pathname = gatewayURL.pathname.replace(/\/+$/, "");

    const settings = {
      gatewayUrl: gatewayURL.toString(),
      sessionKey: "main",
      lastActiveSessionKey: "main",
      theme: "claw",
      themeMode: "system",
      chatFocusMode: false,
      chatShowThinking: true,
      chatShowToolCalls: true,
      splitRatio: 0.6,
      navCollapsed: false,
      navWidth: 220,
      navGroupsCollapsed: {},
      borderRadius: 50,
      sessionsByGateway: {
        [gatewayURL.toString()]: {
          sessionKey: "main",
          lastActiveSessionKey: "main"
        }
      }
    };
    const serialized = JSON.stringify(settings);
    window.localStorage.setItem("openclaw.control.settings.v1", serialized);
    window.localStorage.setItem(`openclaw.control.settings.v1:${gatewayURL.toString()}`, serialized);
    window.localStorage.setItem("clawmanager.openclaw.instanceId", String(staleInstanceId));
    window.localStorage.setItem("clawmanager.openclaw.gatewayUrl", gatewayURL.toString());
  }, instanceId);
}

async function expectedGatewayURL(page: Page, instanceId: number) {
  return page.evaluate((targetInstanceId) => {
    const gatewayURL = new URL(`/api/v1/instances/${targetInstanceId}/proxy`, window.location.origin);
    gatewayURL.protocol = gatewayURL.protocol === "https:" ? "wss:" : "ws:";
    gatewayURL.search = "";
    gatewayURL.hash = "";
    gatewayURL.pathname = gatewayURL.pathname.replace(/\/+$/, "");
    return gatewayURL.toString();
  }, instanceId);
}

async function reachConfigurationStep(page: Page, mode: InstanceMode) {
  await page.goto("/instances/new");
  await expect(page.getByRole("heading", { name: /create instance/i })).toBeVisible();

  await page.locator("main input#name").nth(1).fill(`e2e-${mode}-mode-check`);
  await page.getByRole("button", { name: new RegExp(`^${mode === "lite" ? "Lite" : "Pro"}`, "i") }).click();
  const basicNextButton = page.getByRole("button", { name: /^Next$/i });
  await expect(basicNextButton).toBeEnabled();
  await basicNextButton.click();

  await expect(page.getByRole("heading", { name: /select instance type/i })).toBeVisible();
  const nextButton = page.getByRole("button", { name: /^Next$/i });
  await expect(nextButton).toBeEnabled();
  await nextButton.click();

  await expect(page.getByRole("heading", { name: /summary/i })).toBeVisible();
}

async function visiblePageText(page: Page) {
  return page.evaluate(() => document.body.innerText);
}

test.describe("lite/pro instance modes", () => {
  test("@p0 create wizard exposes the lite/pro mode contract", async ({ page }) => {
    await test.step("lite derives gateway runtime and skips dedicated resources", async () => {
      await reachConfigurationStep(page, "lite");
      const summary = page.locator(".app-panel").filter({
        has: page.getByRole("heading", { name: /summary/i })
      });

      await expect(summary.getByText(/^Lite$/)).toBeVisible();
      await expect(summary.getByText(/^Gateway$/)).toBeVisible();
      await expect(page.getByRole("heading", { name: /quick configuration/i })).toHaveCount(0);
      await expect(page.getByRole("button", { name: /enable gpu/i })).toHaveCount(0);
      await expect(summary.getByText(/^CPU$/)).toHaveCount(0);
      await expect(summary.getByText(/^Memory$/)).toHaveCount(0);
      await expect(summary.getByText(/^Storage$/)).toHaveCount(0);
      await expect(summary.getByText(/^GPU$/)).toHaveCount(0);
    });

    await test.step("pro derives desktop runtime and shows dedicated resources", async () => {
      await reachConfigurationStep(page, "pro");
      const summary = page.locator(".app-panel").filter({
        has: page.getByRole("heading", { name: /summary/i })
      });

      await expect(summary.getByText(/^Pro$/)).toBeVisible();
      await expect(summary.getByText(/^50 GB$/)).toBeVisible();
      await expect(summary.getByText(/^Desktop$/)).toBeVisible();
      await expect(page.getByRole("heading", { name: /quick configuration/i })).toBeVisible();
      await expect(page.getByRole("button", { name: /enable gpu/i })).toBeVisible();
      await expect(summary.getByText(/^CPU$/)).toBeVisible();
      await expect(summary.getByText(/^Memory$/)).toBeVisible();
      await expect(summary.getByText(/^Storage$/)).toBeVisible();
      await expect(summary.getByText(/^GPU$/)).toBeVisible();
    });
  });

  test("@p0 lite detail keeps gateway workspace layout", async ({ page }) => {
    const instances = await adminInstances(page);
    const lite = firstMode(instances, "lite");
    if (!lite) {
      test.skip(true, "No lite/gateway instance exists in this target environment.");
      return;
    }

    await page.goto(`/instances/${lite.id}`);
    await expect(page.getByRole("heading", { name: lite.name }).first()).toBeVisible();
    await expect.poll(() => visiblePageText(page)).toContain("Mode Lite");
    await expect.poll(() => visiblePageText(page)).toContain("Runtime gateway");
    await page.getByRole("button", { name: /share link/i }).click();
    await expect(page.locator('[data-panel="share-link-popover"]').last()).toBeVisible();
    await expect(page.getByRole("button", { name: /create password/i })).toBeVisible();

    await expect(page.getByRole("heading", { name: /runtime overview/i })).toHaveCount(0);
    await expect(page.getByRole("heading", { name: /runtime events/i })).toHaveCount(0);
    await expect(page.getByRole("heading", { name: /instance skills/i })).toHaveCount(0);
  });

  test("@p0 pro detail keeps desktop management layout", async ({ page }) => {
    const instances = await adminInstances(page);
    const pro = firstMode(instances, "pro");
    if (!pro) {
      test.skip(true, "No pro/desktop instance exists in this target environment.");
      return;
    }

    await page.goto(`/instances/${pro.id}`);
    await expect(page.getByRole("heading", { name: pro.name }).first()).toBeVisible();
    await expect.poll(() => visiblePageText(page)).toContain("Mode Pro");
    await expect.poll(() => visiblePageText(page)).toContain("Runtime desktop");
    await page.getByRole("button", { name: /share link/i }).click();
    await expect(page.locator('[data-panel="share-link-popover"]').last()).toBeVisible();
    await expect(page.getByRole("button", { name: /create password/i })).toBeVisible();

    await expect(page.getByRole("heading", { name: /runtime overview/i })).toBeVisible();
    await expect(page.getByRole("heading", { name: /runtime events/i })).toBeVisible();
    await expect(page.getByRole("heading", { name: /instance skills/i })).toBeVisible();
    const proDetailText = await visiblePageText(page);
    expect(proDetailText.indexOf("Instance Skills")).toBeLessThan(proDetailText.indexOf("Runtime Overview"));
    expect(proDetailText).not.toContain("Resource Monitor");
    expect(proDetailText).not.toContain("Runtime Status");

    const workspace = await apiFromPage<{ entries: Array<{ path: string }> }>(
      page,
      `/api/v1/instances/${pro.id}/workspace/files?path=`
    );
    expect(workspace.entries.length).toBeGreaterThan(0);
    expect(workspace.entries.some((entry) => [".config", ".openclaw", "Desktop"].includes(entry.path))).toBeTruthy();
  });

  test("@p0 admin instances page surfaces mode and backend management controls", async ({ page }) => {
    const instances = await adminInstances(page);
    const lite = firstMode(instances, "lite");
    const pro = firstMode(instances, "pro");

    await page.goto("/admin/instances");
    await expect(page.getByRole("heading", { name: /global instances/i })).toBeVisible();
    await expect.poll(() => visiblePageText(page)).toContain("Gateway runtime");
    await expect.poll(() => visiblePageText(page)).toContain("Desktop deployment");

    const filters = page.locator("main select");
    const modeFilter = filters.nth(6);
    const runtimeFilter = filters.nth(7);
    await expect(modeFilter).toContainText("Lite");
    await expect(modeFilter).toContainText("Pro");
    await expect(runtimeFilter).toContainText("Gateway");
    await expect(runtimeFilter).toContainText("Desktop");

    if (lite) {
      await modeFilter.selectOption("lite");
      await expect.poll(() => visiblePageText(page)).toContain(lite.name);
      await expect.poll(() => visiblePageText(page)).toContain("Gateway binding");
      await expect.poll(() => visiblePageText(page)).toContain("Gateway");
    }

    if (pro) {
      await modeFilter.selectOption("pro");
      await expect.poll(() => visiblePageText(page)).toContain(pro.name);
      await expect.poll(() => visiblePageText(page)).toContain("Deployment");
      await expect.poll(() => visiblePageText(page)).toContain("Desktop");
    }
  });

  test("@p0 portal refreshes OpenClaw Lite gateway settings before embedding", async ({ page }) => {
    const instances = await userInstances(page);
    const liteInstances = runningOpenClawLiteInstances(instances);
    if (liteInstances.length < 2) {
      test.skip(true, "Need at least two running OpenClaw Lite instances for stale gateway coverage.");
      return;
    }

    const [target, stale] = liteInstances;
    await primeOpenClawGatewayStorage(page, stale.id);
    await page.goto("/portal");
    await page.locator("button").filter({ hasText: target.name }).first().dispatchEvent("click");

    const gatewayURL = await expectedGatewayURL(page, target.id);
    await expect
      .poll(() => page.evaluate(() => window.localStorage.getItem("clawmanager.openclaw.instanceId")))
      .toBe(String(target.id));
    await expect
      .poll(() => page.evaluate(() => window.localStorage.getItem("clawmanager.openclaw.gatewayUrl")))
      .toBe(gatewayURL);

    const settings = await page.evaluate(() => window.localStorage.getItem("openclaw.control.settings.v1"));
    expect(settings).toBeTruthy();
    expect(JSON.parse(settings!).gatewayUrl).toBe(gatewayURL);
  });

  test("@p1 password share link opens a password gate", async ({ page }) => {
    const instances = await adminInstances(page);
    const target = activeInstances(instances).find((instance) => instance.runtime_type !== "shell");
    if (!target) {
      test.skip(true, "No non-shell instance exists for share link coverage.");
      return;
    }

    let passwordCreated = false;
    try {
      const result = await apiFromPage<PasswordExternalAccessResult>(
        page,
        `/api/v1/instances/${target.id}/external-access/password`,
        {
          method: "POST",
          data: {
            expires_mode: "preset",
            expires_preset: "1h"
          }
        }
      );
      passwordCreated = true;

      expect(result.password).toMatch(/^pwd_/);
      expect(result.share_url).toBeTruthy();
      expect(result.share_url).toMatch(/^\/s\/[^/?#]+\/$/);
      expect(result.share_url).not.toContain("token=");
      expect(result.share_url).not.toContain("/api/v1/");
      expect(result.share_url).not.toContain("/proxy");

      await page.goto(`/instances/${target.id}`);
      await page.reload({ waitUntil: "domcontentloaded" });
      await expect(page.locator('button[data-share-auth="password"]', { hasText: /share link/i }).last()).toBeVisible();
      await page.getByRole("button", { name: /share link/i }).click();
      const sharePanel = page.locator('[data-panel="share-link-popover"]').last();
      const shareHeader = sharePanel.locator(".mb-3").first();
      await expect(shareHeader).not.toContainText("Password");
      await expect(shareHeader).not.toContainText(result.password.slice(0, 12));
      await expect(sharePanel.getByLabel(/share link url/i)).toHaveValue(
        new RegExp(`${escapeRegExp(result.share_url!)}$`)
      );
      await expect(sharePanel).not.toContainText("Password is shown only");
      await expect(sharePanel).not.toContainText("created before link recovery");
      await expect(sharePanel.getByRole("button", { name: /copy share link/i })).toBeVisible();
      await expect(sharePanel.getByRole("button", { name: /copy password/i })).toBeVisible();

      const passwordInput = sharePanel.getByLabel(/share link password/i);
      await expect(passwordInput).toHaveValue(result.password);
      await expect(passwordInput).toHaveAttribute("type", "password");
      await sharePanel.getByRole("button", { name: /show password/i }).click();
      await expect(passwordInput).toHaveAttribute("type", "text");
      await expect(sharePanel.getByRole("button", { name: /hide password/i })).toBeVisible();

      await page.goto(result.share_url!);
      await expect(page.getByRole("heading", { name: /share link password/i })).toBeVisible();
      await expect(page.locator('input[name="password"]')).toBeVisible();
      await expect(page.getByRole("button", { name: /open|continue/i })).toBeVisible();
    } finally {
      if (passwordCreated) {
        await page.goto("/dashboard");
        await apiFromPage<null>(page, `/api/v1/instances/${target.id}/external-access`, {
          method: "DELETE"
        });
      }
    }
  });
});
