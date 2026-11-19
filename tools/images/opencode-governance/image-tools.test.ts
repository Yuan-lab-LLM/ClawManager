import { afterAll, expect, test } from "bun:test"
import { Cause, Deferred, Effect, Exit, Fiber, Layer } from "effect"

const requestID = `image_${"a".repeat(32)}`
const artifactID = `img_${"b".repeat(32)}`
const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a/1cAAAAASUVORK5CYII=", "base64")
const edits: { get: (key: string) => unknown; getAll: (key: string) => unknown[] }[] = []
let artifactMode = "ok"
const calls: { path: string; headers: Headers; body: Record<string, unknown> }[] = []
let rejectImage = false
let revoke = false
let hang = false
let started = Deferred.makeUnsafe<void>()
const server = Bun.serve({ hostname: "127.0.0.1", port: 0, async fetch(request) {
  const path = new URL(request.url).pathname
  const multipart = request.headers.get("content-type")?.startsWith("multipart/form-data")
  const form = multipart ? await request.formData() : undefined
  if (form) edits.push(form)
  const body = request.method === "POST" && !multipart ? await request.json() as Record<string, unknown> : {}
  calls.push({ path, headers: request.headers, body })
  if (request.headers.get("authorization") !== "Bearer image-test-key") return new Response(null, { status: 401 })
  if (path.endsWith("capabilities")) return Response.json({ protocol: 1, generation: 4, task_identity_required: true })
  if (path.endsWith("check") && revoke) return new Response(null, { status: 403 })
  if (path.includes("images/artifacts/")) {
    if (artifactMode === "hang") {
      Effect.runSync(Deferred.succeed(started, undefined))
      return new Response(new ReadableStream({ start(controller) { controller.enqueue(png.subarray(0, 8)) } }), { headers: { "Content-Type": "image/png" } })
    }
    if (artifactMode === "denied") return new Response(null, { status: 404 })
    if (artifactMode === "html") return new Response("<html>private-canary</html>", { headers: { "Content-Type": "text/html" } })
    if (artifactMode === "oversize") return new Response(new Uint8Array(5 * 1024 * 1024 + 1), { headers: { "Content-Type": "image/png" } })
    if (artifactMode === "revoke") revoke = true
    return new Response(png, { headers: { "Content-Type": "image/png" } })
  }
  if (path.endsWith("images/edits")) return Response.json({ request_id: requestID, created: 1, artifact_ids: [artifactID] })
  if (path.endsWith("images/models")) return Response.json({ models: [{ id: "image-model", operations: ["generate"], estimated_price_per_image: 0.2, currency: "USD", api_key: "secret-canary" }] })
  if (path.endsWith("images/generations")) {
    if (hang) {
      Effect.runSync(Deferred.succeed(started, undefined))
      return new Response(new ReadableStream({ start(controller) { controller.enqueue(new TextEncoder().encode("{")) } }))
    }
    if (rejectImage) return Response.json({ error: "secret-provider-canary" }, { status: 502, headers: { "X-Trace-ID": requestID } })
    return Response.json({ request_id: requestID, created: 1, artifact_ids: [artifactID], b64_json: "pixel-canary", revised_prompt: "prompt-canary",
      clawmanager: { selected_model: "image-model", selected_model_id: 100, routing_policy: body.model === "auto" ? "image-priority-load-latency-v1" : "explicit-image-model", api_key: "secret-canary" } })
  }
  if (path.includes("images/requests/")) return Response.json({ request_id: requestID, state: "unknown", settled: false, estimated_cost: 0.2, artifact_ids: [], private_key: "secret-canary" })
  return Response.json({ revoked: false, finished: false, runtime_acknowledged: false })
} })
const keys = ["CLAWMANAGER_MODEL_GOVERNANCE_PROTOCOL", "CLAWMANAGER_LLM_BASE_URL", "CLAWMANAGER_LLM_API_KEY", "CLAWMANAGER_GATEWAY_GENERATION"] as const
const old = keys.map((key) => process.env[key])
process.env.CLAWMANAGER_MODEL_GOVERNANCE_PROTOCOL = "1"
process.env.CLAWMANAGER_LLM_BASE_URL = `http://127.0.0.1:${server.port}`
process.env.CLAWMANAGER_LLM_API_KEY = "image-test-key"
process.env.CLAWMANAGER_GATEWAY_GENERATION = "4"
// Dedicated process, just like the governance tests: imports pin the environment.
const { Governance } = await import("../src/clawmanager-governance.js")
const { ClawManagerImageTools } = await import("../src/clawmanager-image-tools.js")
const { Agent } = await import("@opencode/core/agent")
const { Plugin } = await import("@opencode/core/plugin")
const { PluginHost } = await import("@opencode/core/plugin/host")
const { Tool } = await import("@opencode/core/tool")
const { Session } = await import("@opencode/schema/session")
const { SessionMessage } = await import("@opencode/schema/session-message")
const { PluginTestLayer } = await import("./plugin/fixture")
const { Environment } = await import("../src/environment/index.js")
const { FileMutation } = await import("../src/file-mutation.js")
const { AppNodeBuilder } = await import("../src/effect/app-node-builder.js")
const { LayerNode } = await import("@opencode/util/effect/layer-node")
const { hostEnvironmentLayer } = await import("./fixture/environment")
const imageFilesLayer = AppNodeBuilder.build(LayerNode.group([FileMutation.node, Environment.node]), [Environment.node.replace(hostEnvironmentLayer)])
const ImageTestLayer = Layer.mergeAll(PluginTestLayer, imageFilesLayer)
afterAll(() => { server.stop(true); keys.forEach((key, i) => { if (old[i] === undefined) delete process.env[key]; else process.env[key] = old[i] }) })

