import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const read = (relativePath) => readFileSync(path.resolve(scriptDir, relativePath), "utf8");
const router = read("../src/router/index.tsx");
const listPage = read("../src/pages/instances/IEISystemListInstancesPage.tsx");
const detailPage = read("../src/pages/instances/IEISystemInstancePage.tsx");
const service = read("../src/services/ieiSystemService.ts");
const renewalHook = read("../src/hooks/useExpiringResourceRenewal.ts");
const instanceAccess = read("../src/lib/instanceAccess.ts");
const workspaceManager = read("../src/components/WorkspaceFileManager.tsx");
const translations = read("../src/lib/i18n.ts");
const runtimeCatalog = read("../src/lib/ieiRuntimeCatalog.ts");
const mockServer = read("./iei-mock-server.mjs");

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

assert(
  router.includes('path="/ieisystem/list-instances"') &&
    router.includes('path="/ieisystem/instances/:id"') &&
    !router.includes('path="/northbound/owners/:owner/lite-instances"'),
  "IEI pages must use the fixed public routes and remove the old owner URL.",
);

assert(
  service.includes('axios.create({') &&
    service.includes("withCredentials: true") &&
    service.includes('post("/session/refresh")') &&
    !service.includes('from "./api"') &&
    !service.toLowerCase().includes("share"),
  "IEI access must use a dedicated cookie client and must not reuse ShareLink or normal JWT auth.",
);

assert(
  listPage.includes('params.get("token")') &&
    listPage.includes("window.history.replaceState") &&
    listPage.includes("exchangeSession(token)") &&
    listPage.includes('meta[name="referrer"]'),
  "The one-time SSO token must be exchanged and removed from the browser URL with no-referrer policy.",
);

assert(
  listPage.includes("InstanceTypeIcon") &&
    listPage.includes('src="/openclaw.png"') &&
    listPage.includes('src="/hermes.png"') &&
    listPage.includes('src="/opencode.png"') &&
    listPage.includes('src="/deepseek-harness.svg"') &&
    listPage.includes('src="/workbuddy.png"') &&
    !listPage.includes("instance.description"),
  "IEI instance cards must show their runtime icon without the Created by description line.",
);

assert(
  listPage.includes("我的实例") &&
    listPage.includes("实例信息") &&
    listPage.includes("运行时说明") &&
    listPage.includes('src="/inspur-information.png"') &&
    listPage.includes('alt="浪潮信息"') &&
    listPage.includes("智慧协作门户") &&
    !listPage.includes("OWNER PORTAL") &&
    !listPage.includes("实例模式") &&
    !runtimeCatalog.includes("Lite") &&
    !runtimeCatalog.includes("Pro") &&
    runtimeCatalog.includes('"deepseek-harness"') &&
    runtimeCatalog.includes('workbuddy: {') &&
    runtimeCatalog.includes('badge: "NEW"'),
  "The owner portal must use the three-column runtime-aware layout without exposing deployment modes.",
);

assert(
  listPage.includes("工作空间尚未分配") &&
    listPage.includes("支持的工作空间") &&
    listPage.includes("所有者身份已验证") &&
    listPage.includes("实例状态自动同步") &&
    listPage.includes("一切皆插件") &&
    runtimeCatalog.includes('tagline: "一切皆插件，按需组合智能体能力"') &&
    runtimeCatalog.includes('tagline: "围绕信息、文档与事务持续协作"'),
  "The owner portal empty state must present team-distribution runtimes while retaining historical presentation metadata.",
);

assert(
  ["openclaw", "hermes", "opencode", "deepseek-harness"].every(
    (type) => (mockServer.match(new RegExp(`type: "${type}"`, "g")) ?? []).length === 2,
  ) &&
    (mockServer.match(/type: "workbuddy"/g) ?? []).length === 2 &&
    (mockServer.match(/runtime_variant: "linux"/g) ?? []).length === 2,
  "The local IEI test server must provide two instances for every supported runtime and Linux-only WorkBuddy data.",
);

