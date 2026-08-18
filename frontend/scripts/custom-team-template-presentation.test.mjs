import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

const customPagePath = path.resolve(
  import.meta.dirname,
  "../src/pages/teams/CustomTeamTemplatesPage.tsx",
);
const createPagePath = path.resolve(
  import.meta.dirname,
  "../src/pages/teams/CreateTeamPage.tsx",
);
const customSource = fs.readFileSync(customPagePath, "utf8");
const createSource = fs.readFileSync(createPagePath, "utf8");

assert.match(
  customSource,
  /用自然语言调整 Leader 的延展职责/,
  "Leader must expose the same natural-language adjustment workflow",
);
assert.match(
  customSource,
  /固定 Leader 基座、成员协调关系和创建后的全员介绍流程不会被修改/,
  "Leader adjustment must explain the immutable collaboration base",
);
assert.match(
  customSource,
  /member\.isLeader[\s\S]*应用 Leader 职责调整/,
  "Leader adjustment must have an explicit action label",
);
assert.doesNotMatch(
  customSource,
  /重新生成 Worker|regenerateMember\(/,
  "individual member regeneration must not remain on the custom Team page",
);
assert.match(
  customSource,
  /重新生成整个 Team/,
  "whole-Team regeneration must remain available",
);
assert.match(
  customSource,
  /if \(!instruction\)[\s\S]*setAdjustmentErrors[\s\S]*请先输入希望如何调整/,
  "an empty member adjustment must produce visible field feedback",
);
assert.match(
  customSource,
  /role="alert"/,
  "empty adjustment feedback must be announced accessibly",
);
assert.doesNotMatch(
  createSource,
  /OpenClaw Leader · Lite Worker 可选/,
  "the removed runtime hint must not remain beside the custom Team action",
);
assert.match(
  createSource,
  /<Link\s+to="\/teams\/custom-templates"[\s\S]*?\+ 自定义 Team[\s\S]*?<\/Link>/,
  "the custom Team button must remain the standalone right-side action",
);

process.stdout.write("Custom Team template presentation contract test passed\n");
