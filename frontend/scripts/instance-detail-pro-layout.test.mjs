import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const detailSource = readFileSync(
  path.resolve(scriptDir, "../src/pages/instances/InstanceDetailPage.tsx"),
  "utf8",
);
const frameSource = readFileSync(
  path.resolve(scriptDir, "../src/components/InstanceServiceFrame.tsx"),
  "utf8",
);
const skillPanelSource = readFileSync(
  path.resolve(scriptDir, "../src/components/InstanceSkillHubPanel.tsx"),
  "utf8",
);

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

assert(
  detailSource.includes("isDedicatedInstance") &&
    detailSource.includes('instance.instance_mode === "pro"') &&
    detailSource.includes('instance.runtime_type !== "gateway"'),
  "Instance detail page must branch pro/desktop instances away from the lite workspace layout.",
);

for (const label of ["Runtime Overview", "Runtime Events"]) {
  assert(detailSource.includes(label), `Pro instance detail section missing: ${label}`);
}

assert(
  detailSource.includes("<InstanceSkillHubPanel"),
  "Pro instance detail section missing: InstanceSkillHubPanel",
);

assert(
  detailSource.includes("getRuntimeDetails"),
  "Pro instance detail page must load runtime details.",
);
for (const api of ["listInstanceSkills", "attachSkillToInstance", "removeSkillFromInstance"]) {
  assert(skillPanelSource.includes(api), `Pro instance skill panel must use ${api}.`);
}

assert(
  detailSource.includes("renderLiteWorkspace") && detailSource.includes("renderProWorkspace"),
  "Instance detail page must keep separate lite and pro render paths.",
);

assert(
  detailSource.includes('instance.type === "workbuddy"'),
  "Instance detail must expose the Workbuddy Pro /config workspace.",
);

function sliceBetween(source, startMarker, endMarker) {
  const start = source.indexOf(startMarker);
  assert(start >= 0, `Missing marker: ${startMarker}`);
  const end = source.indexOf(endMarker, start);
  assert(end > start, `Missing end marker after: ${startMarker}`);
  return source.slice(start, end);
}

const proRender = sliceBetween(
  detailSource,
  "const renderProWorkspace = () => (",
  "\n  return (",
);

assert(
  proRender.includes("shareLinkControl"),
  "Pro instance detail must place Share Link with the primary instance action buttons.",
);

assert(
  !detailSource.includes("externalAccessSection") && !detailSource.includes("externalAccessCompact"),
  "Instance detail must not render the old full-width or inline external access controls.",
);

assert(
  detailSource.includes('data-panel="share-link-popover"') &&
    detailSource.includes("Share link expiration") &&
    detailSource.includes("Create Password"),
  "Share link expiration and password controls must live inside the Share Link popover.",
);

assert(
  proRender.includes('data-section="runtime-overview"') &&
    proRender.indexOf("<InstanceSkillHubPanel") < proRender.indexOf("Runtime Overview"),
  "Instance Skills must render before the compact Runtime Overview card.",
);

assert(
  !proRender.includes("Resource Monitor") && !proRender.includes("Runtime Status"),
  "Runtime Overview must use a compact summary instead of large nested Resource Monitor and Runtime Status sections.",
);

assert(
  proRender.includes('initialPath="/config"'),
  "Pro instance workspace must open at /config.",
);

assert(
  proRender.includes("pro-desktop-workspace"),
  "Pro instance workspace must be placed in the desktop workspace area.",
);

assert(
  !proRender.includes("h-[560px]") &&
    (proRender.includes("aspect-video") || proRender.includes("aspect-[16/9]")),
  "Pro desktop frame must use a stable 16:9 desktop ratio instead of the old fixed height.",
);

assert(
  proRender.indexOf("Runtime Events") > proRender.indexOf("Runtime Overview"),
  "Runtime Events must be lower priority and render after Instance Skills.",
);

assert(
  frameSource.includes("requestFullscreen") &&
    frameSource.includes("Maximize2") &&
    frameSource.includes("Minimize2"),
  "Instance service frame must expose a fullscreen control.",
);

console.log("Instance detail pro layout contract is valid.");
