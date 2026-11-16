import type { Instance, InstanceListResponse } from "../types/instance";

export interface InstanceListView {
  query: string;
  page: number;
  scrollTop: number;
}

const targetKey = "cm:instance-navigation:target:v1";
const viewKey = (userId: number) => `cm:instance-navigation:view:v1:${userId}`;
const lastKey = (userId: number) => `cm:instance-navigation:last:v1:${userId}`;
const listKey = (userId: number) => `cm:instance-navigation:list:v1:${userId}`;
const targetTTL = 15_000;
const validId = (value: unknown): value is number =>
  typeof value === "number" && Number.isSafeInteger(value) && value > 0;
const record = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);
const text = (value: unknown): value is string => typeof value === "string" && value.length <= 512;
const workspacePath = (value: unknown): value is string => text(value)
  && (value === "" || (value.startsWith("/") && !value.startsWith("//")
    && !/[?#%\\=&:]/.test(value) && ![...value].some(char => char.charCodeAt(0) < 32 || char.charCodeAt(0) === 127) && !/(^|\/)\.\.?(\/|$)/.test(value)));
const nonnegative = (value: unknown): value is number =>
  typeof value === "number" && Number.isFinite(value) && value >= 0;
const member = (value: unknown, options: readonly string[]) =>
  typeof value === "string" && options.includes(value);
const defaultView = (): InstanceListView => ({ query: "", page: 1, scrollTop: 0 });

function read(key: string, userId: number): Record<string, unknown> | null {
  if (!validId(userId)) return null;
  try {
    const value: unknown = JSON.parse(window.sessionStorage.getItem(key) ?? "null");
    return record(value) && value.version === 1 && value.userId === userId ? value : null;
  } catch {
    return null;
  }
}

function write(key: string, userId: number, value: Record<string, unknown>): void {
  if (!validId(userId)) return;
  try {
    window.sessionStorage.setItem(key, JSON.stringify({ ...value, version: 1, userId }));
  } catch {
    // Navigation still works when storage is blocked or full.
  }
}

function safeInstance(value: unknown): Instance | null {
  if (!record(value) || !validId(value.id) || !validId(value.user_id)) return null;
  if (!["name", "os_type", "os_version", "storage_class", "created_at", "updated_at"]
    .every(key => text(value[key]))) return null;
  if (!workspacePath(value.mount_path)
    || (value.workspace_path !== undefined && !workspacePath(value.workspace_path))) return null;
  if (!["cpu_cores", "memory_gb", "disk_gb", "gpu_count"]
    .every(key => nonnegative(value[key])) || typeof value.gpu_enabled !== "boolean") return null;
  if (!member(value.type, ["openclaw", "ubuntu", "debian", "centos", "custom", "webtop",
    "hermes", "opencode", "workbuddy", "deepseek-harness", "codex", "claude-code"])
    || !member(value.runtime_type, ["desktop", "shell", "gateway"])
    || !member(value.instance_mode, ["lite", "pro"])
    || !member(value.status, ["creating", "running", "stopped", "error", "deleting"])) return null;
  if ((value.runtime_variant !== undefined && !member(value.runtime_variant, ["linux", "windows"]))
    || (value.stopped_reason != null && value.stopped_reason !== "" && !member(value.stopped_reason, ["manual", "idle_timeout"]))
    || (value.availability !== undefined && !member(value.availability, ["available", "starting", "unavailable"]))
    || (value.desktop_stream_profile !== undefined && !member(value.desktop_stream_profile, ["low", "standard", "high"]))
    || (value.runtime_generation !== undefined && (!nonnegative(value.runtime_generation)
      || !Number.isSafeInteger(value.runtime_generation)))) return null;

  const instance = value as unknown as Instance;
  // Only display/runtime metadata crosses a document reload. This is never an
  // access grant; the destination must fetch permissions and live metadata.
  return {
    id: instance.id,
    user_id: instance.user_id,
    name: instance.name,
    type: instance.type,
    runtime_type: instance.runtime_type,
    instance_mode: instance.instance_mode,
    status: instance.status,
    stopped_reason: instance.stopped_reason || undefined,
    cpu_cores: instance.cpu_cores,
    memory_gb: instance.memory_gb,
    disk_gb: instance.disk_gb,
    gpu_enabled: instance.gpu_enabled,
    gpu_count: instance.gpu_count,
    os_type: instance.os_type,
    os_version: instance.os_version,
    storage_class: instance.storage_class,
    mount_path: instance.mount_path,
    workspace_path: instance.workspace_path,
    created_at: instance.created_at,
    updated_at: instance.updated_at,
    runtime_variant: instance.runtime_variant,
    availability: instance.availability,
    desktop_stream_profile: instance.desktop_stream_profile,
    runtime_generation: instance.runtime_generation,
  };
}

export function rememberInstanceTarget(userId: number, instance: Instance): void {
  const safe = safeInstance(instance);
  if (safe) write(targetKey, userId, { savedAt: Date.now(), instance: safe });
}

export function readInstanceTarget(userId: number, instanceId: number): Instance | null {
  if (!validId(instanceId)) return null;
  const value = read(targetKey, userId);
  const now = Date.now();
  if (!value || typeof value.savedAt !== "number" || !Number.isFinite(value.savedAt)
    || value.savedAt > now || now - value.savedAt >= targetTTL) return null;
  const instance = safeInstance(value.instance);
  // Repeatable reads support StrictMode initializers; only the latest target
  // remains eligible, and its short TTL bounds stale display data.
  return instance?.id === instanceId ? instance : null;
}

function safeList(value: unknown): InstanceListResponse | null {
  if (!record(value) || !Array.isArray(value.instances) || value.instances.length > 50
    || !validId(value.page) || !validId(value.limit) || value.limit > 50
    || !nonnegative(value.total) || !Number.isSafeInteger(value.total)
    || value.instances.length > value.limit || value.instances.length > value.total) return null;
  const instances = value.instances.map(safeInstance);
  if (instances.some(instance => instance === null)) return null;
  return { instances: instances as Instance[], total: value.total, page: value.page, limit: value.limit };
}

export function rememberInstanceList(userId: number, query: string, data: InstanceListResponse): void {
  const safe = safeList(data);
  // One bounded page per account; never persist credentials or an entire cache.
  if (text(query) && safe) write(listKey(userId), userId, { query, data: safe, savedAt: Date.now() });
}

export function readInstanceList(userId: number, query: string, page: number): { data: InstanceListResponse; savedAt: number } | null {
  if (!text(query) || !validId(page)) return null;
  const value = read(listKey(userId), userId);
  const now = Date.now();
  if (!value || value.query !== query || typeof value.savedAt !== "number" || !Number.isFinite(value.savedAt)
    || value.savedAt > now || now - value.savedAt >= targetTTL) return null;
  const data = safeList(value.data);
  return data?.page === page ? { data, savedAt: value.savedAt } : null;
}

function validView(value: unknown): value is InstanceListView {
  return record(value) && text(value.query) && validId(value.page) && nonnegative(value.scrollTop);
}

export function readInstanceListView(userId: number): InstanceListView {
  const value = read(viewKey(userId), userId);
  return validView(value)
    ? { query: value.query, page: value.page, scrollTop: value.scrollTop }
    : defaultView();
}

export function writeInstanceListView(userId: number, view: InstanceListView): void {
  if (validView(view)) write(viewKey(userId), userId,
    { query: view.query, page: view.page, scrollTop: view.scrollTop });
}

export function rememberLastInstance(userId: number, instanceId: number): void {
  if (validId(instanceId)) write(lastKey(userId), userId, { instanceId });
}

export function readLastInstance(userId: number): number | null {
  const value = read(lastKey(userId), userId);
  return validId(value?.instanceId) ? value.instanceId : null;
}

// Keep search/page/scroll preferences; discard deleted metadata and resume targets.
export function forgetInstance(userId: number, instanceId: number): void {
  if (!validId(userId) || !validId(instanceId)) return;
  try {
    if (readLastInstance(userId) === instanceId) window.sessionStorage.removeItem(lastKey(userId));
    if ((read(targetKey, userId)?.instance as { id?: number } | undefined)?.id === instanceId) window.sessionStorage.removeItem(targetKey);
    window.sessionStorage.removeItem(listKey(userId));
  } catch { /* Navigation still works without storage. */ }
}
