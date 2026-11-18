// Browser regression against the real admin page. All API calls use fixtures.
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir } from "node:fs/promises";
import path from "node:path";
import { createServer } from "vite";

const requireE2E = createRequire(process.env.CM_BROWSER_TOOL_PACKAGE || new URL("../../e2e/package.json", import.meta.url));
const { chromium } = requireE2E("playwright");
const server = await createServer({ root: path.resolve(import.meta.dirname, ".."), server: { host: "127.0.0.1", port: 0 } });
await server.listen();
const port = server.httpServer.address().port;
const url = `http://127.0.0.1:${port}/admin/runtime-pods`;
const browser = await chromium.launch({
  ...(process.env.CM_BROWSER_EXECUTABLE ? { executablePath: process.env.CM_BROWSER_EXECUTABLE } : { channel: "chrome" }),
  headless: true,
});
const screenshotDir = process.env.CM_SLOT_RESET_SCREENSHOTS || "/tmp/cm-slot-reset-browser";
await mkdir(screenshotDir, { recursive: true });

try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, reducedMotion: "reduce" });
  page.setDefaultTimeout(10000);
  await page.routeWebSocket("**/api/v1/ws?**", () => {});
  await page.addInitScript(() => localStorage.setItem("access_token", "fixture"));
  let role = "admin";
  let sourceUID = "pod-original";
  let mode = "allowed";
  let retrySeen = 0;
  let submitConflicts = 0;
  let submitted;
  let submissions = 0;
  let showOperation = false;
  let exactLookups = 0;
  let recoveryState = "pending";
  let resetPhase = "waiting_worker";
  let releaseUnconfirmed = false;
  const containerID = `containerd://${"old".repeat(27)}`;
  const operation = () => ({
    id: "slot-reset-original",
    kind: "slot_reset",
    status: resetPhase === "released" && !releaseUnconfirmed ? "completed" : "needs_attention",
    items: [{ state: "processing", source_pod_id: 7, instance_id: 42, slot_reset: {
      slot: "slot-0", pod_uid: submitted.source_pod_uid, container_id: containerID, instance_id: 42,
      phase: resetPhase, recovery_action: "reschedule", recovery_state: recoveryState,
      ...(releaseUnconfirmed ? { error_message: "Fence release still needs verification" } : {}),
    } }],
  });
  await page.route("**/api/v1/**", async route => {
    const request = route.request();
    const endpoint = new URL(request.url());
    const pathname = endpoint.pathname;
    let data = {};
    if (pathname.endsWith("/auth/me")) data = { id: 23, username: "operator", role };
    else if (pathname.endsWith("/admin/runtime-pods")) {
      const pod = {
        id: 7, pod_uid: sourceUID, runtime_type: "openclaw", namespace: "runtime", pod_name: "unknown-pod",
        state: "unknown", capacity: 1, used_slots: 1, agent_reported: false,
        slots: [{ index: 1, container_name: "slot-0", ready: false, state: "unknown", reconciliation_state: "unknown", restart_count: 1 }],
      };
      data = { pods: [pod], summary: { pods: 1, capacity: 1 }, pagination: { page: 1, page_size: 16, total: 1, total_pages: 1 }, nodes: [] };
    } else if (pathname.endsWith("/slots/slot-0/reset/preflight")) {
      assert.equal(request.postDataJSON().source_pod_uid, sourceUID);
      if (mode === "denied") return route.fulfill({ status: 403, json: { message: "Admin access required" } });
      data = {
        allowed: mode !== "blocked",
        blockers: mode === "blocked" ? ["Node identity unavailable"] : [],
        pod_uid: sourceUID, pod_name: "unknown-pod", node_name: "node-a",
        slot: "slot-0", runtime_type: "openclaw", container_id: containerID,
        scope_fingerprint: "original-fingerprint", instance_id: ["blocked", "unknown-owner"].includes(mode) ? 0 : 42, generation: 2,
        recovery_action: mode === "preserve" ? "preserve_current" : mode === "unknown-owner" ? "none" : "reschedule",
        ...(mode === "preserve" ? { current_pod_id: 9, current_slot: "slot-1" } : {}),
      };
    } else if (pathname.endsWith("/slots/slot-0/reset")) {
      if (mode === "conflict") {
        submitConflicts++;
        return route.fulfill({ status: 409, json: { error: "Source container changed after the preview" } });
      }
      submissions++;
      submitted = request.postDataJSON();
      return route.abort("timedout");
    } else if (pathname.endsWith("/runtime-scheduling")) {
      const requestID = endpoint.searchParams.get("slot_reset_request_id");
      if (requestID) {
        assert.equal(requestID, submitted.request_id);
        exactLookups++;
        data = showOperation ? operation() : null;
      } else data = { items: showOperation ? [operation()] : [], supported_modes: [] };
    } else if (pathname.endsWith("/runtime-scheduling/slot-reset-original/retry")) {
      retrySeen++;
      data = operation();
    }
    await route.fulfill({ json: { data } });
  });

  const openUnknown = async () => {
    await page.getByRole("button", { name: "unknown-pod", exact: true }).click();
    await page.getByRole("button", { name: /Slot 1 · Unknown/ }).click();
  };
  const reset = () => page.getByRole("button", { name: "Force reset slot", exact: true });
  const confirm = () => page.getByRole("button", { name: "Confirm slot reset", exact: true });
  const dialog = () => page.getByRole("dialog");
  await page.goto(url);
  await openUnknown();
  await reset().focus();
  await page.keyboard.press("Enter");
  await dialog().getByText(containerID, { exact: true }).waitFor();
  await dialog().getByText(/schedule instance #42/).waitFor();
  await page.keyboard.press("Escape");
  assert.equal(await reset().evaluate(element => element === document.activeElement), true);

  mode = "unknown-owner";
  await reset().click();
  await dialog().getByText("Owner unknown", { exact: true }).waitFor();
  await dialog().getByText("Clean up the old source only. No instance will be started.").waitFor();
  assert.equal(await confirm().isEnabled(), true);
  await page.keyboard.press("Escape");

  mode = "blocked";
  await reset().click();
  await dialog().getByText("Node identity unavailable").waitFor();
  assert.equal(await confirm().isDisabled(), true);
  await page.keyboard.press("Escape");

  mode = "denied";
  await reset().click();
  await dialog().getByText("Admin access required", { exact: true }).waitFor();
  assert.equal(await confirm().isDisabled(), true);
  await page.keyboard.press("Escape");

  mode = "preserve";
  await reset().click();
  await dialog().getByText(/Keep the current run/).waitFor();
  await dialog().getByText(/Pod #9 · slot-1/).waitFor();
  await page.keyboard.press("Escape");

  mode = "conflict";
  await reset().click();
  await confirm().click();
  await dialog().getByText("Source container changed after the preview", { exact: true }).waitFor();
  assert.equal(submitConflicts, 1);
  assert.equal(await confirm().isDisabled(), true);
  assert.equal(exactLookups, 0);
  mode = "allowed";
  await dialog().getByRole("button", { name: "Check again", exact: true }).click();
  await page.waitForFunction(() => !document.querySelector('[data-testid="runtime-slot-reset-confirm"]').disabled);
  await page.keyboard.press("Escape");

  mode = "allowed";
  await reset().click();
  await dialog().getByText(containerID, { exact: true }).waitFor();
  sourceUID = "pod-replaced";
  await dialog().getByText(/The selected source changed/).waitFor();
  assert.equal(await confirm().isDisabled(), true);
  assert.equal(submissions, 0);
  await page.keyboard.press("Escape");

  await reset().click();
  await dialog().getByText(containerID, { exact: true }).waitFor();
  for (const [width, locale] of [[1440, "en"], [1280, "zh"], [768, "ja"], [390, "ko"], [390, "de"], [320, "en"]]) {
    await page.setViewportSize({ width, height: width < 500 ? 720 : 900 });
    await page.evaluate(locale => {
      // The production language menu is unnecessary for this fixture. Persisted
      // locale is restored by I18nProvider when the page reloads.
      localStorage.setItem("clawmanager_locale", locale);
    }, locale);
    await page.reload();
    await page.getByRole("button", { name: "unknown-pod", exact: true }).click();
    await page.locator("button[aria-pressed=false]").filter({ hasText: "01" }).first().click();
    await page.getByTestId("runtime-slot-reset-action").click();
    await dialog().getByText(containerID, { exact: true }).waitFor();
    assert.equal(await dialog().evaluate(element => element.scrollWidth <= element.clientWidth && element.getBoundingClientRect().right <= innerWidth), true);
    const submit = dialog().getByTestId("runtime-slot-reset-confirm");
    assert.equal(await submit.isEnabled(), true);
    assert.equal(await submit.evaluate(element => element.getBoundingClientRect().bottom <= innerHeight), true);
    await page.screenshot({ path: path.join(screenshotDir, `preflight-${width}-${locale}.png`), animations: "disabled" });
  }

  await confirm().dblclick();
  await dialog().getByText(/Submission outcome is unknown/).waitFor();
  assert.equal(submissions, 1);
  assert.equal(submitted.source_pod_uid, "pod-replaced");
  assert.equal(submitted.source_container_id, containerID);
  assert.equal(submitted.scope_fingerprint, "original-fingerprint");
  assert.ok(submitted.request_id);
  await page.waitForFunction(() => document.querySelector("dialog")?.textContent?.includes("Looking for the original task"));

  await page.reload();
  await openUnknown();
  await reset().click();
  await dialog().getByText(/Submission outcome is unknown/).waitFor();
  assert.equal(submissions, 1);
  showOperation = true;
  await dialog().getByText("Waiting for a verified new worker", { exact: true }).waitFor();
  await dialog().getByText("Waiting to be scheduled", { exact: true }).waitFor();
  assert.ok(exactLookups >= 2);
  await page.screenshot({ path: path.join(screenshotDir, "waiting-320.png") });
  await dialog().getByRole("button", { name: "Recheck and continue", exact: true }).click();
  assert.equal(retrySeen, 1);
  recoveryState = "starting";
  await dialog().getByText("Starting", { exact: true }).waitFor();
  recoveryState = "running";
  await dialog().getByText("Running", { exact: true }).waitFor();
  assert.equal(await dialog().getByText("Waiting for a verified new worker", { exact: true }).count(), 1);
  releaseUnconfirmed = true;
  resetPhase = "released";
  await dialog().getByText("Fence release still needs verification", { exact: true }).waitFor();
  assert.equal(await dialog().getByText("Slot available", { exact: true }).count(), 0);
  releaseUnconfirmed = false;
  await dialog().getByText("Slot available", { exact: true }).waitFor();
  await dialog().getByRole("button", { name: "View task history", exact: true }).click();
  await dialog().getByText("Slot available", { exact: true }).waitFor();
  await dialog().getByText("Running", { exact: true }).waitFor();

  await page.evaluate(() => localStorage.clear());
  role = "user";
  await page.reload();
  await page.waitForURL(value => !value.pathname.startsWith("/admin/"));
  assert.equal(await reset().count(), 0);
  console.log("PASS unknown slot, strict preflight, scope drift, keyboard, focus, five locales and six viewport checks");
  console.log("PASS double-click, lost response, reload and exact task lookup, retry, independent slot and instance progress, admin route");
} finally {
  await browser.close();
  await server.close();
}