test("registered native image tools retain trusted identity, redact results and use stable idempotency", async () => {
  await Effect.runPromise(Effect.gen(function* () {
    const plugins = yield* Plugin.Service
    const tools = yield* Tool.Service
    yield* ClawManagerImageTools.Plugin.effect(yield* PluginHost.make(plugins))
    const snapshot = yield* tools.snapshot()
    expect(snapshot.definitions.filter((item) => item.name.startsWith("clawmanager_")).map((item) => item.name).sort()).toEqual([
      "clawmanager_edit_image", "clawmanager_generate_image", "clawmanager_image_models", "clawmanager_image_request_status", "clawmanager_save_image",
    ])
    const sessionID = Session.ID.make("ses_images")
    const run = (name: string, input: Record<string, unknown>, id = "call_generate") => Governance.withPrimary(
      sessionID, "clawmanager", [{ type: "user", id: "msg_images" }], snapshot.execute({
        sessionID, agent: Agent.ID.make("build"), messageID: SessionMessage.ID.make("msg_answer"),
        call: { type: "tool-call", id, name, input },
      }),
    )
    const catalog = yield* run("clawmanager_image_models", {}, "call_catalog")
    expect(JSON.stringify(catalog)).toContain("image-model")
    expect(JSON.stringify(catalog)).not.toContain("canary")
    const result = yield* run("clawmanager_generate_image", { model: "image-model", prompt: "draw" })
    expect(JSON.stringify(result)).toContain(artifactID)
    expect(JSON.stringify(result)).not.toContain("canary")
    yield* run("clawmanager_generate_image", { model: "image-model", prompt: "draw" })
    const generated = calls.filter((call) => call.path.endsWith("images/generations"))
    expect(generated).toHaveLength(2)
    expect(generated[0]!.headers.get("Idempotency-Key")).toBe(generated[1]!.headers.get("Idempotency-Key"))
    expect(generated[0]!.headers.get("X-ClawManager-Task-ID")).toBe("msg_images")
    expect(generated[0]!.headers.get("X-ClawManager-Runtime-Generation")).toBe("4")
    expect(generated[0]!.headers.get("X-ClawManager-Image-Result")).toBe("references")
    expect(generated[0]!.body.n).toBe(1)
    expect(generated[0]!.body.response_format).toBeUndefined()
    const status = yield* run("clawmanager_image_request_status", { request_id: requestID }, "call_status")
    expect(JSON.stringify(status)).toContain("unknown")
    expect(JSON.stringify(status)).not.toContain("canary")
    const auto = yield* run("clawmanager_generate_image", { model: "auto", prompt: "draw" }, "call_auto")
    expect(JSON.stringify(auto)).toContain("image-priority-load-latency-v1")
    expect(JSON.stringify(auto)).toContain("image-model")
    expect(JSON.stringify(auto)).not.toContain("canary")
    expect(calls.findLast((call) => call.path.endsWith("images/generations"))!.body.model).toBe("auto")
    rejectImage = true
    const failure = yield* Effect.exit(run("clawmanager_generate_image", { model: "image-model", prompt: "draw" }, "call_fail"))
    expect(failure._tag).toBe("Failure")
    expect(JSON.stringify(failure)).not.toContain("canary")
    expect(JSON.stringify(failure)).toContain(requestID)
    rejectImage = false
    const before = calls.filter((call) => call.path.includes("/images/")).length
    revoke = true
    expect((yield* Effect.exit(run("clawmanager_generate_image", { model: "image-model", prompt: "draw" }, "call_revoked")))._tag).toBe("Failure")
    expect(calls.filter((call) => call.path.includes("/images/")).length).toBe(before)
    revoke = false
  }).pipe(Effect.provide(ImageTestLayer), Effect.scoped))
})

