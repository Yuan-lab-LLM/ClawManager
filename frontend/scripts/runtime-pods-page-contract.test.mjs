import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const layoutPath = path.resolve(scriptDir, "../src/components/AdminLayout.tsx");
const pagePath = path.resolve(scriptDir, "../src/pages/admin/RuntimePodsPage.tsx");
const i18nPath = path.resolve(scriptDir, "../src/lib/i18n.ts");

const layoutSource = readFileSync(layoutPath, "utf8");
const pageSource = readFileSync(pagePath, "utf8");
const i18nSource = readFileSync(i18nPath, "utf8");

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

assert(
  layoutSource.includes("t('nav.runtime')") || layoutSource.includes('t("nav.runtime")'),
  "Admin sidebar must label the runtime pod page through nav.runtime.",
);
assert(
  !layoutSource.includes("label: 'Runtime Pods'") && !layoutSource.includes('label: "Runtime Pods"'),
  "Admin sidebar must not hard-code Runtime Pods.",
);
assert(
  pageSource.includes("t('runtimePods.title')") || pageSource.includes('t("runtimePods.title")'),
  "Runtime pods page title must use runtimePods.title.",
);
assert(
  pageSource.includes("selectRuntimePod(pod)") &&
    pageSource.includes('role="button"') &&
    /onKeyDown=\{\(event\) =>\s*handleRuntimePodCardKeyDown\(event, pod\)\s*\}/.test(pageSource),
  "Runtime pod cards must be selectable by clicking or using the keyboard.",
);
assert(
  pageSource.includes("event.stopPropagation()"),
  "Runtime pod card action buttons must not trigger card selection accidentally.",
);
assert(
  i18nSource.includes('runtime: "Runtime"') &&
    i18nSource.includes('runtime: "运行时"') &&
    i18nSource.includes('title: "运行时"'),
  "Runtime page translations must include English and Chinese runtime labels.",
);

console.log("Runtime pods page source contract is valid.");

assert(pageSource.includes("page_size: 16"), "Runtime inventory must request sixteen cards per page.");
const css = readFileSync(path.resolve(scriptDir, "../src/styles/runtime-pods.css"), "utf8");
assert(/grid-template-columns: repeat\(4, minmax\(0, 1fr\)\)/.test(css) && /grid-template-rows: repeat\(4, minmax\(0, 1fr\)\)/.test(css), "Desktop inventory must use four rows and four columns.");
