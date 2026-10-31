// Installed only into the pinned OpenCode core. No UI patch or provider credentials.
export * as ClawManagerImageTools from "./clawmanager-image-tools.js"

import { createHash } from "node:crypto"
import path from "node:path"
import type { Context } from "@opencode/plugin/effect/plugin"
import type { SessionHooks } from "@opencode/plugin/effect/session"
import type { Tool } from "@opencode/schema/tool"
import { ToolFailure } from "@opencode/ai"
import { Effect, Schema } from "effect"
import { Governance } from "./clawmanager-governance.js"
import { Permission } from "./permission.js"
import { FileAccess } from "./file-access.js"
import { FileMutation } from "./file-mutation.js"
import { Environment } from "./environment/index.js"
import { Location } from "./location.js"

const base = (process.env.CLAWMANAGER_LLM_BASE_URL ?? "").replace(/\/+$/, "")
const key = process.env.CLAWMANAGER_LLM_API_KEY ?? ""
const RequestID = Schema.String.check(Schema.isPattern(/^image_[a-f0-9]{32}$/))
const ArtifactID = Schema.String.check(Schema.isPattern(/^img_[a-f0-9]{32}$/))
const ModelID = Schema.String.check(Schema.isMinLength(1), Schema.isMaxLength(256))
const Catalog = Schema.Struct({ models: Schema.Array(Schema.Struct({
  id: ModelID,
  operations: Schema.Array(Schema.Literals(["generate", "edit"])),
  estimated_price_per_image: Schema.Number,
  currency: Schema.Literal("USD"),
})) })
const Generated = Schema.Struct({
  request_id: RequestID, artifact_ids: Schema.Array(ArtifactID), created: Schema.Number,
  clawmanager: Schema.optional(Schema.Struct({
    selected_model: Schema.optional(ModelID), selected_model_id: Schema.optional(Schema.Number),
    routing_policy: Schema.optional(Schema.String.check(Schema.isMaxLength(64))),
  })),
})
const Status = Schema.Struct({
  request_id: RequestID, state: Schema.String, settled: Schema.Boolean,
  records_ready: Schema.optional(Schema.Boolean),
  estimated_cost: Schema.Number, artifact_ids: Schema.Array(ArtifactID),
})
const GenerateInput = Schema.Struct({
  model: ModelID.annotate({ description: "Exact image-model ID from clawmanager_image_models, or auto for Gateway capability/permission/load routing. Never use a chat model ID. Auto selects one deployment and never retries another paid operation." }),
  prompt: Schema.String.check(Schema.isMinLength(1), Schema.isMaxLength(32000)),
  size: Schema.optional(Schema.String.check(Schema.isMaxLength(64))),
  quality: Schema.optional(Schema.String.check(Schema.isMaxLength(64))),
})
const WorkspacePath = Schema.String.check(Schema.isMinLength(1), Schema.isMaxLength(1024)).annotate({
  description: "Relative path to an ordinary image file within the active workspace. No absolute paths, parent traversal or symbolic links. Parent directories must already exist.",
})
const Saved = Schema.Struct({ artifact_id: ArtifactID, path: Schema.String, mime: Schema.String, bytes: Schema.Number })
const EditInput = Schema.Struct({
  ...GenerateInput.fields,
  image_paths: Schema.Array(WorkspacePath).check(Schema.isMinLength(1), Schema.isMaxLength(16)),
  mask_path: Schema.optional(WorkspacePath.annotate({ description: "Optional PNG mask in the workspace; dimensions must match the first image. Transparent areas indicate edits." })),
})
const maxImageBytes = 5 * 1024 * 1024

class ImageToolError extends Error {}