test("interrupting an image tool preserves cancellation instead of returning a retryable failure", async () => {
  const { imageRequest } = await import("../src/clawmanager-image-tools.js")
  hang = true
  started = Deferred.makeUnsafe<void>()
  try {
    await Effect.runPromise(Effect.gen(function* () {
      const task = Governance.withPrimary("ses_cancel_image", "clawmanager", [{ type: "user", id: "msg_cancel_image" }],
        imageRequest("generations", { sessionID: Session.ID.make("ses_cancel_image"), id: Tool.CallID.make("call_cancel") }, { model: "image-model", prompt: "draw" }))
      const fiber = yield* Effect.forkScoped(task)
      yield* Deferred.await(started)
      yield* Fiber.interrupt(fiber)
      const result = yield* Fiber.await(fiber)
      expect(Exit.isFailure(result) && Cause.hasInterruptsOnly(result.cause)).toBe(true)
    }).pipe(Effect.scoped))
  } finally { hang = false }
})

test("leaf permission denial blocks image network requests", async () => {
  const { Permission } = await import("../src/permission.js")
  const { makePluginTestLayer } = await import("./plugin/fixture")
  let assertions = 0
  const denied = Layer.succeed(Permission.Service, Permission.Service.of({
    close: Effect.void, ask: (input) => Effect.succeed({ id: input.id ?? Permission.ID.create(), effect: "deny" }),
    assert: (input) => {
      assertions++
      expect(input.action).toBe("clawmanager_generate_image")
      expect(input.resources).toEqual(["image-model"])
      expect(input.source).toMatchObject({ type: "tool", id: "call_denied" })
      return Effect.fail(new Permission.BlockedError({ permission: input.action, resources: input.resources, rules: [] }))
    },
    reply: () => Effect.void, get: () => Effect.succeed(undefined), forSession: () => Effect.succeed([]), list: () => Effect.succeed([]),
  }))
  const before = calls.filter((call) => call.path.includes("/images/")).length
  await Effect.runPromise(Effect.gen(function* () {
    yield* ClawManagerImageTools.Plugin.effect(yield* PluginHost.make(yield* Plugin.Service))
    const snapshot = yield* (yield* Tool.Service).snapshot()
    const result = yield* Effect.exit(snapshot.execute({
      sessionID: Session.ID.make("ses_denied"), agent: Agent.ID.make("build"), messageID: SessionMessage.ID.make("msg_denied"),
      call: { type: "tool-call", id: "call_denied", name: "clawmanager_generate_image", input: { model: "image-model", prompt: "draw" } },
    }))
    expect(result._tag).toBe("Failure")
  }).pipe(Effect.provide(Layer.mergeAll(makePluginTestLayer(denied), imageFilesLayer)), Effect.scoped))
  expect(assertions).toBe(1)
  expect(calls.filter((call) => call.path.includes("/images/")).length).toBe(before)
})

