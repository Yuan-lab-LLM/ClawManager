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
const routerPath = path.resolve(scriptDir, "../src/router/index.tsx");

const normalizeNewlines = (source) => source.replace(/\r\n/g, "\n");
const pageSource = normalizeNewlines(readFileSync(pagePath, "utf8"));
const serviceSource = normalizeNewlines(readFileSync(servicePath, "utf8"));
const i18nSource = normalizeNewlines(readFileSync(i18nPath, "utf8"));
const routerSource = normalizeNewlines(readFileSync(routerPath, "utf8"));

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
    pageSource.includes("ghcr.io/yuan-lab-llm/agentsruntime/hermes-lite:latest") &&
    pageSource.includes("ghcr.io/yuan-lab-llm/agentsruntime/opencode-lite:latest"),
  "System settings page must follow the published OpenClaw Lite latest image and keep the other Lite defaults.",
);
assert(
  pageSource.includes("addProCustomCard"),
  "System settings page must keep custom Pro runtime card creation.",
);
assert(
  pageSource.includes('HIDDEN_TEAM_RUNTIME_CARD_TYPES') &&
    pageSource.includes("new Set(['workbuddy', 'codex', 'claude-code'])") &&
    pageSource.includes('!HIDDEN_TEAM_RUNTIME_CARD_TYPES.has(card.instance_type)') &&
    pageSource.includes('HIDDEN_TEAM_RUNTIME_CARD_TYPES.has(item.instance_type)') &&
    pageSource.includes('isRuntimeCardVisible(item)'),
  "System settings must statically hide WorkBuddy, Codex, and Claude Code cards from fixed and persisted image sources.",
);
assert(
  !pageSource.includes('/admin/settings/openclaw-upgrade-lab') &&
    !pageSource.includes('OpenClaw 升级实验室') &&
    !routerSource.includes('/admin/settings/openclaw-upgrade-lab') &&
    !routerSource.includes('OpenClawUpgradeLabPage'),
  "The team distribution must expose neither an entry nor a route for the OpenClaw upgrade lab.",
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
assert(
  pageSource.includes("rolloutRuntimeType === 'openclaw'\n            ? ''") &&
    pageSource.includes("runtimePoolService.preflightOpenClawRollout") &&
    pageSource.includes("rolloutImmutableTargetHelp") &&
    pageSource.includes("preflight.strategy === 'openclaw_8plus_data_safe'"),
  "OpenClaw rollout must start empty and pass through the dedicated compatibility preflight.",
);

console.log("System settings runtime grouping source contract is valid.");
