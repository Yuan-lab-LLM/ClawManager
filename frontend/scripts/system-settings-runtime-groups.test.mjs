import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const pagePath = path.resolve(
  scriptDir,
  "../src/pages/admin/SystemSettingsPage.tsx",
);
const servicePath = path.resolve(
  scriptDir,
  "../src/services/systemSettingsService.ts",
);
const i18nPath = path.resolve(scriptDir, "../src/lib/i18n.ts");

const pageSource = readFileSync(pagePath, "utf8");
const serviceSource = readFileSync(servicePath, "utf8");
const i18nSource = readFileSync(i18nPath, "utf8");

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

assert(
  serviceSource.includes('"desktop" | "gateway"'),
  "System image settings must expose desktop/gateway runtime types.",
);
assert(
  !serviceSource.includes('"desktop" | "shell"'),
  "System image settings must not expose shell as the Lite runtime type.",
);
assert(
  pageSource.includes("LITE_RUNTIME_CARDS") &&
    pageSource.includes("PRO_BASE_RUNTIME_CARDS"),
  "System settings page must define separate Lite and Pro runtime groups.",
);
assert(
  pageSource.includes("runtime_type: 'gateway'") &&
    pageSource.includes("runtime_type: 'desktop'"),
  "System settings page must map Lite to gateway and Pro to desktop.",
);
assert(
  pageSource.includes("ghcr.io/yuan-lab-llm/agentsruntime/openclaw-lite:latest") &&
    pageSource.includes("ghcr.io/yuan-lab-llm/agentsruntime/hermes-lite:latest"),
  "System settings page must use the Lite default runtime images.",
);
assert(
  pageSource.includes("addProCustomCard"),
  "System settings page must keep custom Pro runtime card creation.",
);
assert(
  pageSource.includes("instance_type: 'workbuddy'") &&
    pageSource.includes("display_name: 'Workbuddy Pro'") &&
    pageSource.includes("agentsruntime/workbuddy-linux:latest"),
  "System settings page must expose Workbuddy as a fixed Pro runtime card.",
);
assert(
  pageSource.includes("systemSettingsPage.liteRolloutTitle") &&
    pageSource.includes("systemSettingsPage.proRuntimeTitle") &&
    i18nSource.includes("Lite runtime rolling upgrade") &&
    i18nSource.includes("Pro runtime"),
  "System settings page must render separate rollout, Lite, and Pro sections.",
);
assert(
  pageSource.includes("runtimePoolService.listPods(rolloutRuntimeType)") &&
    pageSource.includes("rolloutCurrentImage"),
  "System settings rollout current image must come from live runtime pods, not only saved card settings.",
);
assert(
  !pageSource.includes("RUNTIME_TYPE_OPTIONS"),
  "System settings page must not expose the legacy Shell/Desktop selector.",
);

console.log("System settings runtime grouping source contract is valid.");
