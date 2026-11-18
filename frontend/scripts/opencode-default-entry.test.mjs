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
    !detailPage.includes('? "starter"'),
  "The workspace browser must not force OpenCode Lite into a managed starter directory.",
);
assert.ok(
  !serviceFrame.includes("openCodeNewSessionUrl") &&
    !serviceFrame.includes("encodeOpenCodeDirectory") &&
    !serviceFrame.includes("openCodeInitialDirectory") &&
    !detailPage.includes("openCodeInitialDirectory"),
  "Opening OpenCode must not force a directory-scoped blank session route.",
);
assert.ok(
  serviceFrame.includes("let src = embedUrl;") &&
    serviceFrame.includes("setPreparedFrame({ instanceId, embedUrl, src });") &&
    serviceFrame.includes("src={frameSrc}"),
  "The service frame must preserve the runtime's normal web entry URL.",
);
assert.ok(
  !serviceFrame.includes("InstanceShellTerminal") &&
    serviceFrame.includes("<iframe"),
  "OpenCode must remain a web interface without a terminal fallback.",
);

console.log("OpenCode default entry frontend checks passed.");
