import assert from "node:assert/strict";
import test from "node:test";
import { hermesDesktopRenewalDelay, isHermesDesktopFrameMessage, resolveHermesDesktopRendererUrl, resolveHermesDesktopView } from "./hermesDesktopFrame.ts";

const origin = "https://manager.example";
const viewInput = { instanceId: 42, instanceAvailable: true, pending: false, failed: false };
const supported = { instance_id: 42, available: true };

test("Desktop is the default only after capability confirmation; initial pending mounts neither frame", () => {
  assert.deepEqual(resolveHermesDesktopView({ ...viewInput, pending: true }), { mode: "pending", reason: "checking" });
  assert.deepEqual(resolveHermesDesktopView({ ...viewInput, capability: supported }), { mode: "desktop" });
  assert.deepEqual(resolveHermesDesktopView({ ...viewInput, instanceAvailable: false, pending: true }), { mode: "unavailable" });
});

test("capability results are scoped to the selected instance", () => {
  const next = { ...viewInput, instanceId: 43 };
  assert.equal(resolveHermesDesktopView({ ...next, pending: true }).mode, "pending");
  assert.deepEqual(resolveHermesDesktopView({ ...next, capability: supported }), { mode: "unavailable", reason: "capabilityUnknown" });
  assert.deepEqual(resolveHermesDesktopView({ ...next, capability: { instance_id: 43, available: true } }), { mode: "desktop" });
});

test("gate and capability failures identify Desktop as unavailable with a fixed safe reason, never raw server text", () => {
  const reasons = {
    feature_disabled: "featureDisabled", runtime_capability_unsupported: "runtimeUnsupported", runtime_unsupported: "runtimeUnsupported",
    runtime_auth_unavailable: "securityUnavailable", runtime_origin_unavailable: "securityUnavailable", ticket_store_unavailable: "securityUnavailable",
    unsupported_instance: "unsupportedInstance", team_not_supported: "unsupportedInstance",
    runtime_unavailable: "runtimeNotReady", instance_not_running: "runtimeNotReady", runtime_not_ready: "runtimeNotReady",
    "unexpected raw token=do-not-display": "capabilityUnknown", toString: "capabilityUnknown", ["__proto__"]: "capabilityUnknown",
  };
  for (const [reason, expected] of Object.entries(reasons)) {
    assert.deepEqual(resolveHermesDesktopView({ ...viewInput, capability: { instance_id: 42, available: false, reason } }), { mode: "unavailable", reason: expected });
  }
  assert.deepEqual(resolveHermesDesktopView(viewInput), { mode: "unavailable", reason: "capabilityUnknown" });
  assert.deepEqual(resolveHermesDesktopView({ ...viewInput, capability: { instance_id: 42, available: false, reason: { toString: "not-callable" } } }), { mode: "unavailable", reason: "capabilityUnknown" });
  assert.deepEqual(resolveHermesDesktopView({ ...viewInput, failed: true, capability: supported }), { mode: "unavailable", reason: "probeFailed" });
});

test("unavailable Desktop is not a permanent user choice; a later confirmed capability opens Desktop", () => {
  assert.equal(resolveHermesDesktopView({ ...viewInput, capability: { ...supported, available: false, reason: "feature_disabled" } }).mode, "unavailable");
  assert.equal(resolveHermesDesktopView({ ...viewInput, capability: supported }).mode, "desktop");
});

test("renderer is same-origin, instance-scoped and contains no bearer material", () => {
  assert.equal(resolveHermesDesktopRendererUrl("/hermes-desktop-web/?instance_id=42", 42, origin), "/hermes-desktop-web/?instance_id=42");
  assert.equal(resolveHermesDesktopRendererUrl(`${origin}/hermes-desktop-web/index.html?instance_id=42`, 42, origin), "/hermes-desktop-web/index.html?instance_id=42");
  for (const url of [
    "https://evil.example/hermes-desktop-web/?instance_id=42",
    "//evil.example/hermes-desktop-web/?instance_id=42",
    "http://manager.example/hermes-desktop-web/?instance_id=42",
    "javascript:alert(1)",
    "https://username:password@manager.example/hermes-desktop-web/?instance_id=42",
    "/other-ui/?instance_id=42",
    "/hermes-desktop-web/?instance_id=43",
    "/hermes-desktop-web/?instance_id=42&token=secret",
    "/hermes-desktop-web/?instance_id=42&instance_id=42",
    "/hermes-desktop-web/?instance_id=42#token=secret",
    "/hermes-desktop-web/",
  ]) assert.equal(resolveHermesDesktopRendererUrl(url, 42, origin), null, url);
  assert.equal(resolveHermesDesktopRendererUrl(undefined, 42, origin), null);
});

test("iframe readiness only accepts messages from the current instance frame and origin", () => {
  const frame = {};
  const event = { source: frame, origin, data: { type: "clawmanager:hermes-desktop:ready", instanceId: 42 } };
  assert.equal(isHermesDesktopFrameMessage(event, frame, 42, origin), true);
  assert.equal(isHermesDesktopFrameMessage({ ...event, data: { type: "clawmanager:hermes-desktop:error", instanceId: 42 } }, frame, 42, origin), true);
  for (const modified of [
    { ...event, source: {} },
    { ...event, origin: "https://evil.example" },
    { ...event, data: { ...event.data, instanceId: 43 } },
    { ...event, data: { ...event.data, instanceId: "42" } },
    { ...event, data: { ...event.data, type: "unrelated" } },
    { ...event, data: null },
    { ...event, data: "ready" },
  ]) assert.equal(isHermesDesktopFrameMessage(modified, frame, 42, origin), false);
  assert.equal(isHermesDesktopFrameMessage(event, null, 42, origin), false);
});

test("cookie renewal is scheduled before expiry and invalid expiries fail closed", () => {
  const now = Date.parse("2026-09-07T00:00:00Z");
  assert.equal(hermesDesktopRenewalDelay("2026-09-07T00:10:00Z", now), 480_000);
  assert.equal(hermesDesktopRenewalDelay("2026-09-07T00:03:00Z", now), 120_000);
  assert.equal(hermesDesktopRenewalDelay("2026-09-07T00:00:30Z", now), 1000);
  for (const expiry of [undefined, "invalid", "2026-09-06T23:59:59Z", "2026-09-07T00:00:00Z"]) assert.equal(hermesDesktopRenewalDelay(expiry, now), null);
});
