import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(scriptDir, "..");

const pageSource = readFileSync(
  path.join(frontendRoot, "src/pages/admin/ModelManagementPage.tsx"),
  "utf8",
);
const serviceSource = readFileSync(
  path.join(frontendRoot, "src/services/modelService.ts"),
  "utf8",
);

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

for (const marker of [
  "provider_models?: DiscoveredProviderModel[]",
  "discovered_models: []",
  "discovered_models: card.discovered_models ?? []",
]) {
  assert(
    pageSource.includes(marker) || serviceSource.includes(marker),
    `Provider catalog contract missing: ${marker}`,
  );
}

assert(
  pageSource.includes("<ModelDiscoveryPlan") &&
    pageSource.includes("onExcludedChange=") && pageSource.includes("onChange={(modelId, patch)"),
  "Discovery must remain an editable selection plan, not implicitly deployed models.",
);

assert(!pageSource.includes("provider_models: card.discovered_models"),
  "Saving one deployment must not implicitly authorize the discovered catalog.");

console.log("Managed provider model catalog contract is valid.");