test("OpenAI and Anthropic prepared turns advertise and execute the same governed image tool", async () => {
  const { SessionModelRequest } = await import("../src/session/model-request.js")
  const { SessionModelTransport } = await import("../src/session/model-transport.js")
  const { SessionRunnerModel } = await import("../src/session/runner/model.js")
  const { Money } = await import("@opencode/schema/money")
  const { Project } = await import("../src/project.js")
  const { Location } = await import("../src/location.js")
  const { AbsolutePath } = await import("../src/schema.js")
  const { OpenAIChat, AnthropicMessages } = await import("@opencode/ai/protocols")
  const { LanguageModel } = await import("@opencode/ai")
  const { DateTime } = await import("effect")
  const session = Session.Info.make({
    id: Session.ID.make("ses_image_protocols"), projectID: Project.ID.global, cost: Money.USD.zero,
    tokens: { input: 0, output: 0, reasoning: 0, cache: { read: 0, write: 0 } },
    time: { created: DateTime.makeUnsafe(0), updated: DateTime.makeUnsafe(0) },
    location: Location.Ref.make({ directory: AbsolutePath.make("/project") }),
  })
  const transport = SessionModelTransport.Service.of({ bind: () => ({ execute: () => Effect.die("unused") }), close: () => Effect.void, closeAll: Effect.void })
  await Effect.runPromise(Effect.gen(function* () {
    yield* ClawManagerImageTools.Plugin.effect(yield* PluginHost.make(yield* Plugin.Service))
    const tools = yield* (yield* Tool.Service).snapshot()
    const requests = yield* SessionModelRequest.Service.pipe(Effect.provide(SessionModelRequest.layer))
    for (const route of [OpenAIChat.route, AnthropicMessages.route]) {
     for (const provider of ["clawmanager", "unmanaged"]) {
      const model = SessionRunnerModel.resolved(LanguageModel.make({ id: "auto", provider, route }), {
        capabilities: { tools: true, input: ["text"], output: ["text"] }, cost: [], limit: { context: 8192, output: 1024 },
      })
      expect(String(model.ref.providerID)).toBe(provider)
      yield* Governance.withPrimary(session.id, provider, [{ type: "user", id: "msg_image_protocols" }], Effect.gen(function* () {
        const prepared = yield* requests.primary({ session, agent: Agent.ID.make("build"), model, tools, system: [], messages: [] })
        for (const name of ["clawmanager_image_models", "clawmanager_generate_image", "clawmanager_image_request_status", "clawmanager_save_image", "clawmanager_edit_image"]) {
          expect(prepared.request.tools?.some((tool) => tool.name === name)).toBe(provider === "clawmanager")
        }
        if (provider !== "clawmanager") return
        const result = yield* prepared.executeTool({ sessionID: session.id, agent: Agent.ID.make("build"), messageID: SessionMessage.ID.make("msg_image_answer"),
          call: { type: "tool-call", id: "call_image_protocol", name: "clawmanager_generate_image", input: { model: "image-model", prompt: "draw" } } })
        expect(JSON.stringify(result)).toContain(artifactID)
        const file = route === OpenAIChat.route ? "openai.png" : "anthropic.png"
        const saved = yield* prepared.executeTool({ sessionID: session.id, agent: Agent.ID.make("build"), messageID: SessionMessage.ID.make("msg_image_answer"),
          call: { type: "tool-call", id: "call_protocol_save", name: "clawmanager_save_image", input: { artifact_id: artifactID, path: file } } })
        expect(JSON.stringify(saved)).toContain(file)
        const edited = yield* prepared.executeTool({ sessionID: session.id, agent: Agent.ID.make("build"), messageID: SessionMessage.ID.make("msg_image_answer"),
          call: { type: "tool-call", id: "call_protocol_edit", name: "clawmanager_edit_image", input: { model: "image-model", prompt: "edit", image_paths: [file] } } })
        expect(JSON.stringify(edited)).toContain(artifactID)
      }))
     }
    }
  }).pipe(Effect.provideService(SessionModelTransport.Service, transport), Effect.provide(ImageTestLayer), Effect.scoped))
})

