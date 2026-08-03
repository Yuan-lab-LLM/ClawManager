import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const portalSource = readFileSync(
  path.resolve(scriptDir, "../src/pages/instances/InstancePortalPage.tsx"),
  "utf8",
);
const desktopAccessHookSource = readFileSync(
  path.resolve(scriptDir, "../src/hooks/useInstanceDesktopAccess.ts"),
  "utf8",
);

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

assert(
  portalSource.includes("WorkspaceFileManager"),
  "Portal must reuse the instance detail workspace file manager.",
);

assert(
  portalSource.includes('instance.type === "workbuddy"'),
  "Portal must expose the Workbuddy Pro /config workspace.",
);

assert(
  portalSource.includes('selectedInstance.instance_mode === "pro"'),
  "Portal must branch rendering by instance_mode so Lite instances are not shown as Pro.",
);

assert(
  portalSource.includes('return isPro ? "/config" : undefined;') &&
    portalSource.includes("initialPath={workspaceInitialPath(selectedInstance, isProPortal)}"),
  "Portal Pro workspace file manager must match detail page config-root behavior.",
);

assert(
  portalSource.includes('data-portal-mode={isProPortal ? "pro" : "lite"}'),
  "Portal must expose separate Pro and Lite layout markers.",
);

assert(
  portalSource.includes("xl:grid-rows-[minmax(0,1fr)]") &&
    portalSource.includes("xl:overflow-hidden") &&
    !portalSource.includes("content-start overflow-y-auto") &&
    !portalSource.includes("aspect-video"),
  "Portal Pro desktop layout must stretch to fill the available height without leaving a lower blank area.",
);

assert(
  portalSource.includes("canShowRuntimeOverlay") &&
    portalSource.includes("isProPortal"),
  "Portal runtime controls must be limited to Pro layout.",
);

assert(
  !portalSource.includes("__openclaw__/canvas"),
  "Portal Pro desktop instances must use the backend proxy entry instead of the OpenClaw gateway canvas route.",
);

assert(
  portalSource.includes("portalEmbedUrl") &&
    portalSource.includes("portalEmbedUrlForInstance") &&
    portalSource.includes("preparedPortalFrame.embedUrl === portalEmbedUrl") &&
    portalSource.includes("src={portalFrameSrc}"),
  "Portal iframe must use a mode-aware embed URL instead of the raw access URL.",
);

assert(
  portalSource.includes("instanceIdFromProxyUrl") &&
    portalSource.includes("embedUrlInstanceId !== instance.id"),
  "Portal iframe must not reuse an access URL from a previously selected instance.",
);

assert(
  portalSource.includes("setShouldConnect(false)") &&
    portalSource.includes("instance.id !== selectedId"),
  "Portal must reset the connection intent before switching to another instance.",
);

assert(
  portalSource.includes("pendingConnectInstanceIdRef") &&
    portalSource.includes("pendingConnectInstanceIdRef.current === nextInstanceId") &&
    portalSource.includes("shouldConnect && portalEmbedUrl"),
  "Portal access generation must target the currently selected instance after switching.",
);

assert(
  portalSource.includes('selectedInstanceStatus !== "running"') &&
    portalSource.includes("requestAccess();"),
  "Portal must auto-connect running instances when switching in the portal.",
);

assert(
  /modeLabel\(\s*instance\.instance_mode\s*\)/.test(portalSource) &&
    /modeClass\(\s*instance\.instance_mode\s*,?\s*\)/.test(portalSource),
  "Portal left instance list must display the Lite/Pro instance type.",
);

assert(
  desktopAccessHookSource.includes("refreshAccess({ forceReload: true })"),
  "Desktop access hook must force a fresh access URL when connecting a selected instance.",
);

console.log("Instance portal mode layout contract is valid.");
