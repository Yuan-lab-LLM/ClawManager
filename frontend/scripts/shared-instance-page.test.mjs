import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const read = (relativePath) =>
  readFileSync(path.resolve(scriptDir, relativePath), "utf8");
const routerSource = read("../src/router/index.tsx");
const pageSource = read("../src/pages/instances/SharedInstancePage.tsx");
const sharedServiceSource = read("../src/services/sharedInstanceService.ts");
const workspaceSource = read("../src/components/WorkspaceFileManager.tsx");
const teamServiceSource = read("../src/services/teamService.ts");

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

assert(
  routerSource.includes('path="/share/:code"') &&
    routerSource.includes("element={<SharedInstancePage />}") &&
    !routerSource.includes("<ProtectedRoute><SharedInstancePage"),
  "Shared instance page must be public without using the authenticated route wrapper.",
);

assert(
  pageSource.includes("<WorkspaceFileManager") &&
    pageSource.includes("createSharedWorkspaceService") &&
    pageSource.includes('workspaceKey={`share:${code}`}') &&
    pageSource.includes('session.workspace_access === "write"'),
  "Shared page must render the reusable single-instance workspace with scoped capabilities.",
);

assert(
  pageSource.includes("const [workspaceVisible, setWorkspaceVisible] = useState(true)") &&
    pageSource.includes("PanelRightClose") &&
    pageSource.includes("PanelRightOpen") &&
    pageSource.includes("canShowWorkspace && workspaceVisible") &&
    pageSource.includes("workspaceVisible && (canShowWorkspace ?"),
  "Shared page must expose the workspace visibility control without changing the default visible state.",
);

assert(
  sharedServiceSource.includes('axios.create({') &&
    !sharedServiceSource.includes('from "./api"') &&
    sharedServiceSource.includes("X-ClawManager-Share-CSRF"),
  "Shared workspace must use an anonymous client and protect mutations with the share CSRF token.",
);

assert(
  workspaceSource.includes("service = workspaceService") &&
    workspaceSource.includes("canWrite = true") &&
    workspaceSource.includes('queryKey: ["workspace", cacheKey, currentPath]'),
  "Existing instance workspace behavior must remain the default while share caches stay isolated.",
);

assert(
  !pageSource.includes("teamService") &&
    teamServiceSource.includes("/teams/${teamId}/workspace/files"),
  "Shared instance files must not use the Team workspace transport.",
);

console.log("Shared single-instance page contract is valid.");