test("save and edit tools use real workspace files, protect existing files and keep pixels out of context", async () => {
  const { Environment } = await import("../src/environment/index.js")
  const { Location } = await import("../src/location.js")
  const { symlink } = await import("node:fs/promises")
  await Effect.runPromise(Effect.gen(function* () {
    const environment = yield* Environment.Service
    const location = yield* Location.Service
    const plugins = yield* Plugin.Service
    const tools = yield* Tool.Service
    yield* ClawManagerImageTools.Plugin.effect(yield* PluginHost.make(plugins))
    const snapshot = yield* tools.snapshot()
    const sessionID = Session.ID.make("ses_image_files")
    const run = (name: string, input: Record<string, unknown>, id: string) => Governance.withPrimary(
      sessionID, "clawmanager", [{ type: "user", id: "msg_image_files" }], snapshot.execute({
        sessionID, agent: Agent.ID.make("build"), messageID: SessionMessage.ID.make("msg_files_answer"),
        call: { type: "tool-call", id, name, input },
      }),
    )
    const saved = yield* run("clawmanager_save_image", { artifact_id: artifactID, path: "generated.png" }, "call_save")
    expect(JSON.stringify(saved)).toContain("generated.png")
    expect(JSON.stringify(saved)).not.toContain(png.toString("base64"))
    expect(Buffer.from((yield* environment.files.read(`${location.directory}/generated.png`)).bytes)).toEqual(png)
    expect((yield* Effect.exit(run("clawmanager_save_image", { artifact_id: artifactID, path: "generated.png" }, "call_duplicate_save")))._tag).toBe("Failure")
    expect(Buffer.from((yield* environment.files.read(`${location.directory}/generated.png`)).bytes)).toEqual(png)
    yield* environment.files.write(`${location.directory}/mask.png`, png)
    const result = yield* run("clawmanager_edit_image", { model: "image-model", prompt: "change color", image_paths: ["generated.png"], mask_path: "mask.png" }, "call_edit")
    expect(JSON.stringify(result)).toContain(artifactID)
    expect(JSON.stringify(result)).not.toContain(png.toString("base64"))
    const form = edits.at(-1)!
    expect(form.get("model")).toBe("image-model")
    expect(form.get("n")).toBe("1")
    expect(form.get("prompt")).toBe("change color")
    const uploaded = form.getAll("image[]") as File[]
    expect(uploaded).toHaveLength(1)
    expect(uploaded[0]!.type).toBe("image/png")
    expect(Buffer.from(yield* Effect.promise(() => uploaded[0]!.arrayBuffer()))).toEqual(png)
    expect((form.get("mask") as File).type).toBe("image/png")
    const first = calls.findLast((call) => call.path.endsWith("images/edits"))!
    expect(first.headers.get("X-ClawManager-Task-ID")).toBe("msg_image_files")
    expect(first.headers.get("X-ClawManager-Image-Result")).toBe("references")
    yield* run("clawmanager_edit_image", { model: "image-model", prompt: "change color", image_paths: ["generated.png"], mask_path: "mask.png" }, "call_edit")
    expect(calls.findLast((call) => call.path.endsWith("images/edits"))!.headers.get("Idempotency-Key")).toBe(first.headers.get("Idempotency-Key"))
    yield* run("clawmanager_save_image", { artifact_id: artifactID, path: "edited.png" }, "call_save_edited")
    expect(Buffer.from((yield* environment.files.read(`${location.directory}/edited.png`)).bytes)).toEqual(png)

    // Paths and bytes come from a real temporary workspace, not a mocked file API.
    yield* Effect.promise(() => symlink(`${location.directory}/generated.png`, `${location.directory}/link.png`))
    yield* Effect.promise(() => symlink(location.directory, `${location.directory}/linked-dir`))
    yield* environment.files.write(`${location.directory}/not-image.png`, new TextEncoder().encode("private-invalid-image-canary"))
    yield* environment.files.write(`${location.directory}/large.png`, new Uint8Array(5 * 1024 * 1024 + 1))
    const before = edits.length
    for (const file of ["../outside.png", "/etc/passwd", "link.png", "linked-dir/generated.png", "not-image.png", "large.png", "missing.png"]) {
      const exit = yield* Effect.exit(run("clawmanager_edit_image", { model: "image-model", prompt: "edit", image_paths: [file] }, `call_bad_${before}`))
      expect(exit._tag).toBe("Failure")
      expect(JSON.stringify(exit)).not.toContain("private-invalid-image-canary")
    }
    expect(edits.length).toBe(before)
    for (const file of ["../outside.png", "/tmp/outside.png", "link.png", "linked-dir/new.png", "missing/new.png", "wrong.jpg"]) {
      expect((yield* Effect.exit(run("clawmanager_save_image", { artifact_id: artifactID, path: file }, "call_bad_save")))._tag).toBe("Failure")
    }
    for (const mode of ["denied", "html", "oversize", "revoke"]) {
      artifactMode = mode
      const exit = yield* Effect.exit(run("clawmanager_save_image", { artifact_id: artifactID, path: `${mode}.png` }, `call_save_${mode}`))
      expect(exit._tag).toBe("Failure")
      expect(JSON.stringify(exit)).not.toContain("private-canary")
      expect((yield* Effect.exit(environment.files.stat(`${location.directory}/${mode}.png`)))._tag).toBe("Failure")
    }
  }).pipe(Effect.provide(ImageTestLayer), Effect.scoped)).finally(() => { artifactMode = "ok"; revoke = false })
})

