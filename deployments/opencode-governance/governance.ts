// OpenCode 2.0.27 adapter. No UI, prompt logging, or provider credentials here.
import { randomUUID } from "node:crypto"
import { Context, Effect } from "effect"
import { Model } from "@opencode/schema/model"

export class GovernanceError extends Error {
  constructor(readonly revoked = false) {
    super(revoked ? "当前模型不可用，本轮任务已中止，请重新选择模型" : "模型授权检查暂时不可用，已停止当前任务，请稍后重试")
  }
}

export type Task = { sessionID: string; taskID: string; revoked: boolean; settled: boolean; primary: boolean }
type CatalogEntry = { id: string; context_window: number; tools: boolean; reasoning: boolean; vision?: boolean }
const base = (process.env.CLAWMANAGER_LLM_BASE_URL ?? "").replace(/\/+$/, "")
const key = process.env.CLAWMANAGER_LLM_API_KEY ?? ""
export const enabled = process.env.CLAWMANAGER_MODEL_GOVERNANCE_PROTOCOL === "1" && Boolean(base && key)
const generation = Number(process.env.CLAWMANAGER_GATEWAY_GENERATION ?? "0")
const tasks = new Map<string, Task>()
const primary = new Map<string, Task>()
const pendingReceipts = new Set<Task>()
let enrollment: Promise<void> | undefined
let enrolledAt = 0
let cached: CatalogEntry[] | undefined
let catalogAt = 0
let catalogPending: Promise<CatalogEntry[]> | undefined
let receiptFlush: Promise<void> | undefined
let maintenanceOperation = ""
let maintenanceClosed = false
let maintenanceAt = 0
let admissions = 0
let jobScopes = 0
const Admitted = Context.Reference<boolean>("@clawmanager/Admitted", { defaultValue: () => false })

// Do not expose a ready-to-stop flag here. These counters are evidence for the
// control plane, which must still fence placement and confirm process exit.
export async function pollMaintenance(active: number, queued: number) {
  if (!enabled) return
  const value = await request("maintenance", {
    generation, operation_id: maintenanceOperation, gate_closed: maintenanceClosed,
    active_executions: active + [...tasks.values()].filter((task) => !task.settled).length,
    active_admissions: admissions, active_jobs: jobScopes, queued_inputs: queued,
  })
  if (!value || typeof value !== "object" || !("generation" in value) || value.generation !== generation ||
    !("operation_id" in value) || typeof value.operation_id !== "string" ||
    (value.operation_id !== "" && !/^[a-f0-9]{32}$/.test(value.operation_id)) ||
    !("draining" in value) || typeof value.draining !== "boolean" || (value.draining && !value.operation_id)) throw new GovernanceError()
  maintenanceOperation = value.operation_id
  maintenanceClosed = value.draining
  maintenanceAt = Date.now()
}

export function admit<A, E, R>(operation: Effect.Effect<A, E, R>) {
  if (!enabled) return operation
  return Effect.gen(function* () {
    // Tool-spawned continuations belong to already accepted work. The parent
    // execution/job remains counted until its scoped cleanup has completed.
    if ((yield* Admitted) || (yield* CurrentTask)) return yield* operation
    return yield* Effect.acquireUseRelease(
      Effect.sync(() => {
        if (maintenanceClosed || !maintenanceAt || Date.now() - maintenanceAt >= 5000)
          throw new Error("实例正在维护或维护状态暂不可用，请稍后重试；当前任务不会被强制终止")
        admissions++
      }),
      () => operation.pipe(Effect.provideService(Admitted, true)),
      () => Effect.sync(() => { admissions-- }),
    )
  })
}

// Register FIRST on a job scope, so this LIFO finalizer runs after tool/process
// cleanup. A background result alone is not evidence its process has exited.
export function jobStarted() {
  if (!enabled) return Effect.void
  jobScopes++
  return Effect.sync(() => { jobScopes-- })
}

export const CurrentTask = Context.Reference<Task | undefined>("@clawmanager/Task", { defaultValue: () => undefined })
const identity = (task: Task) => ({ session_id: task.sessionID, task_id: task.taskID, generation })
const taskKey = (sessionID: string, taskID: string) => JSON.stringify([sessionID, taskID])

