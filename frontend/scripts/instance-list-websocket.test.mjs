import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const listSource = readFileSync(
  path.resolve(scriptDir, "../src/pages/instances/InstanceListPage.tsx"),
  "utf8",
);
const detailSource = readFileSync(
  path.resolve(scriptDir, "../src/pages/instances/InstanceDetailPage.tsx"),
  "utf8",
);

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

assert(
  !listSource.includes("useInstanceStatusWebSocket") &&
    !listSource.includes("InstanceStatusUpdate"),
  "Instance list must not keep a long-lived instance status WebSocket connection.",
);

assert(
  listSource.includes("refetchInterval: 5000") && listSource.includes("refetchOnReconnect:"),
  "Instance list must keep periodic snapshot synchronization and reconnect recovery without a WebSocket.",
);

assert(
  detailSource.includes("useInstanceStatusWebSocket"),
  "Instance detail should keep WebSocket updates for focused real-time status.",
);

console.log("Instance list WebSocket contract is valid.");
