import { afterAll, expect, test } from "bun:test"
import { Deferred, Effect, Fiber } from "effect"
import type { Prepared } from "../src/session/model-request.js"

const calls: { path: string; body: Record<string, unknown> }[] = []
const revoked = new Set<string>()
let catalogFails = false
let maintenanceFails = false
let maintenance = { operation_id: "", draining: false, generation: 4 }
const server = Bun.serve({
  hostname: "127.0.0.1", port: 0,
  async fetch(request) {
    const path = new URL(request.url).pathname
    const body = (request.method === "POST" ? await request.json() : {}) as Record<string, unknown>
    calls.push({ path, body })
    if (request.headers.get("authorization") !== "Bearer test-key") return new Response(null, { status: 401 })
    if (path.endsWith("capabilities")) return Response.json({ protocol: 1, generation: 4, task_identity_required: true })
    if (path.endsWith("maintenance")) {
      if (maintenanceFails) return new Response(null, { status: 503 })
      return Response.json(maintenance)
    }
    if (path.endsWith("catalog")) {
      if (catalogFails) return new Response(null, { status: 503 })
      return Response.json({ models: [{ id: "auto", context_window: 8192, tools: true, reasoning: false, vision: true }] })
    }
    if (path.endsWith("check") && revoked.has(String(body.task_id))) return new Response(null, { status: 403 })
    return Response.json({ revoked: false, finished: false, runtime_acknowledged: false })
  },
})
const keys = ["CLAWMANAGER_MODEL_GOVERNANCE_PROTOCOL", "CLAWMANAGER_LLM_BASE_URL", "CLAWMANAGER_LLM_API_KEY", "CLAWMANAGER_GATEWAY_GENERATION"] as const
const old = keys.map((key) => process.env[key])
process.env.CLAWMANAGER_MODEL_GOVERNANCE_PROTOCOL = "1"
process.env.CLAWMANAGER_LLM_BASE_URL = `http://127.0.0.1:${server.port}`
process.env.CLAWMANAGER_LLM_API_KEY = "test-key"
process.env.CLAWMANAGER_GATEWAY_GENERATION = "4"
// Run in a dedicated Bun test process so the adapter's environment is pinned.
const { Governance, GovernanceError, withPrimary, withAuxiliary, headers, settleSession, projectCatalog } = await import("../src/clawmanager-governance.js")
afterAll(() => { server.stop(true); keys.forEach((key, i) => { if (old[i] === undefined) delete process.env[key]; else process.env[key] = old[i] }) })

test("maintenance blocks new admissions, permits accepted work and reports it until cleanup", async () => {
  await Governance.pollMaintenance(0, 0)
  await Effect.runPromise(Effect.scoped(Effect.gen(function* () {
    const entered = yield* Deferred.make<void>()
    const release = yield* Deferred.make<void>()
    const running = yield* Effect.forkScoped(Governance.admit(
      Deferred.succeed(entered, undefined).pipe(Effect.andThen(Deferred.await(release)),
        Effect.andThen(Governance.admit(Effect.succeed("continued")))),
    ))
    yield* Deferred.await(entered)
    maintenance = { operation_id: "a".repeat(32), draining: true, generation: 4 }
    yield* Effect.promise(() => Governance.pollMaintenance(1, 2))
    yield* Effect.promise(() => Governance.pollMaintenance(1, 2))
    const report = calls.findLast((call) => call.path.endsWith("maintenance"))!.body
    expect(report.gate_closed).toBe(true)
    expect(report.active_admissions).toBe(1)
    expect(report.queued_inputs).toBe(2)
    expect((yield* Effect.exit(Governance.admit(Effect.void)))._tag).toBe("Failure")
    yield* Deferred.succeed(release, undefined)
    expect(yield* Fiber.join(running)).toBe("continued")
  })))
  await Governance.pollMaintenance(0, 0)
  expect(calls.findLast((call) => call.path.endsWith("maintenance"))!.body.active_admissions).toBe(0)
  maintenance = { ...maintenance, draining: false }
  await Governance.pollMaintenance(0, 0)
  expect(await Effect.runPromise(Governance.admit(Effect.succeed("open")))).toBe("open")
})

