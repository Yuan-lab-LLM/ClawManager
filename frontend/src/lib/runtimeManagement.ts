import type { RuntimePod, RuntimeSlot, RuntimeSchedulingOperation } from "../types/runtimePool";

export type SlotState =
  | "running"
  | "starting"
  | "migrating"
  | "error"
  | "idle"
  | "unknown"
  | "unavailable";

export function slotState(slot: RuntimeSlot): SlotState {
  if (slot.reconciliation_state === "unknown") return "unknown";
  if (slot.reconciliation_state === "residual" || slot.reconciliation_state === "conflict") return "error";
  if (!slot.ready) return "unavailable";
  if (!slot.instance_id) return "idle";
  const state = slot.instance_state ?? "";
  if (["running", "ready", "active"].includes(state)) return "running";
  if (["starting", "creating", "pending", "reserved"].includes(state))
    return "starting";
  if (["migrating", "upgrading", "draining", "stopping"].includes(state))
    return "migrating";
  return "error";
}

export function canManageSlot(slot: RuntimeSlot) {
  return !!slot.instance_id && (slot.reconciliation_state === undefined
    ? slot.binding_confirmed !== false
    : slot.reconciliation_state === "consistent" && slot.binding_confirmed === true);
}

// A delayed stop can leave a SQL reservation on a physically empty slot.
// This projection is only for retrying its original failed move; server preflight
// still revalidates the binding, generation and stop proof before any mutation.
export function failedMoveRetrySelections(pods: RuntimePod[], operation?: RuntimeSchedulingOperation) {
  if (!operation || !["failed", "partial_failure"].includes(operation.status) ||
      !["migration", "balance", "eviction"].includes(operation.kind ?? "")) return [];
  return operation.items.flatMap(item => {
    if (item.state !== "failed" || item.recovery || item.orphan_recovery ||
        !item.source_pod_uid || !item.source_generation ||
        item.source_gateway_id !== `gw-${item.instance_id}-${item.source_generation}`) return [];
    const source = pods.find(pod => pod.id === item.source_pod_id && pod.pod_uid === item.source_pod_uid);
    if (!source || source.agent_reported !== true || source.bindings_reported !== true) return [];
    const slot = source.slots?.find(slot => slot.ready && !slot.instance_id &&
      slot.reconciliation_state === "conflict" && slot.expected_instance_id === item.instance_id);
    return slot ? [{ source, slot: { ...slot, instance_id: item.instance_id,
      generation: item.source_generation, gateway_id: item.source_gateway_id } }] : [];
  });
}

export function recoveryInstanceIds(pod: RuntimePod): number[] {
  return [...new Set((pod.slots ?? []).flatMap((slot) => slot.expected_instance_id ? [slot.expected_instance_id] : canManageSlot(slot) ? [slot.instance_id!] : []))];
}

export function canEvictPod(pod: RuntimePod) {
  const slots = pod.slots ?? [];
  return pod.id > 0 && pod.agent_reported !== false && pod.bindings_reported !== false &&
    (pod.reconciliation_state === undefined || pod.reconciliation_state === "consistent") &&
    !pod.unknown_slots && slots.some(canManageSlot) &&
    slots.every((slot) => slot.reconciliation_state === undefined || slot.reconciliation_state === "consistent") &&
    slots.filter((slot) => slot.instance_id).every(canManageSlot) &&
    slots.filter(canManageSlot).length >= pod.used_slots;
}

export function isSchedulable(pod: RuntimePod) {
  return (
    pod.id > 0 &&
    pod.agent_reported !== false &&
    pod.state === "ready" &&
    !pod.draining &&
    !pod.scheduling_paused &&
    pod.scheduling_enabled !== false &&
    pod.pool_purpose !== "openclaw-upgrade-lab" &&
    (!pod.pool_role ||
      (pod.pool_role === "upgrade-target" && pod.scheduling_enabled === true))
  );
}

