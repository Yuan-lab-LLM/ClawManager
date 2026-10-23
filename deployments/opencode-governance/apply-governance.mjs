import fs from "node:fs"
import path from "node:path"
import crypto from "node:crypto"
import { execFileSync } from "node:child_process"
import { fileURLToPath } from "node:url"

export const revision = "1f4d07a4a519185955ae8ab2ea3607805509e386"
export const version = "2.0.27"
const here = path.dirname(fileURLToPath(import.meta.url))
const root = path.resolve(process.argv[2] ?? ".")
const checkOnly = process.argv.includes("--check")
const refreshPatches = process.argv.includes("--refresh-patches")
const hash = (value) => crypto.createHash("sha256").update(value).digest("hex")
const helper = fs.readFileSync(path.join(here, "governance.ts"), "utf8").replaceAll("\r\n", "\n")
const tests = fs.readFileSync(path.join(here, "governance.test.ts"), "utf8").replaceAll("\r\n", "\n")
const imageTools = fs.readFileSync(path.join(here, "image-tools.ts"), "utf8").replaceAll("\r\n", "\n")
const imageTests = fs.readFileSync(path.join(here, "image-tools.test.ts"), "utf8").replaceAll("\r\n", "\n")
const core = path.join(root, "packages/core/src")
const manifestPath = path.join(root, ".clawmanager-governance-manifest.json")
const verifiedFiles = new Set()
if (JSON.parse(fs.readFileSync(path.join(root, "packages/core/package.json"))).version !== version ||
    execFileSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).trim() !== revision) {
  throw new Error(`Only official OpenCode ${version} (${revision}) is supported; refusing to patch this source`)
}
if (fs.existsSync(manifestPath)) {
  const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf8"))
  if ((manifest.imageTools !== hash(imageTools) || manifest.imageTests !== hash(imageTests)) && !refreshPatches)
    throw new Error("Image tool adapter changed; use --refresh-patches on verified source or a fresh pinned checkout")
  if (manifest.revision !== revision || (manifest.helper !== hash(helper) && !process.argv.includes("--refresh-helper") && !refreshPatches)) throw new Error("Adapter changed; use a fresh pinned source checkout or explicitly refresh verified helper files")
  for (const [relative, expected] of Object.entries(manifest.files)) {
    if (hash(fs.readFileSync(path.join(root, relative), "utf8").replaceAll("\r\n", "\n")) !== expected)
      throw new Error(`Patched source modified: ${relative}`)
    verifiedFiles.add(relative)
  }
  if (process.argv.includes("--refresh-helper") && !checkOnly && !refreshPatches) {
    for (const [relative, contents] of [["packages/core/src/clawmanager-governance.ts", helper], ["packages/core/test/clawmanager-governance.test.ts", tests]]) {
      const file = path.join(root, relative)
      if (fs.existsSync(file) && !(relative in manifest.files)) throw new Error(`Refusing to overwrite untracked helper file: ${relative}`)
      fs.writeFileSync(file, contents)
      manifest.files[relative] = hash(contents)
    }
    manifest.helper = hash(helper)
    fs.writeFileSync(manifestPath, JSON.stringify(manifest, null, 2) + "\n")
  }
  if (!refreshPatches) {
    console.log("Verified previously applied OpenCode 2.0.27 adapter")
    process.exit(0)
  }
}

function patch(relative, edits) {
  const file = path.join(core, relative)
  const original = fs.readFileSync(file, "utf8").replaceAll("\r\n", "\n")
  const upstream = execFileSync("git", ["show", `${revision}:packages/core/src/${relative}`], { cwd: root, encoding: "utf8" }).replaceAll("\r\n", "\n")
  if (original !== upstream && !(refreshPatches && verifiedFiles.has(`packages/core/src/${relative}`))) throw new Error(`Source differs from pinned upstream: ${relative}`)
  let text = upstream
  for (const [anchor, replacement] of edits) {
    if (text.split(anchor).length !== 2) throw new Error(`Unsupported source anchor: ${relative}`)
    text = text.replace(anchor, replacement)
  }
  return [file, "// ClawManager governance, pinned OpenCode 2.0.27; no UI patch.\n" + text]
}

