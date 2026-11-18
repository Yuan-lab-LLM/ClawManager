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
// Entry keyboard behavior, action isolation and bounded capacity layout run in
// e2e/tests/uiux/management-ui.spec.ts against the rendered native table.
assert(
  i18nSource.includes('runtime: "Runtime"') &&
    i18nSource.includes('runtime: "运行时"') &&
    i18nSource.includes('title: "运行时"'),
  "Runtime page translations must include English and Chinese runtime labels.",
);

assert(pageSource.includes("page_size: 16"), "Runtime inventory must request sixteen Runtime rows per page.");

console.log("Runtime pods page source contract is valid.");
