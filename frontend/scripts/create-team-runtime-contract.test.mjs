import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

const source = fs.readFileSync(
  path.resolve("src/pages/teams/CreateTeamPage.tsx"),
  "utf8",
);

for (const marker of [
  'const LEADER_RUNTIME_TYPE: RuntimeType = "openclaw"',
  'const TEAM_WORKER_RUNTIME_TYPE: RuntimeType = "openclaw"',
  "runtime_type: runtimeType",
  "image_registry: imageForRuntime(runtimeType)",
  "OpenClaw Lite",
]) {
  assert.ok(source.includes(marker), `CreateTeamPage missing ${marker}`);
}

assert.ok(
  source.includes("? LEADER_RUNTIME_TYPE\n          : TEAM_WORKER_RUNTIME_TYPE"),
  "Every new Team Worker must use the temporarily supported OpenClaw Lite runtime.",
);
assert.ok(
  !source.includes('(["openclaw", "hermes"] as RuntimeType[]).map'),
  "Team creation must not expose the Hermes Lite Worker selector.",
);
assert.ok(
  !source.includes("selectedHermesImage") && !source.includes("hermesLiteImage"),
  "Team creation must not render Hermes Lite image controls or summaries.",
);

console.log("create Team OpenClaw-only runtime contract passed");
