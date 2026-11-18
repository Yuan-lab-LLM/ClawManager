import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(scriptDir, "..");
const repoRoot = path.resolve(frontendRoot, "..");

const trustHookSource = readFileSync(
  path.join(frontendRoot, "src/hooks/useRuntimeCertificateTrust.ts"),
  "utf8",
);
const serviceFrameSource = readFileSync(
  path.join(frontendRoot, "src/components/InstanceServiceFrame.tsx"),
  "utf8",
);
const portalSource = readFileSync(
  path.join(frontendRoot, "src/pages/instances/InstancePortalPage.tsx"),
  "utf8",
);
const sharedInstanceSource = readFileSync(
  path.join(frontendRoot, "src/pages/instances/SharedInstancePage.tsx"),
  "utf8",
);
const ieiInstanceSource = readFileSync(
  path.join(frontendRoot, "src/pages/instances/IEISystemInstancePage.tsx"),
  "utf8",
);
const nginxSource = readFileSync(
  path.join(repoRoot, "deployments/nginx/nginx.conf"),
  "utf8",
);

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

for (const marker of [
  'const CERTIFICATE_CHECK_PATH = "/__clawmanager_cert_check"',
  'const CERTIFICATE_TRUST_PATH = "/__clawmanager_cert_trust"',
  'mode: "no-cors"',
  'credentials: "omit"',
  'referrerPolicy: "no-referrer"',
  "window.sessionStorage",
  'window.open(target.trustUrl, "_blank", "noopener,noreferrer")',
  'window.addEventListener("pageshow"',
  'window.addEventListener("focus"',
  'document.addEventListener("visibilitychange"',
]) {
  assert(trustHookSource.includes(marker), `Runtime certificate trust hook missing: ${marker}`);
}

assert(!trustHookSource.includes("window.location.assign"), "A failed probe must never navigate the CM page away.");

assert(
  trustHookSource.includes("new URL(CERTIFICATE_TRUST_PATH, parsed.origin)") &&
    !trustHookSource.includes("accessToken"),
  "Certificate confirmation URL must be derived from the runtime origin without an access token.",
);

assert(
  serviceFrameSource.includes("useRuntimeCertificateTrust") &&
    serviceFrameSource.includes('normalizedType === "opencode"') &&
    serviceFrameSource.includes('normalizedType === "deepseek-harness"') &&
    serviceFrameSource.includes("certificateConfirmationRequired") &&
    serviceFrameSource.includes("confirmCertificate"),
  "Instance detail service frame must gate OpenCode and DSH iframe loading on certificate confirmation.",
);

assert(
  portalSource.includes("useRuntimeCertificateTrust") &&
    portalSource.includes("certificateConfirmationRequired") &&
    portalSource.includes("confirmCertificate();"),
  "Instance portal must reuse the certificate confirmation flow.",
);

assert(
  sharedInstanceSource.includes("useRuntimeCertificateTrust") &&
    sharedInstanceSource.includes('normalizedType === "opencode"') &&
    sharedInstanceSource.includes('normalizedType === "deepseek-harness"') &&
    sharedInstanceSource.includes("certificateConfirmationRequired") &&
    sharedInstanceSource.includes("confirmCertificate"),
  "Shared instance page must gate OpenCode and DSH iframe loading on certificate confirmation.",
);

assert(
  ieiInstanceSource.includes("useRuntimeCertificateTrust") &&
    ieiInstanceSource.includes('normalizedType === "opencode"') &&
    ieiInstanceSource.includes('normalizedType === "deepseek-harness"') &&
    ieiInstanceSource.includes("certificateConfirmationRequired") &&
    ieiInstanceSource.includes("confirmCertificate"),
  "IEI instance page must gate OpenCode and DSH iframe loading on certificate confirmation.",
);

for (const endpoint of [
  "location = /__clawmanager_cert_check",
  "location = /__clawmanager_cert_trust",
]) {
  assert(
    nginxSource.split(endpoint).length - 1 === 2,
    `${endpoint} must exist once for OpenCode and once for DSH.`,
  );
}

assert(
  !nginxSource.includes("window.history.back()") &&
    nginxSource.split("Close this tab and return to ClawManager.").length - 1 === 2,
  "Both runtime confirmation pages must explain returning from the new tab without automatic navigation.",
);

assert(!portalSource.includes("onPointerUp={retryAccess}"), "A portal click must not open confirmation twice.");

for (const source of [serviceFrameSource, portalSource, sharedInstanceSource, ieiInstanceSource]) {
  assert(source.includes("certificateOrigin"), "Certificate confirmation must identify the runtime origin.");
}

console.log("Runtime certificate confirmation contract is valid.");