test("cancelling an artifact download interrupts the tool without creating a partial workspace file", async () => {
  const { Location } = await import("../src/location.js")
  artifactMode = "hang"
  started = Deferred.makeUnsafe<void>()
  try {
    await Effect.runPromise(Effect.gen(function* () {
      const environment = yield* Environment.Service
      const location = yield* Location.Service
      const plugins = yield* Plugin.Service
      const tools = yield* Tool.Service
      yield* ClawManagerImageTools.Plugin.effect(yield* PluginHost.make(plugins))
      const snapshot = yield* tools.snapshot()
      const sessionID = Session.ID.make("ses_cancel_save")
      const fiber = yield* Effect.forkScoped(Governance.withPrimary(sessionID, "clawmanager", [{ type: "user", id: "msg_cancel_save" }], snapshot.execute({
        sessionID, agent: Agent.ID.make("build"), messageID: SessionMessage.ID.make("msg_cancel_answer"),
        call: { type: "tool-call", id: "call_cancel_save", name: "clawmanager_save_image", input: { artifact_id: artifactID, path: "cancelled.png" } },
      })))
      yield* Deferred.await(started)
      yield* Fiber.interrupt(fiber)
      const exit = yield* Fiber.await(fiber)
      expect(Exit.isFailure(exit) && Cause.hasInterruptsOnly(exit.cause)).toBe(true)
      expect((yield* Effect.exit(environment.files.stat(`${location.directory}/cancelled.png`)))._tag).toBe("Failure")
    }).pipe(Effect.provide(ImageTestLayer), Effect.scoped))
  } finally { artifactMode = "ok" }
})

test("save requires edit permission and edit requires read permission before sending files", async () => {
  const { Permission } = await import("../src/permission.js")
  const { Environment } = await import("../src/environment/index.js")
  const { Location } = await import("../src/location.js")
  const { makePluginTestLayer } = await import("./plugin/fixture")
  const assertions: string[] = []
  const denied = Layer.succeed(Permission.Service, Permission.Service.of({
    close: Effect.void, ask: (input) => Effect.succeed({ id: input.id ?? Permission.ID.create(), effect: "deny" }),
    assert: (input) => {
      assertions.push(input.action)
      if (input.action.startsWith("clawmanager_")) return Effect.void
      return Effect.fail(new Permission.BlockedError({ permission: input.action, resources: input.resources, rules: [] }))
    },
    reply: () => Effect.void, get: () => Effect.succeed(undefined), forSession: () => Effect.succeed([]), list: () => Effect.succeed([]),
  }))
  const before = calls.filter((call) => call.path.includes("/images/")).length
  await Effect.runPromise(Effect.gen(function* () {
    const environment = yield* Environment.Service
    const location = yield* Location.Service
    const plugins = yield* Plugin.Service
    const tools = yield* Tool.Service
    yield* environment.files.write(`${location.directory}/input.png`, png)
    yield* ClawManagerImageTools.Plugin.effect(yield* PluginHost.make(plugins))
    const snapshot = yield* tools.snapshot()
    for (const call of [
      { type: "tool-call" as const, id: "call_save_denied", name: "clawmanager_save_image", input: { artifact_id: artifactID, path: "saved.png" } },
      { type: "tool-call" as const, id: "call_read_denied", name: "clawmanager_edit_image", input: { model: "image-model", prompt: "edit", image_paths: ["input.png"] } },
    ]) {
      expect((yield* Effect.exit(snapshot.execute({ sessionID: Session.ID.make("ses_image_files_denied"), agent: Agent.ID.make("build"), messageID: SessionMessage.ID.make("msg_files_denied"), call })))._tag).toBe("Failure")
    }
  }).pipe(Effect.provide(Layer.mergeAll(makePluginTestLayer(denied), imageFilesLayer)), Effect.scoped))
  expect(assertions).toEqual(["edit", "clawmanager_edit_image", "read"])
  expect(calls.filter((call) => call.path.includes("/images/")).length).toBe(before)
})
