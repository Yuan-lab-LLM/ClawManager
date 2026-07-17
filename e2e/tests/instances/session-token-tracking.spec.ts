import { expect, test } from "../../fixtures/test.js";
import { env } from "../../fixtures/env.js";
import {
  gatewayChatCompletion,
  getInstanceSessionUsage,
  listGatewayModels,
  listInstances,
  login,
} from "../../fixtures/apiClient.js";
import { getInstanceGatewayToken } from "../../fixtures/dbClient.js";
import { users } from "../../fixtures/users.js";

function firstOpenClawInstance(instances: Awaited<ReturnType<typeof listInstances>>) {
  return instances.instances.find(
    (instance) =>
      instance.type === "openclaw" &&
      instance.status !== "deleting",
  );
}

async function fetchSessionUsage(
  request: Parameters<typeof login>[0],
  accessToken: string,
  instanceId: number,
) {
  return getInstanceSessionUsage(request, accessToken, instanceId, { page: 1, limit: 50 });
}

function isGatewaySuccessStatus(status: number): boolean {
  return status === 200 || status === 201;
}

test("@p1 session usage endpoint returns structured payload for openclaw instance", async ({
  request,
}) => {
  const accessToken = (await login(request, users.admin)).access_token;
  const instances = await listInstances(request, accessToken, { limit: 100 });
  const instance = firstOpenClawInstance(instances);
  test.skip(!instance, "No openclaw instance available for session usage test");

  const data = await fetchSessionUsage(request, accessToken, instance!.id);
  expect(data.summary).toBeTruthy();
  expect(Array.isArray(data.items)).toBe(true);
  expect(typeof data.total).toBe("number");
  expect(typeof data.compliance.recent_fallback_audit_count).toBe("number");
});

test("@p1 session usage detail requires session_id", async ({ request }) => {
  const accessToken = (await login(request, users.admin)).access_token;
  const instances = await listInstances(request, accessToken, { limit: 100 });
  const instance = firstOpenClawInstance(instances);
  test.skip(!instance, "No openclaw instance available for session usage detail test");

  const response = await request.get(
    `${env.backendUrl}/instances/${instance!.id}/session-usage/detail`,
    {
      headers: { Authorization: `Bearer ${accessToken}` },
    },
  );
  expect(response.status()).toBe(400);
});

test("@p0 gateway calls aggregate tokens by openclaw session key", async ({ request }) => {
  const accessToken = (await login(request, users.admin)).access_token;
  const instances = await listInstances(request, accessToken, { limit: 100 });
  const instance = firstOpenClawInstance(instances);
  test.skip(!instance, "No openclaw instance available for gateway aggregation test");

  const models = await listGatewayModels(request, accessToken);
  test.skip(models.length === 0, "No gateway models configured for session aggregation test");

  const baseline = await fetchSessionUsage(request, accessToken, instance!.id);
  const baselineMain = baseline.items.find((item) => item.session_key === "main");
  let successfulCalls = 0;

  for (let attempt = 0; attempt < 3; attempt += 1) {
    const response = await gatewayChatCompletion(request, accessToken, {
      model: "auto",
      instance_id: instance!.id,
      messages: [{ role: "user", content: `session aggregation probe ${Date.now()}-${attempt}` }],
    }, {
      "x-openclaw-session-key": "main",
      "x-openclaw-run-id": `e2e-session-${Date.now()}-${attempt}`,
    });
    if (isGatewaySuccessStatus(response.status())) {
      successfulCalls += 1;
    }
  }
  test.skip(successfulCalls === 0, "Gateway upstream unavailable; skipping token aggregation assertion");

  await expect
    .poll(async () => {
      const latest = await fetchSessionUsage(request, accessToken, instance!.id);
      const main = latest.items.find((item) => item.session_key === "main");
      return main?.invocation_count ?? 0;
    }, { timeout: 20_000 })
    .toBeGreaterThan(baselineMain?.invocation_count ?? 0);

  await expect
    .poll(async () => {
      const latest = await fetchSessionUsage(request, accessToken, instance!.id);
      const main = latest.items.find((item) => item.session_key === "main");
      return main?.total_tokens ?? 0;
    }, { timeout: 20_000 })
    .toBeGreaterThan(baselineMain?.total_tokens ?? 0);
});

test("@p0 gateway missing session key surfaces fallback compliance", async ({ request }) => {
  const accessToken = (await login(request, users.admin)).access_token;
  const instances = await listInstances(request, accessToken, { limit: 100 });
  const instance = firstOpenClawInstance(instances);
  test.skip(!instance, "No openclaw instance available for fallback compliance test");

  const models = await listGatewayModels(request, accessToken);
  test.skip(models.length === 0, "No gateway models configured for fallback compliance test");

  const baseline = await fetchSessionUsage(request, accessToken, instance!.id);
  const response = await gatewayChatCompletion(request, accessToken, {
    model: "auto",
    instance_id: instance!.id,
    messages: [{ role: "user", content: `fallback probe ${Date.now()}` }],
  }, {
    "x-openclaw-run-id": `e2e-fallback-${Date.now()}`,
  });
  test.skip(!isGatewaySuccessStatus(response.status()), "Gateway upstream unavailable; skipping fallback assertion");

  await expect
    .poll(async () => {
      const latest = await fetchSessionUsage(request, accessToken, instance!.id);
      return latest.compliance.has_fallback_sessions;
    }, { timeout: 20_000 })
    .toBe(true);

  await expect
    .poll(async () => {
      const latest = await fetchSessionUsage(request, accessToken, instance!.id);
      return latest.compliance.recent_fallback_audit_count;
    }, { timeout: 20_000 })
    .toBeGreaterThan(baseline.compliance.recent_fallback_audit_count);
});

test("@p0 instance gateway token can call chat completions", async ({ request }) => {
  const accessToken = (await login(request, users.admin)).access_token;
  const instances = await listInstances(request, accessToken, { limit: 100 });
  const instance = firstOpenClawInstance(instances);
  test.skip(!instance, "No openclaw instance available for instance gateway token test");

  let gatewayToken: string | null = null;
  try {
    gatewayToken = await getInstanceGatewayToken(instance!.id);
  } catch {
    test.skip(true, "E2E database unavailable for instance gateway token lookup");
  }
  test.skip(!gatewayToken, "Instance gateway token not provisioned");

  const models = await listGatewayModels(request, gatewayToken);
  test.skip(models.length === 0, "No gateway models configured for instance token test");

  const response = await gatewayChatCompletion(request, gatewayToken, {
    model: "auto",
    messages: [{ role: "user", content: `instance token probe ${Date.now()}` }],
  }, {
    "x-openclaw-session-key": "main",
    "x-openclaw-run-id": `e2e-instance-token-${Date.now()}`,
  });
  expect([200, 201]).toContain(response.status());
});
