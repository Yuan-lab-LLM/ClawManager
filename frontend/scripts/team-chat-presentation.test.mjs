import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import ts from "typescript";

const pagePath = path.resolve(import.meta.dirname, "../src/pages/teams/TeamDetailPage.tsx");
const source = fs.readFileSync(pagePath, "utf8");
const sourceFile = ts.createSourceFile(
  pagePath,
  source,
  ts.ScriptTarget.Latest,
  true,
  ts.ScriptKind.TSX,
);

const requiredFunctions = new Set([
  "workspaceLinkToRelativePath",
  "mergeTeamChatArtifactRefs",
  "dedupeTeamChatMessages",
  "normalizeTeamChatVisibleControlText",
]);
const declarations = [];
for (const statement of sourceFile.statements) {
  if (ts.isFunctionDeclaration(statement) && statement.name && requiredFunctions.has(statement.name.text)) {
    declarations.push(`export ${statement.getText(sourceFile)}`);
  }
}
assert.equal(declarations.length, requiredFunctions.size, "chat presentation helpers must remain testable");

const compiled = ts.transpileModule(declarations.join("\n"), {
  compilerOptions: {
    module: ts.ModuleKind.CommonJS,
    target: ts.ScriptTarget.ES2022,
  },
}).outputText;
const module = { exports: {} };
new Function("module", "exports", compiled)(module, module.exports);

const { dedupeTeamChatMessages, mergeTeamChatArtifactRefs, normalizeTeamChatVisibleControlText } = module.exports;
const body = "P1 implementation delivered.";
const messages = dedupeTeamChatMessages([
  {
    id: "event-641",
    kind: "member",
    sender: "developer",
    senderKey: "developer",
    content: body,
    time: 1,
    dedupeKey: "feedback:narrative-hash",
    presentationKey: "turn:35:developer:dev-kanban:worker-turn-1:body-hash",
  },
  {
    id: "event-642",
    kind: "member",
    sender: "developer",
    senderKey: "developer",
    content: body,
    time: 2,
    tone: "feedback",
    dedupeKey: "feedback:completion-hash",
    presentationKey: "turn:35:developer:dev-kanban:worker-turn-1:body-hash",
    artifactRefs: [
      "/team/artifacts/team-16-task-35/members/developer/dev-kanban/kanban.html",
      "/team/work/team-16-task-35/kanban.html",
    ],
  },
]);
assert.equal(messages.length, 1, "same-turn narrative and completion must render as one bubble");
assert.equal(messages[0].tone, "feedback");
assert.deepEqual(messages[0].artifactRefs, [
  "/team/artifacts/team-16-task-35/members/developer/dev-kanban/kanban.html",
  "/team/work/team-16-task-35/kanban.html",
]);

const distinctTurns = dedupeTeamChatMessages([
  { ...messages[0], id: "turn-a", presentationKey: "turn-a", dedupeKey: "feedback:turn-a" },
  { ...messages[0], id: "turn-b", presentationKey: "turn-b", dedupeKey: "feedback:turn-b" },
]);
assert.equal(distinctTurns.length, 2, "identical prose from different source turns must remain visible");

assert.equal(
  normalizeTeamChatVisibleControlText(`${body}\n\nNO_REPLY`, { eventKind: "agent_narrative" }),
  body,
  "historical assistant-session control tokens must not create a second presentation identity",
);
assert.equal(
  normalizeTeamChatVisibleControlText("NO_REPLY", { eventKind: "agent_narrative" }),
  "",
  "a token-only narrative is not user-visible",
);
assert.equal(
  normalizeTeamChatVisibleControlText("OpenClaw uses NO_REPLY as its silent token.", { eventKind: "agent_narrative" }),
  "OpenClaw uses NO_REPLY as its silent token.",
  "ordinary prose about the token must be preserved",
);

assert.deepEqual(
  mergeTeamChatArtifactRefs(
    ["/team/work/team-16-task-35/kanban.html"],
    ["/workspaces/teams/user-1/team-16-shared/work/team-16-task-35/kanban.html"],
  ),
  ["/team/work/team-16-task-35/kanban.html"],
  "canonical and pooled physical paths must collapse to one attachment",
);

assert.match(source, /artifactRefs\.slice\(0, 5\)/, "collapsed chat bubbles must show at most five file links");
assert.match(source, /展开其余 \$\{hiddenArtifactCount\} 个文件/, "extra file links must be explicitly expandable");
assert.match(source, /aria-expanded=\{artifactsExpanded\}/, "file expansion control must expose accessibility state");
assert.match(
  source,
  /queryAnchors\.length\s*>=\s*2/,
  "question navigation must appear as soon as a second user question exists",
);
assert.doesNotMatch(
  source,
  /setDispatchError\(null\);\s*setSelectedGroupKey\(null\);\s*(?:const\s+\w+\s*=\s*)?await teamService\.dispatchTask/s,
  "a failed dispatch must not discard the user's current historical selection",
);
assert.match(
  source,
  /const dispatchedTask = await teamService\.dispatchTask[\s\S]*setLoadedTasks\(\(current\) => mergeTasksByLatestState\(current, \[dispatchedTask\]\)\);[\s\S]*setSelectedGroupKey\(canonicalTaskKey\(dispatchedTask\.id\)\);/,
  "a successful local dispatch must immediately select the newly created task",
);
assert.doesNotMatch(
  source,
  /server-smoke/,
  "the Kanban must not retain the old fixed smoke-test title",
);
assert.match(
  source,
  /payload:\s*\{\s*title:\s*taskPrompt\.trim\(\),\s*prompt:\s*taskPrompt\.trim\(\)/,
  "new tasks must use the submitted query instead of a fixed title",
);
assert.match(
  source,
  /title=\{queryText \|\| undefined\}[\s\S]*\{queryText \|\| "用户提交 query 后，这里会展示拆解、执行和汇总。"\}/,
  "the Kanban header must present the current query as its primary text",
);
assert.match(
  source,
  /case "long":\s*return 1110;[\s\S]*case "medium":\s*return 945;[\s\S]*default:\s*return 740;/,
  "the shared chat and Kanban height must reduce empty space without clipping the detail panel",
);

process.stdout.write("Team chat presentation contract test passed\n");
