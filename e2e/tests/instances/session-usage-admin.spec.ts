import { expect, test } from "../../fixtures/test.js";
import { env } from "../../fixtures/env.js";
import {
  getAdminSessionUsageOverview,
  getInstanceSessionUsage,
  listInstances,
  login,
  registerUser,
} from "../../fixtures/apiClient.js";
import { users } from "../../fixtures/users.js";

function firstManagedRuntimeInstance(instances: Awaited<ReturnType<typeof listInstances>>) {
  return instances.instances.find(
    (instance) =>
      (instance.type === "openclaw" || instance.type === "hermes") &&
      instance.status !== "deleting",
  );
}

test("@p1 admin session usage overview returns structured payload", async ({ request }) => {
  const accessToken = (await login(request, users.admin)).access_token;
  const overview = await getAdminSessionUsageOverview(request, accessToken, {
    page: 1,
    limit: 20,
  });

  expect(overview.summary).toBeTruthy();
  expect(Array.isArray(overview.items)).toBe(true);
  expect(typeof overview.total).toBe("number");
  expect(typeof overview.page).toBe("number");
  expect(typeof overview.limit).toBe("number");
});

test("@p1 admin session usage overview rejects invalid since timestamp", async ({ request }) => {
  const accessToken = (await login(request, users.admin)).access_token;
  const response = await request.get(`${env.backendUrl}/admin/session-usage/overview`, {
    headers: { Authorization: `Bearer ${accessToken}` },
    params: { since: "not-a-date" },
  });
  expect(response.status()).toBe(400);
});

test("@p1 instance session usage accepts since query parameter", async ({ request }) => {
  const accessToken = (await login(request, users.admin)).access_token;
  const instances = await listInstances(request, accessToken, { limit: 100 });
  const instance = firstManagedRuntimeInstance(instances);
  test.skip(!instance, "No openclaw/hermes instance available for since-filter test");

  const since = new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString();
  const data = await getInstanceSessionUsage(request, accessToken, instance!.id, {
    page: 1,
    limit: 20,
    since,
  });

  expect(data.summary).toBeTruthy();
  expect(Array.isArray(data.items)).toBe(true);
});

test("@p1 instance session usage rejects invalid since timestamp", async ({ request }) => {
  const accessToken = (await login(request, users.admin)).access_token;
  const instances = await listInstances(request, accessToken, { limit: 100 });
  const instance = firstManagedRuntimeInstance(instances);
  test.skip(!instance, "No openclaw/hermes instance available for invalid since test");

  const response = await request.get(
    `${env.backendUrl}/instances/${instance!.id}/session-usage`,
    {
      headers: { Authorization: `Bearer ${accessToken}` },
      params: { since: "bad-timestamp" },
    },
  );
  expect(response.status()).toBe(400);
});

test("@p2 non-admin cannot access session usage overview", async ({ request }) => {
  await registerUser(request, users.user);
  const accessToken = (await login(request, users.user)).access_token;
  const response = await request.get(`${env.backendUrl}/admin/session-usage/overview`, {
    headers: { Authorization: `Bearer ${accessToken}` },
  });
  expect(response.status()).toBe(403);
});

test("@p1 session usage rejects until before since", async ({ request }) => {
  const accessToken = (await login(request, users.admin)).access_token;
  const response = await request.get(`${env.backendUrl}/admin/session-usage/overview`, {
    headers: { Authorization: `Bearer ${accessToken}` },
    params: {
      since: "2026-07-10T00:00:00Z",
      until: "2026-07-01T00:00:00Z",
    },
  });
  expect(response.status()).toBe(400);
});
