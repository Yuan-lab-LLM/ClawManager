import type {
  AgentStatus,
  BaselineCategory,
  BaselineStatus,
  FilePolicy,
  InvasionPolicy,
  LogEntry,
  RansomPolicy,
} from '../types/hostHardening';

// All endpoints same-origin via Nginx (or vite dev proxy → ksec-bridge :9101).
// MVP: no auth headers — bridge has KSEC_BRIDGE_AUTH_ENABLED=false.
const base = '/api/host';

async function jsonOrThrow<T>(res: Response): Promise<T> {
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    const detail =
      typeof (body as { details?: unknown }).details === 'string'
        ? (body as { details: string }).details
        : '';
    const msg = (body as { error?: string }).error ?? `HTTP ${res.status}`;
    throw new Error(detail ? `${msg}: ${detail}` : msg);
  }
  return res.json() as Promise<T>;
}

export async function getStatus(): Promise<AgentStatus> {
  return jsonOrThrow(await fetch(`${base}/status`));
}

export async function getRansomPolicy(): Promise<RansomPolicy> {
  return jsonOrThrow(await fetch(`${base}/policy/ransome`));
}

/** PUT result. `warning` set when the master switch flipped OK but policy load failed. */
export interface PutResult {
  success: boolean;
  warning?: string;
}

export async function putRansomPolicy(pol: RansomPolicy): Promise<PutResult> {
  return jsonOrThrow<PutResult>(
    await fetch(`${base}/policy/ransome`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(pol),
    }),
  );
}

export async function getFilePolicy(): Promise<FilePolicy> {
  return jsonOrThrow(await fetch(`${base}/policy/file`));
}

export async function putFilePolicy(pol: FilePolicy): Promise<PutResult> {
  return jsonOrThrow<PutResult>(
    await fetch(`${base}/policy/file`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(pol),
    }),
  );
}

export async function getInvasionPolicy(): Promise<InvasionPolicy> {
  return jsonOrThrow(await fetch(`${base}/policy/invasion`));
}

/**
 * PUT /policy/invasion — 与 KSecGUI Invasion.vue setPolicy() 对齐：
 * 前端用 invasionPolicy.ts 模板 + 当前 state 构造完整 Falco YAML body 后发送，
 * 后端 yaml.dump 整文件落盘到 /opt/KSec/policy/ids.yaml。
 */
export async function putInvasionPolicy(payload: {
  'switch-on': boolean;
  ymlBody: unknown[];
}): Promise<PutResult> {
  return jsonOrThrow<PutResult>(
    await fetch(`${base}/policy/invasion`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    }),
  );
}

export async function getLogs(module: string, limit = 50): Promise<LogEntry[]> {
  return jsonOrThrow(
    await fetch(
      `${base}/logs?module=${encodeURIComponent(module)}&limit=${limit}`,
    ),
  );
}

/** Open an EventSource on `/api/host/logs/stream`. Caller must `close()` on unmount. */
export function openLogStream(module: string): EventSource {
  return new EventSource(`${base}/logs/stream?module=${encodeURIComponent(module)}`);
}

// ===== 合规检测 (CIS baseline) =====

export async function getBaselinePolicy(): Promise<BaselineCategory[]> {
  return jsonOrThrow(await fetch(`${base}/policy/baseline`));
}

export async function getBaselineStatus(): Promise<BaselineStatus> {
  return jsonOrThrow(await fetch(`${base}/baseline/status`));
}

export async function scanBaseline(itemIds: string[]): Promise<void> {
  await jsonOrThrow(
    await fetch(`${base}/baseline/scan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ itemIds }),
    }),
  );
}

export async function repairBaseline(): Promise<void> {
  await jsonOrThrow(
    await fetch(`${base}/baseline/repair`, { method: 'POST' }),
  );
}

export async function rollbackBaseline(): Promise<void> {
  await jsonOrThrow(
    await fetch(`${base}/baseline/rollback`, { method: 'POST' }),
  );
}

/** 把状态机重置回 'home'（清空 /opt/KSec/compliance/log），用于"重新检测" */
export async function resetBaseline(): Promise<void> {
  await jsonOrThrow(
    await fetch(`${base}/baseline/reset`, { method: 'POST' }),
  );
}
