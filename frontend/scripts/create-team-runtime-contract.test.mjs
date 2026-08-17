import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

const source = fs.readFileSync(
  path.resolve("src/pages/teams/CreateTeamPage.tsx"),
  "utf8",
);

for (const marker of [
  'const LEADER_RUNTIME_TYPE: RuntimeType = "openclaw"',
  '(["openclaw", "hermes"] as RuntimeType[]).map',
  'runtime_type: runtimeType',
  'image_registry: imageForRuntime(runtimeType)',
  'member.isLeader ? LEADER_RUNTIME_TYPE : member.runtimeType',
  'OpenClaw Lite',
  'Hermes Lite',
]) {
  assert.ok(source.includes(marker), `CreateTeamPage missing ${marker}`);
}

assert.ok(
  source.includes('runtimeType === "hermes" ? hermesLiteImage : openClawLiteImage'),
  "Each Lite Worker runtime must resolve its own configured image.",
);
assert.ok(
  source.includes('if (member.id !== memberDraftId || member.isLeader)'),
  "The runtime selector must never mutate the fixed OpenClaw Leader.",
);
assert.ok(
  source.includes('runtimeType === "hermes" && !hermesLiteImage'),
  "Hermes selection must be disabled cleanly when no Hermes Lite image is configured.",
);
assert.ok(
  !source.includes("TEAM_WORKER_RUNTIME_TYPE"),
  "Submission must preserve each Worker's selected Lite runtime.",
);

console.log("create Team mixed Lite Worker runtime contract passed");