// The LLM cannot supply identity, endpoint, authorization, or idempotency headers.
const gatewayRequest = Effect.fn("ClawManager.imageGatewayRequest")(function* (
  resource: string, context: Pick<Tool.Context, "sessionID" | "id">, body?: unknown, binary = false,
) {
  if (!Governance.enabled) return yield* Effect.fail(new ImageToolError("Governed image tools are unavailable in this runtime."))
  const identity = yield* Governance.headers
  if (identity["X-ClawManager-Session-ID"] !== context.sessionID)
    return yield* Effect.fail(new ImageToolError("Image tool session does not match the active task."))
  const idempotency = createHash("sha256").update(JSON.stringify([
    identity["X-ClawManager-Session-ID"], identity["X-ClawManager-Task-ID"],
    identity["X-ClawManager-Runtime-Generation"], context.id, resource,
  ])).digest("hex")
  return yield* Effect.tryPromise({
    try: async (signal) => {
      const multipart = body instanceof FormData
      const response = await fetch(`${base}/images/${resource}`, {
        method: body === undefined ? "GET" : "POST", redirect: "error",
        signal: AbortSignal.any([signal, AbortSignal.timeout(300000)]),
        headers: { ...identity, Authorization: `Bearer ${key}`, ...(multipart ? {} : { "Content-Type": "application/json" }),
          ...(body === undefined ? {} : { "Idempotency-Key": idempotency, "X-ClawManager-Image-Result": "references" }) },
        body: body === undefined ? undefined : multipart ? body : JSON.stringify(body),
      })
      if (!response.ok) {
        // Never return raw upstream messages/URLs or credentials to model context.
        const trace = response.headers.get("X-Trace-ID") ?? ""
        await response.body?.cancel()
        throw new ImageToolError(`Image Gateway HTTP ${response.status}.${/^image_[a-f0-9]{32}$/.test(trace) ? ` Request ID: ${trace}. Query its status.` : ""} Do not automatically resubmit a paid generation.`)
      }
      if (!response.body) throw new ImageToolError("Image Gateway returned no result.")
      const mime = (response.headers.get("Content-Type") ?? "").split(";")[0].trim().toLowerCase()
      if (binary && !["image/png", "image/jpeg", "image/webp"].includes(mime)) {
        await response.body.cancel()
        throw new ImageToolError("Image artifact must be PNG, JPEG or WebP.")
      }
      const reader = response.body.getReader()
      const chunks: Uint8Array[] = []
      let length = 0
      try {
        while (true) {
          const chunk = await reader.read()
          if (chunk.done) break
          length += chunk.value.byteLength
          if (length > (binary ? maxImageBytes : 1024 * 1024)) throw new ImageToolError("Image Gateway response exceeds the size limit.")
          chunks.push(chunk.value)
        }
      } finally { await reader.cancel() }
      return { bytes: Buffer.concat(chunks), mime }
    },
    catch: (error) => error instanceof ImageToolError ? error : new ImageToolError(
      "Image Gateway connection interrupted or response invalid. Generation outcome may be unknown; do not automatically issue a new paid generation.",
    ),
  })
})

export const imageRequest = Effect.fn("ClawManager.imageRequest")(function* (
  resource: string, context: Pick<Tool.Context, "sessionID" | "id">, body?: unknown,
) {
  const response = yield* gatewayRequest(resource, context, body)
  return yield* Schema.decodeUnknownEffect(Schema.fromJsonString(Schema.Unknown))(response.bytes.toString("utf8")).pipe(
    Effect.mapError(() => new ImageToolError("Image Gateway returned invalid JSON. Do not automatically resubmit a paid generation.")),
  )
})

// FileAccess owns permission resources; Environment owns filesystem access. The
// image tools narrow this further to ordinary files under the active Location.
const workspaceTarget = Effect.fn("ClawManager.imageWorkspaceTarget")(function* (
  root: string, input: string, access: FileAccess.Interface, environment: Environment.Interface,
) {
  if (path.isAbsolute(input) || input.includes("\\") || input.includes(":") || input.includes("\0") || input.startsWith("~") || input.split("/").some((part) => !part || part === "." || part === ".."))
    return yield* Effect.fail(new ImageToolError("Use a relative image path inside the workspace without traversal."))
  const target = yield* access.resolve({ path: input, kind: "file" })
  const relative = path.relative(root, target.absolute)
  if (!relative || relative === ".." || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative))
    return yield* Effect.fail(new ImageToolError("Image path is outside the active workspace."))
  const parts = relative.split(path.sep)
  for (let i = 1; i <= parts.length; i++) {
    const info = yield* environment.files.stat(path.join(root, ...parts.slice(0, i))).pipe(
      Effect.catchTag("Environment.NotFound", () => Effect.undefined),
    )
    if (i < parts.length && info?.type !== "directory")
      return yield* Effect.fail(new ImageToolError("Image parent directories must already exist and must not be symbolic links."))
    if (i === parts.length && info && info.type !== "file")
      return yield* Effect.fail(new ImageToolError("Image target must be a regular file, not a symbolic link or directory."))
  }
  return target
})

function imageMime(bytes: Uint8Array) {
  const data = Buffer.from(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  if (data.length >= 8 && data.subarray(0, 8).equals(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]))) return "image/png"
  if (data.length >= 3 && data[0] === 255 && data[1] === 216 && data[2] === 255) return "image/jpeg"
  if (data.length >= 12 && data.toString("ascii", 0, 4) === "RIFF" && data.toString("ascii", 8, 12) === "WEBP") return "image/webp"
  throw new ImageToolError("Only PNG, JPEG or WebP image files are supported.")
}

