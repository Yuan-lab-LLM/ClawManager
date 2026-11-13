import { defaultRuntimePod } from "../../fixtures/apiClient.js";
import { test } from "../../fixtures/test.js";
import { users } from "../../fixtures/users.js";
import { AdminRuntimePodsPage } from "../../pages/AdminRuntimePodsPage.js";

test.use({ storageState: users.admin.storageState });

test("@p0 @local-only admin runtime pods page renders no runtime pods for an empty filter", async ({ page }) => {
  const runtimePodsPage = new AdminRuntimePodsPage(page);

  await runtimePodsPage.goto();
  await runtimePodsPage.selectRuntimeFilter("Hermes");
  await runtimePodsPage.expectEmptyState();
});

test("@p0 @local-only admin runtime pods page renders seeded pod status and capacity", async ({ page }) => {
  const runtimePodsPage = new AdminRuntimePodsPage(page);

  await runtimePodsPage.goto();
  await runtimePodsPage.selectRuntimeFilter("OpenClaw");
  await runtimePodsPage.expectPodStatusAndCapacity(defaultRuntimePod);
  await runtimePodsPage.expectPodMetricsVisible(defaultRuntimePod);
});
