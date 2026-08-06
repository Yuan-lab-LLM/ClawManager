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
  detailSource.includes("workspaceVisible") &&
    proRender.includes("workspaceVisible={supportsWorkspace(instance) ? workspaceVisible : undefined}") &&
    proRender.includes("onWorkspaceVisibilityChange={supportsWorkspace(instance) ? setWorkspaceVisible : undefined}") &&
    frameSource.includes("PanelRightOpen") &&
    frameSource.includes("PanelRightClose"),
  "Lite and Pro service frames must expose one shared workspace visibility control.",
);

assert(
  proRender.includes("xl:grid-cols-[minmax(0,7fr)_minmax(380px,3fr)]") &&
    proRender.includes('workspaceVisible ? "xl:grid-cols-[minmax(0,7fr)_minmax(380px,3fr)]" : "xl:grid-cols-1"') &&
    proRender.includes("supportsWorkspace(instance) ?"),
  "Expanded Pro desktop and workspace browser must remain side by side at a stable 70/30 ratio.",
);

const liteRender = sliceBetween(
  detailSource,
  "const renderLiteWorkspace = () => {",
  "\n  const renderProWorkspace = () => (",
);

assert(
  liteRender.includes("workspaceVisible={supportsWorkspace(instance) ? workspaceVisible : undefined}") &&
    liteRender.includes("onWorkspaceVisibilityChange={supportsWorkspace(instance) ? setWorkspaceVisible : undefined}") &&
    liteRender.includes('workspaceVisible ? "xl:grid-cols-[minmax(0,1fr)_minmax(360px,28rem)]" : "xl:grid-cols-1"'),
  "Lite instance detail must keep the workspace visible by default and expand the service frame when hidden.",
);

assert(
  !proRender.includes("aspect-video") &&
    proRender.split("h-[clamp(520px,calc(100vh-10rem),760px)]").length >= 3,
  "Pro desktop and workspace browser must share a bounded height so aspect ratio cannot force cross-column overlap.",
);

assert(
  proRender.indexOf("Runtime Events") > proRender.indexOf("Runtime Overview"),
  "Runtime Events must be lower priority and render after Instance Skills.",
);

assert(
  frameSource.includes("requestFullscreen") &&
    frameSource.includes("Maximize2") &&
    frameSource.includes("Minimize2") &&
    !frameSource.includes("toolbarActions") &&
    frameSource.includes('aria-label={isFullscreen ? t("instances.exitFullscreen")'),
  "Instance service frame must expose a fullscreen control.",
);

console.log("Instance detail pro layout contract is valid.");