export const Plugin = {
  id: "clawmanager.tool.images",
  effect: Effect.fn("ClawManagerImageTools.Plugin")(function* (ctx: Context) {
    // Old/non-governed runtimes retain their existing tools and behavior.
    if (!Governance.enabled) return
    const permission = yield* Permission.Service
    const access = yield* FileAccess.Service
    const environment = yield* Environment.Service
    const mutation = yield* FileMutation.Service
    const location = yield* Location.Service
    const authorize = (action: string, resource: string, context: Tool.Context) => permission.assert({
      action, resources: [resource], save: ["*"], metadata: {},
      sessionID: context.sessionID, agent: context.agent,
      source: { type: "tool", messageID: context.messageID, id: context.id },
    }).pipe(Effect.mapError((error) => new ToolFailure({ message: "Image tool permission denied.", error })))
    const failed = (error: unknown) => error instanceof ToolFailure ? error : new ToolFailure({
      message: error instanceof ImageToolError ? error.message : error instanceof Permission.BlockedError
        ? "Image tool/file permission denied." : "Image operation failed: check file access, input format and Gateway availability. Do not automatically resubmit a paid generation.",
    })
    const readImage = Effect.fn("ClawManager.readWorkspaceImage")(function* (file: string, context: Tool.Context) {
      const target = yield* workspaceTarget(location.directory, file, access, environment)
      yield* access.authorizeRead(target.absolute, context)
      const result = yield* environment.files.read(target.absolute, { offset: 0, length: maxImageBytes + 1 })
      if (result.info.size > maxImageBytes || result.bytes.length > maxImageBytes)
        return yield* Effect.fail(new ImageToolError("Each edit input must be at most 5 MiB."))
      const mime = yield* Effect.try({ try: () => imageMime(result.bytes), catch: () => new ImageToolError("Edit inputs must contain PNG, JPEG or WebP bytes.") })
      return { bytes: result.bytes, mime }
    })
    yield* ctx.tool.transform((editor) => {
      editor.add({
        name: "clawmanager_image_models", options: { codemode: false },
        description: "List image models currently allowed for this instance owner and key. Prices are administrator estimates, not provider invoices. Use before generating images; an empty list means no eligible image models.",
        input: Schema.Struct({}), output: Catalog,
        execute: (_, context) => Effect.gen(function* () {
          yield* authorize("clawmanager_image_models", "models", context)
          const output = yield* imageRequest("models", context).pipe(
            Effect.flatMap(Schema.decodeUnknownEffect(Catalog)), Effect.mapError(failed),
          )
          return { output, content: [{ type: "text" as const, text: JSON.stringify(output) }] }
        }),
      })
      editor.add({
        name: "clawmanager_generate_image", options: { codemode: false },
        description: "Generate ONE image through the governed ClawManager Gateway. This can incur costs. Choose an authorized generate-capable model from clawmanager_image_models, or use auto to delegate selection to Gateway. Returns private artifact IDs and selected model metadata, NOT a public URL or saved workspace file. Use clawmanager_save_image to save the result; do not claim it was saved/displayed before that succeeds. Never automatically retry an uncertain result using another tool call; query the request ID instead.",
        input: GenerateInput, output: Generated,
        execute: (input, context) => Effect.gen(function* () {
          yield* authorize("clawmanager_generate_image", input.model, context)
          const output = yield* imageRequest("generations", context, { ...input, n: 1 }).pipe(
            Effect.flatMap(Schema.decodeUnknownEffect(Generated)), Effect.mapError(failed),
          )
          return { output, content: [{ type: "text" as const, text: JSON.stringify(output) }] }
        }),
      })
      editor.add({
        name: "clawmanager_save_image", options: { codemode: false, permission: "edit" },
        description: "Download an authorized image artifact into an ordinary workspace file (PNG/JPEG/WebP, at most 5 MiB). Uses file edit permission and never intentionally overwrites an existing file. Parent directories must exist. After success, the user can refresh ClawManager Workspace and click the image to preview/download it. No new paid generation occurs. This creates a local copy that later model revocation cannot erase.",
        input: Schema.Struct({ artifact_id: ArtifactID, path: WorkspacePath }), output: Saved,
        execute: (input, context) => Effect.gen(function* () {
          const target = yield* workspaceTarget(location.directory, input.path, access, environment)
          yield* authorize("edit", target.resource, context)
          const artifact = yield* gatewayRequest(`artifacts/${input.artifact_id}`, context, undefined, true)
          const mime = yield* Effect.try({ try: () => imageMime(artifact.bytes), catch: () => new ImageToolError("Image artifact bytes are invalid.") })
          if (mime !== artifact.mime || !({ "image/png": [".png"], "image/jpeg": [".jpg", ".jpeg"], "image/webp": [".webp"] }[mime]?.includes(path.extname(input.path).toLowerCase())))
            return yield* Effect.fail(new ImageToolError("Destination extension must match the image's PNG, JPEG or WebP format."))
          yield* mutation.withLock([target.absolute])(Effect.gen(function* () {
            yield* workspaceTarget(location.directory, input.path, access, environment)
            const exists = yield* environment.files.stat(target.absolute).pipe(Effect.as(true), Effect.catchTag("Environment.NotFound", () => Effect.succeed(false)))
            if (exists) return yield* Effect.fail(new ImageToolError("Destination already exists. Choose another filename; no file was overwritten."))
            yield* Governance.headers
            yield* mutation.write({ target, content: artifact.bytes })
          }))
          const output = { artifact_id: input.artifact_id, path: target.resource, mime, bytes: artifact.bytes.length }
          return { output, content: [{ type: "text" as const, text: `Saved image: ${target.resource}. Refresh ClawManager Workspace and click this file to preview it.` }] }
        }).pipe(Effect.mapError(failed)),
      })
      editor.add({
        name: "clawmanager_edit_image", options: { codemode: false },
        description: "Edit workspace PNG/JPEG/WebP images through the governed Gateway; choose an edit-capable model from clawmanager_image_models or use auto. This can incur costs and sends the selected files to the selected model provider. Requires file read and tool permissions. Up to 16 input images, each at most 5 MiB and combined with optional PNG mask at most 24 MiB. Returns private artifact IDs and selected model metadata; use clawmanager_save_image to save the result. Do not automatically resubmit uncertain outcomes.",
        input: EditInput, output: Generated,
        execute: (input, context) => Effect.gen(function* () {
          yield* authorize("clawmanager_edit_image", input.model, context)
          const form = new FormData()
          form.set("model", input.model)
          form.set("prompt", input.prompt)
          form.set("n", "1")
          if (input.size) form.set("size", input.size)
          if (input.quality) form.set("quality", input.quality)
          let total = 0
          const files = input.image_paths.map((file) => ({ file, field: "image[]" }))
          if (input.mask_path) files.push({ file: input.mask_path, field: "mask" })
          for (const entry of files) {
            const image = yield* readImage(entry.file, context)
            if (entry.field === "mask" && image.mime !== "image/png")
              return yield* Effect.fail(new ImageToolError("Edit mask must be a PNG image."))
            total += image.bytes.length
            if (total > 24 * 1024 * 1024) return yield* Effect.fail(new ImageToolError("Combined edit inputs exceed 24 MiB."))
            form.append(entry.field, new Blob([new Uint8Array(image.bytes)], { type: image.mime }), `input-${files.indexOf(entry)}.${image.mime === "image/jpeg" ? "jpeg" : image.mime.slice(6)}`)
          }
          const output = yield* imageRequest("edits", context, form).pipe(Effect.flatMap(Schema.decodeUnknownEffect(Generated)))
          return { output, content: [{ type: "text" as const, text: JSON.stringify(output) }] }
        }).pipe(Effect.mapError(failed)),
      })
      editor.add({
        name: "clawmanager_image_request_status", options: { codemode: false },
        description: "Inspect an existing image request without starting another paid generation. Unknown means the provider outcome is unresolved, not that retrying is free. Artifact IDs require authenticated access and may expire.",
        input: Schema.Struct({ request_id: RequestID }), output: Status,
        execute: (input, context) => Effect.gen(function* () {
          yield* authorize("clawmanager_image_request_status", input.request_id, context)
          const output = yield* imageRequest(`requests/${input.request_id}`, context).pipe(
            Effect.flatMap(Schema.decodeUnknownEffect(Status)), Effect.mapError(failed),
          )
          return { output, content: [{ type: "text" as const, text: JSON.stringify(output) }] }
        }),
      })
    }).pipe(Effect.orDie)
    const managedOnly = (event: SessionHooks["context"]) => Effect.sync(() => {
      if (event.model.providerID === "clawmanager") return
      delete event.tools.clawmanager_image_models
      delete event.tools.clawmanager_generate_image
      delete event.tools.clawmanager_image_request_status
      delete event.tools.clawmanager_save_image
      delete event.tools.clawmanager_edit_image
    })
    yield* ctx.session.hook("context", managedOnly)
    yield* ctx.session.hook("compaction", managedOnly)
    yield* ctx.session.hook("generate", managedOnly)
  }),
}
