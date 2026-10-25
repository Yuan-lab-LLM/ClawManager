import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const detailSource = readFileSync(
  path.resolve(scriptDir, "../src/pages/instances/InstanceDetailPage.tsx"),
  "utf8",
).replace(/\r\n/g, "\n");
const frameSource = readFileSync(
  path.resolve(scriptDir, "../src/components/InstanceServiceFrame.tsx"),
  "utf8",
).replace(/\r\n/g, "\n");

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

function countOccurrences(source, value) {
  return source.split(value).length - 1;
}

assert(
  detailSource.includes("const RESTART_NOTICE_AUTO_DISMISS_MS = 6000") &&
    detailSource.includes("restartNoticeTimerRef") &&
    detailSource.includes("window.setTimeout"),
  "Restart success notices must have a bounded auto-dismiss timer.",
);

assert(
  countOccurrences(detailSource, "markRestartSubmitted(") === 4 &&
    countOccurrences(
      detailSource,
      'markRestartSubmitted(t("instances.restartSubmitted"))',
    ) === 2 &&
    detailSource.includes(
      'markRestartSubmitted(t("instances.restartEnvironmentSaved"))',
    ) &&
    detailSource.includes(
      'markRestartSubmitted(\n        "OpenCode project saved. The instance is restarting.",',
    ),
  "Every restart success path must finalize through markRestartSubmitted.",
);

assert(
  detailSource.includes("cancelRestartNoticeTimer();") &&
    detailSource.includes("current === message ? null : current"),
  "Restart notice timers must be cancelled and must not clear a newer message.",
);

assert(
  detailSource.includes('aria-label={t("common.close")}') &&
    detailSource.includes("onClick={dismissRestartNotice}"),
  "Restart notices must retain a manual dismissal fallback.",
);

assert(
  countOccurrences(detailSource, "reloadToken={serviceFrameReloadToken}") ===
      2 &&
    detailSource.includes("setServiceFrameReloadToken") &&
    frameSource.includes("reloadToken?: number") &&
    frameSource.includes("-${reloadToken}"),
  "Successful restarts must remount the embedded service frame.",
);

console.log("Instance restart notice contract is valid.");