test("maintenance failure never reopens a closed gate; background scopes remain counted", async () => {
  maintenance = { operation_id: "b".repeat(32), draining: true, generation: 4 }
  const finish = Governance.jobStarted()
  await Governance.pollMaintenance(0, 0)
  expect(calls.findLast((call) => call.path.endsWith("maintenance"))!.body.active_jobs).toBe(1)
  maintenanceFails = true
  await expect(Governance.pollMaintenance(0, 0)).rejects.toBeInstanceOf(GovernanceError)
  expect((await Effect.runPromiseExit(Governance.admit(Effect.void)))._tag).toBe("Failure")
  maintenanceFails = false
  await Effect.runPromise(finish)
  maintenance = { ...maintenance, draining: false }
  await Governance.pollMaintenance(0, 0)
  expect(calls.findLast((call) => call.path.endsWith("maintenance"))!.body.active_jobs).toBe(0)
})

test("primary retries and post-compaction calls retain the exact user-task identity", async () => {
  const first = await Effect.runPromise(withPrimary("s1", "clawmanager", [{ type: "user", id: "msg_one" }], headers))
  const next = await Effect.runPromise(withPrimary("s1", "clawmanager", [], headers))
  expect(first).toEqual(next)
  expect(first["X-ClawManager-Task-ID"]).toBe("msg_one")
  expect(first["X-ClawManager-Runtime-Generation"]).toBe("4")
  expect(calls.find((call) => call.path.endsWith("capabilities"))?.body.runtime_version).toBe("2.0.27")
  await settleSession("s1")
})

test("no anonymous task can bypass enrollment", async () => {
  const exit = await Effect.runPromiseExit(headers)
  expect(exit._tag).toBe("Failure")
  const missing = await Effect.runPromiseExit(withPrimary("missing", "clawmanager", [], Effect.void))
  expect(missing._tag).toBe("Failure")
})

test("auxiliary requests do not replace a primary identity; compaction inherits it", async () => {
  const first = await Effect.runPromise(withPrimary("s2", "clawmanager", [{ type: "user", id: "msg_two" }],
    withAuxiliary("s2", "clawmanager", "compaction", headers)))
  expect(first["X-ClawManager-Task-ID"]).toBe("msg_two")
  const title = await Effect.runPromise(withAuxiliary("s2", "clawmanager", "title", headers))
  expect(title["X-ClawManager-Task-ID"]).toStartWith("title:")
  const next = await Effect.runPromise(withPrimary("s2", "clawmanager", [], headers))
  expect(next["X-ClawManager-Task-ID"]).toBe("msg_two")
  await settleSession("s2")
})

test("revocation joins tool cleanup before a cancellation receipt", async () => {
  let cleaned = false
  const operation = Effect.sync(() => { revoked.add("msg_cancel") }).pipe(
    Effect.andThen(Effect.never),
    Effect.ensuring(Effect.sleep("20 millis").pipe(Effect.andThen(Effect.sync(() => { cleaned = true })))),
  )
  const exit = await Effect.runPromiseExit(withPrimary("cancel", "clawmanager", [{ type: "user", id: "msg_cancel" }], operation))
  expect(exit._tag).toBe("Failure")
  expect(cleaned).toBe(true)
  expect(calls.some((call) => call.path.endsWith("finish") && call.body.task_id === "msg_cancel")).toBe(false)
  await settleSession("cancel")
  expect(calls.find((call) => call.path.endsWith("finish") && call.body.task_id === "msg_cancel")?.body.cancelled).toBe(true)
})

test("unmanaged provider does not acquire a managed task", async () => {
  const count = calls.length
  expect(await Effect.runPromise(withPrimary("other", "custom", [], Effect.succeed(42)))).toBe(42)
  expect(calls.length).toBe(count)
})

test("authorization is checked again before starting a tool", async () => {
  let invoked = false
  const operation = Effect.sync(() => { revoked.add("msg_tool_gate") }).pipe(Effect.andThen(
    Governance.beforeTool("clawmanager", Effect.sync(() => { invoked = true })),
  ))
  expect((await Effect.runPromiseExit(withPrimary("tool_gate", "clawmanager", [{ type: "user", id: "msg_tool_gate" }], operation)))._tag).toBe("Failure")
  expect(invoked).toBe(false)
  await settleSession("tool_gate")
})