assert(
  listPage.includes("useExpiringResourceRenewal") &&
    listPage.includes("refreshSession()") &&
    detailPage.includes("refreshSession()") &&
    detailPage.includes("useExpiringResourceRenewal") &&
    detailPage.includes("refreshDedicatedRuntimeCookie") &&
    instanceAccess.includes("__clawmanager_access_refresh") &&
    instanceAccess.includes('mode: "no-cors"') &&
    renewalHook.includes('"visibilitychange"') &&
    renewalHook.includes('"focus"') &&
    mockServer.includes('/api/v1/ieisystem/session/refresh'),
  "Active IEI pages must renew the local session and dedicated runtime cookie without reloading the agent iframe.",
);

assert(
  detailPage.includes("getInstance(instanceID)") &&
    detailPage.includes("generateAccess(instanceID)") &&
    detailPage.includes("useRuntimeCertificateTrust") &&
    detailPage.includes('normalizedType === "opencode"') &&
    detailPage.includes('normalizedType === "deepseek-harness"') &&
    detailPage.includes("certificateConfirmationRequired") &&
    detailPage.includes("confirmCertificate") &&
    detailPage.includes("浪潮信息安全访问") &&
    detailPage.includes('referrerPolicy="no-referrer"') &&
    detailPage.includes("WorkspaceFileManager") &&
    detailPage.includes("ieiSystemWorkspaceService") &&
    detailPage.includes('localeOverride="zh"') &&
    detailPage.includes("xl:grid-cols-[minmax(0,1fr)_minmax(360px,28rem)]"),
  "Entering an IEI instance must validate ownership, request separate access, and default its workspace labels to Chinese.",
);

assert(
  service.includes("/workspace/files") &&
    service.includes("/workspace/preview") &&
    service.includes("/workspace/download") &&
    service.includes("/workspace/upload") &&
    service.includes("/workspace/folders") &&
    service.includes("/workspace/entries"),
  "The IEI detail page must expose the same workspace file operations as the ShareLink page.",
);

assert(
  service.includes("/lifecycle-operation") &&
    service.includes("/lifecycle-operations/") &&
    service.includes('"Idempotency-Key"') &&
    />\s*重启实例\s*<\/button>/.test(listPage) &&
    !listPage.includes("重启实例（推荐）") &&
    listPage.includes("页面会持续同步状态") &&
    listPage.includes("getLatestLifecycleOperation") &&
    listPage.includes("getLifecycleOperation") &&
    !listPage.includes("window.prompt") &&
    listPage.includes("将永久删除其中的全部文件、配置、技能、任务和会话") &&
    listPage.includes("系统不会自动备份，请先下载需要保留的数据") &&
    listPage.includes("原数据无法恢复") &&
    listPage.includes("lifecycleDisplayStatus") &&
    listPage.includes('return operation?.action === "reset" ? "resetting" : "restarting"') &&
    detailPage.includes("operation.status === \"succeeded\"") &&
    detailPage.includes("getLifecycleOperation"),
  "IEI lifecycle actions must be idempotent, recoverable after refresh, prefer restart, require two explicit reset confirmations, and block access until completion.",
);

assert(
  workspaceManager.includes("useI18n") &&
    workspaceManager.includes("localeOverride") &&
    workspaceManager.includes("entry.downloadable &&") &&
    workspaceManager.includes("entry.is_dir ? `${name}.zip` : name") &&
    workspaceManager.includes('translateLabel("workspaceFileManager.workspace")') &&
    workspaceManager.includes('translateLabel("workspaceFileManager.name")') &&
    workspaceManager.includes('translateLabel("workspaceFileManager.size")') &&
    workspaceManager.includes('translateLabel("workspaceFileManager.modified")') &&
    workspaceManager.includes('translateLabel("workspaceFileManager.actions")') &&
    translations.includes("workspaceFileManagerTranslations") &&
    translations.includes('workspace: "工作区"') &&
    translations.includes('workspace: "ワークスペース"') &&
    translations.includes('workspace: "작업 공간"') &&
    translations.includes('workspace: "Arbeitsbereich"'),
  "The shared IEI workspace file manager headings must follow the selected application language.",
);

console.log("IEI system page contract is valid.");
