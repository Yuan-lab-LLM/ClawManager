import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

const source = fs.readFileSync(
  path.resolve("src/pages/teams/CreateTeamPage.tsx"),
  "utf8",
);

for (const marker of [
  'const LEADER_RUNTIME_TYPE: RuntimeType = "openclaw"',
  'teamRuntimeOptions(false).map',
  'runtime_type: runtimeType',
  'image_registry: imageForRuntime(runtimeType, member.instanceMode)',
  'const runtimeType = member.runtimeType',
  'OpenClaw Lite',
  'runtimeType === "opencode" ? openCodeLiteImage',
  'runtimeType === "deepseek-harness" ? dshLiteImage',
]) {
  assert.ok(source.includes(marker), `CreateTeamPage missing ${marker}`);
}

assert.ok(
  source.includes('runtimeType === "hermes" ? hermesLiteImage : runtimeType === "opencode" ? openCodeLiteImage : runtimeType === "deepseek-harness" ? dshLiteImage : openClawLiteImage'),
  "Each Lite Worker runtime must resolve its own configured image.",
);
assert.ok(
  source.includes('if (member.id !== memberDraftId || (member.isLeader && runtimeType !== "openclaw"))'),
  "The runtime selector must never mutate the fixed OpenClaw Leader.",
);
assert.ok(
  source.includes('disabled={!imageForRuntime(type)}'),
  "Each Worker selection must be disabled when its Lite image is unavailable.",
);
assert.ok(
  !source.includes("TEAM_WORKER_RUNTIME_TYPE"),
  "Submission must preserve each Worker's selected Lite runtime.",
);

console.log("create Team mixed Lite Worker runtime contract passed");
