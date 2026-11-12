import { afterAll, expect, test } from "bun:test"
import { Effect } from "effect"

const calls: Record<string, unknown>[] = []
const generation = process.env.TEAM_TEST_GENERATION === "0" ? 0 : 4
let denied = false
const server = Bun.serve({ hostname: "127.0.0.1", port: 0, async fetch(request) {
 const path = new URL(request.url).pathname
 if (path === "/tool") {
  if (request.headers.get("authorization") !== "Bearer bridge-test") return new Response(null, { status: 403 })
  calls.push(await request.json() as Record<string, unknown>)
  return denied ? Response.json({ error: "private-canary" }, { status: 409 }) : Response.json({ ok: true, result: { accepted: true } })
 }
 if (path.endsWith("capabilities")) return Response.json({ protocol: 1, generation, task_identity_required: true })
 return Response.json({ revoked: false, finished: false, runtime_acknowledged: false })
} })
const values = { CLAWMANAGER_TEAM_ENABLED: "true", CLAWMANAGER_TEAM_ID: "12", CLAWMANAGER_MODEL_GOVERNANCE_PROTOCOL: "1", CLAWMANAGER_LLM_BASE_URL: `http://127.0.0.1:${server.port}`, CLAWMANAGER_LLM_API_KEY: "test-key", CLAWMANAGER_GATEWAY_GENERATION: "4", CLAWMANAGER_TEAM_BRIDGE_PORT: String(server.port), CLAWMANAGER_TEAM_BRIDGE_TOKEN: "bridge-test" }
const previous = new Map([...Object.keys(values), "CLAWMANAGER_AGENT_ENABLED", "INSTANCE_ID"].map(key => [key, process.env[key]]))
Object.assign(process.env, values, { CLAWMANAGER_GATEWAY_GENERATION: String(generation), ...(generation === 0 ? { CLAWMANAGER_AGENT_ENABLED: "true", INSTANCE_ID: "570" } : {}) })
const { Governance } = await import("../src/clawmanager-governance.js")
const { ClawManagerTeamTools } = await import("../src/clawmanager-team-tools.js")
const { Agent } = await import("@opencode/core/agent")
const { Plugin } = await import("@opencode/core/plugin")
const { PluginHost } = await import("@opencode/core/plugin/host")
const { Tool } = await import("@opencode/core/tool")
const { Session } = await import("@opencode/schema/session")
const { SessionMessage } = await import("@opencode/schema/session-message")
const { PluginTestLayer } = await import("./plugin/fixture")
afterAll(() => { server.stop(true); for (const [key, value] of previous) { if (value === undefined) delete process.env[key]; else process.env[key] = value } })

test("native Team tools inherit task identity and preserve existing optional workflow fields", async () => {
 await Effect.runPromise(Effect.gen(function* () {
  yield* ClawManagerTeamTools.Plugin.effect(yield* PluginHost.make(yield* Plugin.Service))
  const snapshot = yield* (yield* Tool.Service).snapshot()
  expect(snapshot.definitions.filter(item => item.name.startsWith("team_")).length).toBe(9)
  const sessionID = Session.ID.make("ses_team")
  const run = (input: Record<string, unknown>, session = sessionID) => Governance.withPrimary(sessionID, "clawmanager", [{ type: "user", id: "msg_team_input" }], snapshot.execute({ sessionID: session, agent: Agent.ID.make("build"), messageID: SessionMessage.ID.make("msg_answer"), call: { type: "tool-call", id: "call_team", name: "team_complete_task", input } }))
  const input = { status: "succeeded", summary: "delivered", phaseDispositions: [{ phaseId: "P1", decision: "skipped", reason: "not needed" }] }
  yield* run(input)
  expect(calls).toHaveLength(1)
  expect(calls[0]).toMatchObject({ name: "team_complete_task", arguments: input, sessionID, taskID: "msg_team_input", generation, callID: "call_team" })
  expect((yield* Effect.exit(run(input, Session.ID.make("ses_other"))))._tag).toBe("Failure")
  expect(calls).toHaveLength(1)
  denied = true
  const result = yield* Effect.exit(run(input))
  expect(result._tag).toBe("Failure")
  expect(JSON.stringify(result)).not.toContain("private-canary")
 }).pipe(Effect.provide(PluginTestLayer), Effect.scoped))
})