export function canManageScheduling(pod: RuntimePod) {
  return (
    pod.id > 0 &&
    pod.agent_reported !== false &&
    !pod.draining &&
    pod.pool_purpose !== "openclaw-upgrade-lab" &&
    (!pod.pool_role ||
      (pod.pool_role === "upgrade-target" &&
        (pod.scheduling_enabled === true || pod.scheduling_paused === true)))
  );
}

export function summarizePod(pod: RuntimePod) {
  const slots = pod.slots ?? [];
  const count = (state: SlotState) =>
    slots.filter((slot) => slotState(slot) === state).length;
  const ready = slots.filter((slot) => slot.ready).length;
  const bound = slots.filter((slot) => slot.instance_id || slot.expected_instance_id).length;
  const occupied = Math.max(bound, pod.used_slots);
  const idle = count("idle");
  return {
    capacity: pod.capacity,
    ready,
    occupied,
    bound: pod.bound_instances,
    residual: slots.filter(slot => !!slot.instance_id && !slot.binding_confirmed && slot.instance_id !== slot.expected_instance_id && slot.reconciliation_state !== undefined).length,
    available: isSchedulable(pod) && pod.bindings_reported !== false && bound >= pod.used_slots ? idle : 0,
    running: count("running"),
    starting: count("starting"),
    migrating: count("migrating"),
    error: count("error"),
    idle,
    unknown: Math.max(count("unknown"), pod.unknown_slots ?? 0),
    unavailable: Math.max(0, pod.capacity - ready),
  };
}

export function filterPods(
  pods: RuntimePod[],
  filters: {
    type?: string;
    search?: string;
    node?: string;
    scheduling?: string;
  },
) {
  const needle = filters.search?.trim().toLowerCase();
  return pods.filter(
    (pod) =>
      (!filters.type ||
        filters.type === "all" ||
        pod.runtime_type === filters.type) &&
      (!filters.node ||
        filters.node === "all" ||
        pod.node_name === filters.node) &&
      (!filters.scheduling ||
        filters.scheduling === "all" ||
        isSchedulable(pod) === (filters.scheduling === "enabled")) &&
      (!needle ||
        [
          pod.pod_name,
          pod.node_name,
          pod.pod_ip,
          ...(pod.slots ?? []).flatMap((slot) => [
            slot.instance_name,
            slot.owner,
            slot.instance_id,
            slot.expected_instance_id,
          ]),
        ].some((value) =>
          String(value ?? "")
            .toLowerCase()
            .includes(needle),
        )),
  );
}

export function availableTargets(
  pods: RuntimePod[],
  source: RuntimePod | RuntimePod[],
) {
  const sources = Array.isArray(source) ? source : [source];
  // Controller names do not define workspace compatibility. Server preflight
  // checks storage, protocol, upgrade affinity and physical slot resources.
  return pods.filter(
    (pod) =>
      summarizePod(pod).available > 0 &&
      sources.length > 0 &&
      sources.every(
        (current) =>
          pod.id !== current.id &&
          pod.runtime_type === current.runtime_type &&
          !!current.namespace &&
          current.namespace === pod.namespace &&
          !!current.deployment_name &&
          !!pod.deployment_name &&
          !!current.image_ref &&
          current.image_ref === pod.image_ref,
      ),
  );
}

export function formatMemory(bytes?: number) {
  if (bytes === undefined || !Number.isFinite(bytes)) return "—";
  return bytes >= 1024 ** 3
    ? `${(bytes / 1024 ** 3).toFixed(1)} Gi`
    : `${Math.round(bytes / 1024 ** 2)} Mi`;
}

export function formatCPU(millis?: number) {
  return millis === undefined || !Number.isFinite(millis)
    ? "—"
    : (millis / 1000).toFixed(1);
}

export function runtimeError(err: unknown) {
  return (
    (err as { response?: { data?: { error?: string; message?: string } } })
      ?.response?.data?.error ||
    (err as { response?: { data?: { message?: string } } })?.response?.data
      ?.message ||
    (err instanceof Error ? err.message : String(err))
  );
}
