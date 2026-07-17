import { expect, test } from "../../fixtures/test.js";
import { env } from "../../fixtures/env.js";
import { login, getLLMGovernanceOverview } from "../../fixtures/apiClient.js";
import { users } from "../../fixtures/users.js";
import { execFileSync } from "node:child_process";

interface ApiEnvelope<T> {
  success: boolean;
  data?: T;
  error?: string;
}

function egressProxyOrigin(): string {
  return env.backendUrl.replace(/\/api\/v1\/?$/, "");
}

test("@p2 create openclaw instance rejects protected env override", async ({ request }) => {
  const tokens = await login(request, users.admin);
  const suffix = Date.now();

  const response = await request.post(`${env.backendUrl}/instances`, {
    headers: { Authorization: `Bearer ${tokens.access_token}` },
    data: {
      name: `e2e-governance-${suffix}`,
      type: "openclaw",
      mode: "lite",
      cpu_cores: 1,
      memory_gb: 2,
      disk_gb: 20,
      gpu_enabled: false,
      gpu_count: 0,
      os_type: "openclaw",
      os_version: "latest",
      environment_overrides: {
        OPENAI_BASE_URL: "https://api.openai.com/v1",
      },
    },
  });

  expect(response.status()).toBeGreaterThanOrEqual(400);
  const body = (await response.json()) as ApiEnvelope<unknown>;
  expect(body.success).toBe(false);
  expect(body.error ?? "").toMatch(/managed by the platform/i);
});

test("@p2 batch lite create rejects protected env override", async ({ request }) => {
  const tokens = await login(request, users.admin);
  const suffix = Date.now();

  const response = await request.post(`${env.backendUrl}/instances/batch/lite`, {
    headers: { Authorization: `Bearer ${tokens.access_token}` },
    data: {
      name_prefix: `e2e-batch-gov-${suffix}`,
      count: 1,
      template: {
        type: "openclaw",
        environment_overrides: {
          OPENAI_API_KEY: "sk-test",
        },
      },
    },
  });

  expect(response.status()).toBeGreaterThanOrEqual(400);
  const body = (await response.json()) as ApiEnvelope<unknown>;
  expect(body.success).toBe(false);
  expect(body.error ?? "").toMatch(/managed by the platform/i);
});

test("@p2 admin llm governance overview returns managed runtime summary", async ({ request }) => {
  const tokens = await login(request, users.admin);
  const overview = await getLLMGovernanceOverview(request, tokens.access_token);

  expect(typeof overview.total_managed_instances).toBe("number");
  expect(Array.isArray(overview.items)).toBe(true);
});

test("@p2 @local-only egress proxy blocks direct openai connect", async () => {
  let statusCode = "";
  try {
    statusCode = execFileSync(
      "curl",
      [
        "-x",
        egressProxyOrigin(),
        "-H",
        "X-ClawManager-Egress-Instance-Id: 1",
        "-m",
        "5",
        "-s",
        "-o",
        process.platform === "win32" ? "NUL" : "/dev/null",
        "-w",
        "%{http_code}",
        "https://api.openai.com",
      ],
      { encoding: "utf8" },
    ).trim();
  } catch {
    test.skip(true, "curl unavailable or egress proxy not reachable");
  }

  expect(statusCode).toBe("403");
});
