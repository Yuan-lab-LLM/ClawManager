export * as ClawManagerTeamTools from "./clawmanager-team-tools.js"
import type { Context } from "@opencode/plugin/effect/plugin"
import { ToolFailure } from "@opencode/ai"
import { Effect, Schema } from "effect"
import { Governance } from "./clawmanager-governance.js"
import { Permission } from "./permission.js"

const Input = Schema.StructWithRest(Schema.Struct({
 status: Schema.optional(Schema.String), summary: Schema.optional(Schema.String),
 resultMarkdown: Schema.optional(Schema.String), text: Schema.optional(Schema.String),
 message: Schema.optional(Schema.String), to: Schema.optional(Schema.String),
 memberId: Schema.optional(Schema.String), progress: Schema.optional(Schema.Number),
 eventKind: Schema.optional(Schema.String), intent: Schema.optional(Schema.String),
 path: Schema.optional(Schema.String), content: Schema.optional(Schema.String),
 artifactRefs: Schema.optional(Schema.Array(Schema.String)),
 reviewVerdict: Schema.optional(Schema.String), reviewedAssignmentId: Schema.optional(Schema.String),
 reviewedRevision: Schema.optional(Schema.Number), reviewedArtifactRefs: Schema.optional(Schema.Array(Schema.String)),
}), [Schema.Record(Schema.String, Schema.Unknown)])
const descriptions = {
 team_send: "Send a real assignment, handoff, question or blocker to a Team member. Do not repeat an existing assignment. Use team_complete_task for delivery.",
 team_status: "Read current Team member status.",
 team_update_progress: "Report actual progress with status and summary. Active assignment identity is inherited automatically.",
 team_complete_task: "Submit actual work using status and summary, full answer in resultMarkdown and actual artifactRefs when applicable. Ordinary replies do not complete Team work. Worker delivery does not complete the Leader root. Reuse existing results when correcting a missed receipt.",
 team_artifact_read: "Read a UTF-8 file in this Team shared workspace using path.",
 team_artifact_write: "Atomically write a UTF-8 Team artifact using path and content.",
 team_artifact_list: "List this Team's artifacts using an optional path.",
 team_artifact_mkdir: "Create a member-scoped Team artifact directory using path.",
 team_artifact_preview: "Get a signed read-only Team file preview using path. Do not run temporary preview servers.",
}
export const Plugin = {
 id: "clawmanager.tool.team",
 effect: Effect.fn("ClawManagerTeamTools.Plugin")(function* (ctx: Context) {
  if (process.env.CLAWMANAGER_TEAM_ENABLED !== "true") return
  const port = Number(process.env.CLAWMANAGER_TEAM_BRIDGE_PORT)
  const token = process.env.CLAWMANAGER_TEAM_BRIDGE_TOKEN
  const generation = Number(process.env.CLAWMANAGER_GATEWAY_GENERATION)
  const managedPro = process.env.CLAWMANAGER_AGENT_ENABLED === "true" && /^[1-9][0-9]*$/.test(process.env.INSTANCE_ID || "")
  if (!Governance.enabled || !Number.isInteger(port) || port < 1 || port > 65535 || !token || !Number.isSafeInteger(generation) || generation < 0 || generation === 0 && !managedPro)
   return yield* Effect.die(new Error("OpenCode Team adapter is not ready."))
  const permission = yield* Permission.Service
  yield* ctx.tool.transform(editor => {
   for (const [name, description] of Object.entries(descriptions)) editor.add({
    name, description, options: { codemode: false }, input: Input, output: Schema.Unknown,
    execute: (args, context) => Effect.gen(function* () {
     yield* permission.assert({ action: name, resources: [process.env.CLAWMANAGER_TEAM_ID || ""], save: ["*"], metadata: {}, sessionID: context.sessionID, agent: context.agent, source: { type: "tool", messageID: context.messageID, id: context.id } }).pipe(Effect.mapError(() => new ToolFailure({ message: "Team tool permission denied." })))
     const identity = yield* Governance.headers
     if (identity["X-ClawManager-Session-ID"] !== context.sessionID)
      return yield* Effect.fail(new ToolFailure({ message: "Team session identity mismatch." }))
     const output = yield* Effect.tryPromise({
      try: async signal => {
       const response = await fetch(`http://127.0.0.1:${port}/tool`, {
        method: "POST", redirect: "error", signal: AbortSignal.any([signal, AbortSignal.timeout(30000)]),
        headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
        body: JSON.stringify({ name, arguments: args, sessionID: context.sessionID, taskID: identity["X-ClawManager-Task-ID"], generation, callID: context.id }),
       })
       if (!response.ok) { await response.body?.cancel(); throw new Error() }
       if (!response.body) throw new Error()
       const reader = response.body.getReader()
       const chunks: Uint8Array[] = []
       let size = 0
       try {
        while (true) {
         const { done, value } = await reader.read()
         if (done) break
         size += value.byteLength
         if (size > 4 * 1024 * 1024) throw new Error()
         chunks.push(value)
        }
        return JSON.parse(Buffer.concat(chunks).toString("utf8"))
       } finally { await reader.cancel().catch(() => {}); reader.releaseLock() }
      },
      catch: () => new ToolFailure({ message: "Team operation failed or result is unconfirmed. Inspect the original work before retrying." }),
     })
     return { output, content: [{ type: "text" as const, text: JSON.stringify(output) }] }
    }),
   })
  })
 }),
}