// Validate every file before any mutation. No changes to SQLite schema or UI.
const files = [
  patch("plugin/internal.ts", [
    ['import { WebSearchTool }', 'import { ClawManagerImageTools } from "../clawmanager-image-tools.js"\nimport { WebSearchTool }'],
    ['  WebSearchTool.Plugin,', '  WebSearchTool.Plugin,\n  ClawManagerImageTools.Plugin,'],
  ]),
  patch("session/runner/llm.ts", [
    ['import { Message }', 'import { Governance } from "../../clawmanager-governance.js"\nimport { Message }'],
    ['const runStep = Effect.fn("SessionRunner.runStep")', 'const runStepImpl = Effect.fn("SessionRunner.runStep")'],
    ['    const settleStaleCompactions =', `    const runStep = (first: SessionContext.Loaded, step: number) =>
      Governance.withPrimary(first.session.id, first.model.ref.providerID, first.messages, runStepImpl(first, step))

    const settleStaleCompactions =`],
    ['    return Service.of({ drain })', '    return Service.of({ drain: (input) => Governance.guardSession(input.sessionID, drain(input)) })'],
  ]),
  patch("session/model-request.ts", [
    ['import {\n  GenerationOptions,', 'import { Governance } from "../clawmanager-governance.js"\nimport {\n  GenerationOptions,'],
    ['      const request = LLMRequest.update(base, {', '      const governanceHeaders = model.ref.providerID === "clawmanager" ? yield* Governance.headers : {}\n      const request = LLMRequest.update(base, {'],
    ['          headers: Object.keys(modelHook.headers).length === 0 ? undefined : modelHook.headers,', '          headers: { ...modelHook.headers, ...governanceHeaders },'],
    ['          tools.execute({ ...call, definitions: hooked }).pipe(', '          Governance.beforeTool(model.ref.providerID, tools.execute({ ...call, definitions: hooked })).pipe('],
  ]),
  patch("session/execution.ts", [
    ['import { Cause,', 'import { Governance } from "../clawmanager-governance.js"\nimport { Cause,'],
    ['import { SessionInbox } from "./inbox.js"', 'import { SessionInbox } from "./inbox.js"\nimport { SessionInboxTable } from "./sql.js"\nimport { count } from "drizzle-orm"'],
    ['    return Service.of({', `    if (Governance.enabled) yield* Effect.forkScoped(Effect.forever(
      Effect.gen(function* () {
        const active = yield* coordinator.active
        const rows = yield* db.select({ total: count() }).from(SessionInboxTable)
        yield* Effect.promise(() => Governance.pollMaintenance(active.size, rows[0]?.total ?? 0))
      }).pipe(Effect.catchCause(() => Effect.void), Effect.andThen(Effect.sleep("1 second")))
    ))
    return Service.of({`],
    ['              yield* bus.publish(SessionEvent.Execution.Succeeded, { sessionID }, releaseOnCommit(sessionID))', '              yield* bus.publish(SessionEvent.Execution.Succeeded, { sessionID }, releaseOnCommit(sessionID))\n              yield* Effect.promise(() => Governance.settleSession(sessionID))'],
    ['                outcome.reason === "shutdown" ? undefined : releaseOnCommit(sessionID),\n              )', '                outcome.reason === "shutdown" ? undefined : releaseOnCommit(sessionID),\n              )\n              if (outcome.reason !== "shutdown") yield* Effect.promise(() => Governance.settleSession(sessionID))'],
    ['            yield* bus.publish(\n              SessionEvent.Execution.Failed,', '            if (Governance.hasPrimaryTask(sessionID)) yield* jobs.cancel(sessionID)\n            yield* bus.publish(\n              SessionEvent.Execution.Failed,'],
    ['              releaseOnCommit(sessionID),\n            )\n          }),', '              releaseOnCommit(sessionID),\n            )\n            yield* Effect.promise(() => Governance.settleSession(sessionID))\n          }),'],
  ]),
  patch("session/title.ts", [
    ['import { isDeepStrictEqual }', 'import { Governance } from "../clawmanager-governance.js"\nimport { isDeepStrictEqual }'],
    ['    const attempt = Effect.fn("SessionTitle.attempt")', '    const attemptImpl = Effect.fn("SessionTitle.attempt")'],
    ['    const generate = Effect.fn("SessionTitle.generate")', `    const attempt = (input: Parameters<typeof attemptImpl>[0]) =>
      Governance.withAuxiliary(input.session.id, input.model.ref.providerID, "title", attemptImpl(input))

    const generate = Effect.fn("SessionTitle.generate")`],
  ]),
  patch("session/generate.ts", [
    ['import type { FileSystem }', 'import { Governance } from "../clawmanager-governance.js"\nimport type { FileSystem }'],
    ['    const prepared = yield* context.request.generate({', '    return yield* Governance.withAuxiliary(input.session.id, model.ref.providerID, "generate", Effect.gen(function* () {\n    const prepared = yield* context.request.generate({'],
    ['    return response.text\n', '    return response.text\n    }))\n'],
  ]),
  patch("generate.ts", [
    ['import { LLM,', 'import { Governance } from "./clawmanager-governance.js"\nimport { LLM,'],
    ['      const response = yield* llm', `      const sessionID = SessionID.create()
      return yield* Governance.admit(Governance.withAuxiliary(sessionID, resolved.ref.providerID, "stateless", Effect.gen(function* () {
      const governanceHeaders = resolved.ref.providerID === "clawmanager" ? yield* Governance.headers : {}
      const response = yield* llm`],
    ['            http: { headers: { "x-opencode-session": SessionID.create() } },', '            http: { headers: { "x-opencode-session": sessionID, ...governanceHeaders } },'],
    ['      return response.text\n', '      return response.text\n      })))\n'],
  ]),
  patch("session.ts", [
    ['import { Effect,', 'import { Governance } from "./clawmanager-governance.js"\nimport { Effect,'],
    ['    return result\n', `    return {
      ...result,
      prompt: (input: Parameters<typeof result.prompt>[0]) => Governance.admit(result.prompt(input)),
      command: (input: Parameters<typeof result.command>[0]) => Governance.admit(result.command(input)),
      shell: (input: Parameters<typeof result.shell>[0]) => Governance.admit(result.shell(input)),
      skill: (input: Parameters<typeof result.skill>[0]) => Governance.admit(result.skill(input)),
      switchAgent: (input: Parameters<typeof result.switchAgent>[0]) => Governance.admit(result.switchAgent(input)),
      switchModel: (input: Parameters<typeof result.switchModel>[0]) => Governance.admit(result.switchModel(input)),
      compact: (input: Parameters<typeof result.compact>[0]) => Governance.admit(result.compact(input)),
      generate: (input: Parameters<typeof result.generate>[0]) => Governance.admit(result.generate(input)),
      resume: (input: Parameters<typeof result.resume>[0]) => Governance.admit(result.resume(input)),
      synthetic: (input: Parameters<typeof result.synthetic>[0]) => Governance.admit(result.synthetic(input)),
      background: (input: Parameters<typeof result.background>[0]) => Governance.admit(result.background(input)),
      move: (input: Parameters<typeof result.move>[0]) => Governance.admit(result.move(input)),
    }
`],
  ]),
  patch("job.ts", [
    ['import { Array, Cause,', 'import { Governance } from "./clawmanager-governance.js"\nimport { Array, Cause,'],
    ['            const scope = yield* Scope.fork(state.scope, "parallel")', '            const scope = yield* Scope.fork(state.scope, "parallel")\n            yield* Scope.addFinalizer(scope, Governance.jobStarted())'],
  ]),
  patch("session/compaction.ts", [
    ['import {\n  AIError,', 'import { Governance } from "../clawmanager-governance.js"\nimport {\n  AIError,'],
    ['        compact(trigger).pipe(', `        Governance.withAuxiliary(trigger.context.session.id, trigger.context.model.ref.providerID, "compaction",
          compact(trigger), trigger.reason === "manual" ? trigger.inputID : undefined).pipe(`],
  ]),
  patch("model.ts", [
    ['import { Model }', 'import { Governance } from "./clawmanager-governance.js"\nimport { Model }'],
    ['    return Service.of({\n      transform:', `    const projected = () => read().pipe(Effect.flatMap((value) => Effect.promise(() => Governance.projectCatalog(value.all))))
    if (Governance.enabled) yield* Effect.forkScoped(Effect.gen(function* () {
      let previous = ""
      while (true) {
        const current = yield* Effect.promise(() => Governance.catalog().then((value) => JSON.stringify(value)).catch(() => "unavailable"))
        if (current !== previous) {
          previous = current
          yield* bus.publish(Model.Event.Updated, {}, { location: { directory: location.directory, workspaceID: location.workspaceID } })
        }
        yield* Effect.sleep("3 seconds")
      }
    }))
    return Service.of({
      transform:`],
    ['        read().pipe(Effect.map((value) => value.byProvider.get(providerID)?.get(modelID))),', '        projected().pipe(Effect.map((value) => value.find((item) => item.providerID === providerID && item.id === modelID))),'],
    ['      all: Effect.fn("Model.all")(() => read().pipe(Effect.map((value) => value.all))),', '      all: Effect.fn("Model.all")(projected),'],
    ['      available: Effect.fn("Model.available")(() => read().pipe(Effect.map((value) => value.available))),', '      available: Effect.fn("Model.available")(() => projected().pipe(Effect.map((value) => value.filter((item) => item.enabled)))),'],
    ['        const model = requested && value.byProvider.get(requested.providerID)?.get(requested.modelID)\n        return model?.enabled ? model : value.available.find(supportsText)', '        const available = (yield* projected()).filter((item) => item.enabled)\n        const model = requested && available.find((item) => item.providerID === requested.providerID && item.id === requested.modelID)\n        return model ?? available.find(supportsText)'],
    ['        const models = value.available.filter(', '        const models = (yield* projected()).filter((item) => item.enabled).filter('],
    ['        return ["gpt-luna",', '        if (Governance.enabled && providerID === "clawmanager") return models.find((item) => item.id === "auto")\n        return ["gpt-luna",'],
    ['      small: Effect.fn("Model.small")(function* (providerID) {\n        const value = yield* read()', '      small: Effect.fn("Model.small")(function* (providerID) {'],
  ]),
  [path.join(core, "clawmanager-governance.ts"), helper],
  [path.join(root, "packages/core/test/clawmanager-governance.test.ts"), tests],
  [path.join(core, "clawmanager-image-tools.ts"), imageTools],
  [path.join(root, "packages/core/test/clawmanager-image-tools.test.ts"), imageTests],
]
for (const [file] of files.slice(-4)) {
  const relative = path.relative(root, file).replaceAll("\\", "/")
  if (fs.existsSync(file) && !verifiedFiles.has(relative)) throw new Error(`Refusing to overwrite untracked helper: ${relative}`)
}
if (checkOnly) { console.log(`Validated ${files.length} files against official ${version}; no files changed`); process.exit(0) }
const manifest = { revision, helper: hash(helper), imageTools: hash(imageTools), imageTests: hash(imageTests), files: {} }
for (const [file, contents] of files) {
  fs.writeFileSync(file, contents)
  manifest.files[path.relative(root, file).replaceAll("\\", "/")] = hash(contents)
}
fs.writeFileSync(manifestPath, JSON.stringify(manifest, null, 2) + "\n")
console.log("Applied and recorded pinned OpenCode 2.0.27 governance adapter; UI unchanged")
