import assert from "node:assert/strict";
import test from "node:test";
import { browserWorkerAccessIsFresh, browserWorkerRenewalDelay, refreshBrowserWorkerAccess } from "./browserWorkerAccess.ts";

test("renew one minute before expiry; tolerate missing, malformed and expired timestamps", () => {
  const now = Date.parse("2026-01-01T00:00:00Z");
  assert.equal(browserWorkerRenewalDelay("2026-01-01T01:00:00Z", now), 3_540_000);
  assert.equal(browserWorkerRenewalDelay("2025-12-31T23:00:00Z", now), 1_000);
  assert.equal(browserWorkerRenewalDelay(undefined, now), null);
  assert.equal(browserWorkerRenewalDelay("invalid", now), null);
});

test("returning to the browser tab cannot reuse expired cached access", () => {
  const now = Date.parse("2026-01-01T00:00:00Z");
  assert.equal(browserWorkerAccessIsFresh("2026-01-01T01:00:00Z", now), true);
  assert.equal(browserWorkerAccessIsFresh("2026-01-01T00:00:30Z", now), false);
  assert.equal(browserWorkerAccessIsFresh("2025-12-31T23:59:00Z", now), false);
  assert.equal(browserWorkerAccessIsFresh(undefined, now), false);
});

test("manual refresh renews credentials before reloading the iframe", async () => {
  const events = [];
  await refreshBrowserWorkerAccess(async () => { events.push("renew"); return true; }, () => events.push("reload"));
  assert.deepEqual(events, ["renew", "reload"]);
});

test("failed renewal does not reload a frame with expired credentials", async () => {
  let reloaded = false;
  await refreshBrowserWorkerAccess(async () => false, () => { reloaded = true; });
  assert.equal(reloaded, false);
});