async function request(path: string, body?: object): Promise<unknown> {
  const response = await fetch(`${base}/runtime/${path}`, {
    method: body === undefined ? "GET" : "POST", redirect: "error",
    headers: { Authorization: `Bearer ${key}`, "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(2500),
  }).catch(() => { throw new GovernanceError() })
  if (response.status === 403) throw new GovernanceError(true)
  if (!response.ok) throw new GovernanceError()
  return response.json().catch(() => { throw new GovernanceError() })
}

export async function negotiate() {
  if (!enabled) return
  if (!Number.isSafeInteger(generation) || generation < 0) throw new GovernanceError()
  if (enrolledAt && Date.now() - enrolledAt < 10000) return
  enrollment ??= request("capabilities", {
    protocol: 1, generation, runtime_version: "2.0.27", adapter_version: "cm-governance-2",
    capabilities: ["task_identity", "turn_cancel", "cancel_receipt", "live_catalog", "maintenance_drain"],
  }).then((value) => {
    if (!value || typeof value !== "object" || !("protocol" in value) || value.protocol !== 1 ||
      !("generation" in value) || value.generation !== generation ||
      !("task_identity_required" in value) || value.task_identity_required !== true) throw new GovernanceError()
    enrolledAt = Date.now()
  }).finally(() => { enrollment = undefined })
  return enrollment
}

export async function check(task: Task) {
  if (task.revoked) throw new GovernanceError(true)
  try { await request("tasks/check", identity(task)) }
  catch (error) { if (error instanceof GovernanceError && error.revoked) task.revoked = true; throw error }
}

async function begin(sessionID: string, taskID: string, isPrimary = false) {
  await negotiate()
  if (!sessionID || !taskID) throw new GovernanceError()
  const id = taskKey(sessionID, taskID)
  const task = tasks.get(id) ?? { sessionID, taskID, revoked: false, settled: false, primary: isPrimary }
  tasks.set(id, task)
  if (isPrimary) primary.set(sessionID, task)
  await check(task)
  return task
}

export const headers = Effect.gen(function* (): Effect.fn.Return<Record<string, string>> {
  if (!enabled) return {}
  const task = yield* CurrentTask
  if (!task) return yield* Effect.die(new GovernanceError())
  yield* Effect.promise(() => check(task))
  return {
    "X-ClawManager-Session-ID": task.sessionID,
    "X-ClawManager-Task-ID": task.taskID,
    "X-ClawManager-Runtime-Generation": String(generation),
  }
})

// raceFirst interrupts and joins the operation, including scoped tool fibers,
// before the caller may record a cancellation receipt.
export function guard<A, E, R>(operation: Effect.Effect<A, E, R>, task: Task) {
  const watch = Effect.forever(Effect.promise(() => check(task)).pipe(Effect.andThen(Effect.sleep("1 second"))))
  return Effect.raceFirst(operation, watch).pipe(Effect.provideService(CurrentTask, task))
}

export function withPrimary<A, E, R>(sessionID: string, provider: string, messages: readonly { type: string; id: string }[], operation: Effect.Effect<A, E, R>) {
  if (!enabled || provider !== "clawmanager") return operation
  return Effect.gen(function* () {
    // The original message can leave projected context after compaction. Reuse
    // only this execution's exact identity, never prompt text or a "main" alias.
    const taskID = messages.findLast((item) => item.type === "user")?.id ?? primary.get(sessionID)?.taskID
    if (!taskID) return yield* Effect.die(new GovernanceError())
    const task = yield* Effect.promise(() => begin(sessionID, taskID, true))
    return yield* guard(operation, task)
  })
}

export function withAuxiliary<A, E, R>(sessionID: string, provider: string, kind: string, operation: Effect.Effect<A, E, R>, inputID?: string) {
  if (!enabled || provider !== "clawmanager") return operation
  return Effect.gen(function* () {
    const inherited = yield* CurrentTask
    // Automatic compaction belongs to the primary task, not a fresh task that
    // could escape revocation. Explicit generate/title operations are separate.
    if ((kind === "compaction" || kind === "stateless") && inherited) return yield* guard(operation, inherited)
    const task = yield* Effect.promise(() => begin(sessionID, `${kind}:${inputID ?? randomUUID()}`))
    return yield* guard(operation, task).pipe(Effect.ensuring(Effect.promise(() => settle(task))))
  })
}

export function guardSession<A, E, R>(sessionID: string, operation: Effect.Effect<A, E, R>) {
  if (!enabled) return operation
  const watch = Effect.forever(Effect.promise(async () => {
    // Covers retries, step boundaries and a switch to a different provider.
    for (const task of tasks.values()) {
      if (task.sessionID === sessionID && !task.settled && task.primary) await check(task)
    }
  }).pipe(Effect.andThen(Effect.sleep("1 second"))))
  return Effect.raceFirst(operation, watch)
}

async function settle(task: Task) {
  task.settled = true
  pendingReceipts.add(task)
  await flushReceipts()
}

export async function settleSession(sessionID: string) {
  primary.delete(sessionID)
  for (const task of tasks.values()) {
    if (task.sessionID !== sessionID || !task.primary) continue
    task.settled = true
    pendingReceipts.add(task)
  }
  await flushReceipts()
}

async function flushReceipts() {
  if (receiptFlush) return receiptFlush
  receiptFlush = (async () => {
    for (const task of pendingReceipts) {
      try {
        // Do not infer cancellation from a last-minute policy read AFTER the
        // work completed. Only an observed revocation during execution counts.
        await request("tasks/finish", { ...identity(task), cancelled: task.revoked })
        pendingReceipts.delete(task)
        tasks.delete(taskKey(task.sessionID, task.taskID))
      } catch { /* Unconfirmed; retry on a later catalog tick. Never claim success. */ }
    }
  })().finally(() => { receiptFlush = undefined })
  return receiptFlush
}

export async function catalog() {
  await negotiate()
  void flushReceipts()
  if (cached && Date.now() - catalogAt < 3000) return cached
  catalogPending ??= request("catalog").then((value) => {
    if (!value || typeof value !== "object" || !("models" in value) || !Array.isArray(value.models)) throw new GovernanceError()
    const entries: CatalogEntry[] = value.models.map((entry: unknown) => {
      if (!entry || typeof entry !== "object" || !("id" in entry) || typeof entry.id !== "string" || !entry.id ||
        !("context_window" in entry) || typeof entry.context_window !== "number" || !Number.isSafeInteger(entry.context_window) || entry.context_window < 0 ||
        !("tools" in entry) || typeof entry.tools !== "boolean" || !("reasoning" in entry) || typeof entry.reasoning !== "boolean") throw new GovernanceError()
      if ("vision" in entry && typeof entry.vision !== "boolean") throw new GovernanceError()
      return { id: entry.id, context_window: entry.context_window, tools: entry.tools, reasoning: entry.reasoning, vision: "vision" in entry ? entry.vision as boolean : undefined }
    })
    cached = entries; catalogAt = Date.now(); return entries
  }).finally(() => { catalogPending = undefined })
  return catalogPending
}

export async function projectCatalog(existing: readonly Model.Info[]): Promise<readonly Model.Info[]> {
  if (!enabled) return existing
  const unmanaged = existing.filter((item) => item.providerID !== "clawmanager")
  const managed = existing.filter((item) => item.providerID === "clawmanager")
  const template = managed.find((item) => item.id === "auto") ?? managed[0]
  if (!template) return unmanaged
  const entries = await catalog().catch(() => undefined)
  if (!entries) return unmanaged
  return [...unmanaged, ...entries.map((entry) => {
    const old = managed.find((item) => item.id === entry.id) ?? template
    return { ...old, id: Model.ID.make(entry.id), modelID: Model.ID.make(entry.id), name: entry.id, enabled: true,
      capabilities: { ...old.capabilities, tools: entry.tools,
        input: entry.vision === undefined ? old.capabilities.input : entry.vision
          ? [...new Set([...old.capabilities.input, "image" as const])]
          : old.capabilities.input.filter((modality) => !modality.startsWith("image")),
      },
      limit: { ...old.limit, context: entry.context_window > 0 ? entry.context_window : old.limit.context },
    }
  })]
}

export const hasPrimaryTask = (sessionID: string) => primary.has(sessionID)
export function beforeTool<A, E, R>(provider: string, operation: Effect.Effect<A, E, R>) {
  return enabled && provider === "clawmanager" ? headers.pipe(Effect.andThen(operation)) : operation
}
export const Governance = { enabled, headers, withPrimary, withAuxiliary, guardSession, settleSession, projectCatalog, catalog, hasPrimaryTask, beforeTool, pollMaintenance, admit, jobStarted }
