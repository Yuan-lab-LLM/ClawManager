import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const read = (relativePath) =>
  readFileSync(path.join(root, relativePath), "utf8");

const createPage = read("src/pages/instances/CreateInstancePage.tsx");
const detailPage = read("src/pages/instances/InstanceDetailPage.tsx");
const serviceFrame = read("src/components/InstanceServiceFrame.tsx");
const service = read("src/services/instanceService.ts");

assert.ok(
  !createPage.includes("workspace_template") &&
    !createPage.includes("openCodeInitialWorkspace"),
  "The create flow must not expose or send a workspace choice.",
);
assert.ok(
  !detailPage.includes("OpenCodeFirstRunGuide") &&
    !service.includes("getOpenCodeFirstRun") &&
    detailPage.includes('return `${workspacePath.replace(/\\/+$/gu, "")}/starter`;'),
  "The instance page must use starter directly without loading onboarding state.",
);
assert.ok(
  serviceFrame.includes("openCodeNewSessionUrl") &&
    serviceFrame.includes("${directorySlug}/session") &&
    detailPage.includes("openCodeInitialDirectory={openCodeInitialDirectory}"),
  "OpenCode Lite must enter starter's blank session route.",
);

console.log("OpenCode default entry frontend checks passed.");