test("revoked task cannot resume within the execution after regrant", async () => {
  revoked.add("msg_sticky")
  expect((await Effect.runPromiseExit(withPrimary("sticky", "clawmanager", [{ type: "user", id: "msg_sticky" }], Effect.void)))._tag).toBe("Failure")
  revoked.delete("msg_sticky")
  expect((await Effect.runPromiseExit(withPrimary("sticky", "clawmanager", [{ type: "user", id: "msg_sticky" }], Effect.void)))._tag).toBe("Failure")
  await settleSession("sticky")
})

test("catalog vision enables image input without changing unmanaged models or output", async () => {
  const managed = { id: "auto", providerID: "clawmanager", capabilities: { tools: true, input: ["text"], output: ["text"] }, limit: { context: 8192 } } as never
  const unmanaged = { id: "custom", providerID: "custom" } as never
  const projected = await projectCatalog([managed, unmanaged])
  expect(projected[0]).toBe(unmanaged)
  expect(projected[1].capabilities.input).toEqual(["text", "image"])
  expect(projected[1].capabilities.output).toEqual(["text"])
})

test("catalog refresh rejects stale grants on failure", async () => {
  const entries = await Governance.catalog()
  expect(entries[0].id).toBe("auto")
  catalogFails = true
  await Bun.sleep(3100)
  // An empty original provider catalog cannot synthesize unauthorized templates.
  expect(await projectCatalog([])).toEqual([])
  await expect(Governance.catalog()).rejects.toBeInstanceOf(GovernanceError)
  catalogFails = false
})

test("native request preparation attaches identity to primary, compaction, title and generate", async () => {
  const { SessionModelRequest } = await import("../src/session/model-request.js")
  const { SessionModelTransport } = await import("../src/session/model-transport.js")
  const { SessionRunnerModel } = await import("../src/session/runner/model.js")
  const { Session } = await import("@opencode/schema/session")
  const { Agent } = await import("@opencode/schema/agent")
  const { Money } = await import("@opencode/schema/money")
  const { Project } = await import("../src/project.js")
  const { Location } = await import("../src/location.js")
  const { AbsolutePath } = await import("../src/schema.js")
  const { OpenAIChat } = await import("@opencode/ai/protocols")
  const { LanguageModel } = await import("@opencode/ai")
  const { DateTime } = await import("effect")
  const { PluginTestLayer } = await import("./plugin/fixture.js")
  const session = Session.Info.make({
    id: Session.ID.make("ses_native_identity"), projectID: Project.ID.global, cost: Money.USD.zero,
    tokens: { input: 0, output: 0, reasoning: 0, cache: { read: 0, write: 0 } },
    time: { created: DateTime.makeUnsafe(0), updated: DateTime.makeUnsafe(0) },
    location: Location.Ref.make({ directory: AbsolutePath.make("/project") }),
  })
  const model = SessionRunnerModel.resolved(LanguageModel.make({ id: "auto", provider: "clawmanager", route: OpenAIChat.route }), {
    capabilities: { tools: true, input: ["text"], output: ["text"] }, cost: [], limit: { context: 8192, output: 1024 },
  })
  expect(String(model.ref.providerID)).toBe("clawmanager")
  const transport = SessionModelTransport.Service.of({
    bind: () => ({ execute: () => Effect.die("unused") }), close: () => Effect.void, closeAll: Effect.void,
  })
  await Effect.runPromise(Effect.gen(function* () {
    const requests = yield* SessionModelRequest.Service.pipe(Effect.provide(SessionModelRequest.layer))
    for (const kind of ["primary", "compaction", "title", "generate"] as const) {
      const operation: Effect.Effect<Prepared> = requests[kind]({ session, agent: Agent.ID.make("build"), model, system: [], messages: [] })
      const prepared = yield* kind === "primary" || kind === "compaction"
        ? withPrimary(session.id, "clawmanager", [{ type: "user", id: "msg_native" }], operation)
        : withAuxiliary(session.id, "clawmanager", kind, operation)
      expect(prepared.request.http?.headers?.["X-ClawManager-Session-ID"]).toBe(session.id)
      expect(prepared.request.http?.headers?.["X-ClawManager-Task-ID"]).toBeTruthy()
      expect(prepared.request.http?.headers?.["X-ClawManager-Runtime-Generation"]).toBe("4")
    }
  }).pipe(Effect.provideService(SessionModelTransport.Service, transport), Effect.provide(PluginTestLayer), Effect.scoped))
  await settleSession(session.id)
})
