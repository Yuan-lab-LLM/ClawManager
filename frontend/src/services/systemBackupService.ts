import api from './api';

export type BackupTaskType = 'backup' | 'drill' | 'preflight' | 'artifact_verify';
export interface BackupTask {
  public_id: string; task_type: BackupTaskType; task_purpose: string; status: string;
  row_version: number; config_version: number; can_cancel: boolean;
  commit_started: boolean; artifact_registered: boolean; acceptance_eligible: boolean;
  failure_category: string; pending_reason: string; attention_reason: string;
  created_at: string; deadline_at: string; finished_at?: string;
  source_public_id?: string;
}
export interface BackupReadiness {
  contract_version: string; enabled: boolean; can_create: boolean;
  config_version: number; recovery_point_count: number;
  checks: Array<{name: string; passed: boolean; code: string; detail: string}>;
}
export interface BackupConfig {
  enabled: boolean; runner: string; profile: string; retention_seconds: number;
  minimum_recovery_points: number; gate_acquisition_timeout_seconds: number;
  quiesce_max_hold_seconds: number; task_deadline_seconds: number;
  max_artifact_bytes: number; staging_capacity_bytes: number; max_index_bytes: number;
  max_source_items: number;
  artifact_target?: SecretReference; encryption_key?: SecretReference;
  signing_key?: SecretReference; catalog?: SecretReference;
}
export interface SecretReference {provider: string; name: string; version: string; purpose: string}
export interface BackupConfigVersion {version: number; config: BackupConfig}
export interface BackupConfigChange {expected_version: number; config: BackupConfig; change_reason?: string}
export interface BackupAuthorizationBinding {aud: string; sub: string; session: string; endpoint: string; request_hash: string; idempotency_key: string}
export interface BackupEvent {id: number; event_type: string; details: Record<string, unknown>; created_at: string}
export interface BackupVerificationReport {
  backup_id: string; origin_installation_id: string; purpose: string; consistency: string;
  integrity_verified: boolean; acceptance_eligible: boolean; parts_verified: number;
}
const base = '/admin/system-backup';
export const systemBackupService = {
  overview: async (signal?: AbortSignal): Promise<{readiness: BackupReadiness; permissions: string[]}> => (await api.get(`${base}/overview`, {signal})).data.data,
  config: async (signal?: AbortSignal): Promise<BackupConfigVersion> => (await api.get(`${base}/config`, {signal})).data.data,
  saveConfig: async (version: number, config: BackupConfig, key: string, reason = '', proof = ''): Promise<BackupConfigVersion> => (await api.put(`${base}/config`, {expected_version: version, config, ...(reason ? {change_reason: reason} : {})}, {headers: {'Idempotency-Key': key, ...(proof ? {'X-System-Backup-Proof': proof} : {})}})).data.data,
  configAuthorization: async (input: BackupConfigChange, key: string): Promise<BackupAuthorizationBinding> => (await api.post(`${base}/config/authorization`, input, {headers: {'Idempotency-Key': key}})).data.data,
  list: async (signal?: AbortSignal): Promise<BackupTask[]> => (await api.get(`${base}/tasks`, {signal})).data.data.items,
  get: async (id: string, signal?: AbortSignal): Promise<BackupTask> => (await api.get(`${base}/tasks/${encodeURIComponent(id)}`, {signal})).data.data,
  events: async (id: string, signal?: AbortSignal): Promise<BackupEvent[]> => (await api.get(`${base}/tasks/${encodeURIComponent(id)}/events`, {signal})).data.data.items,
  verificationReport: async (id: string, signal?: AbortSignal): Promise<BackupVerificationReport> => (await api.get(`${base}/tasks/${encodeURIComponent(id)}/verification-report`, {signal})).data.data,
  create: async (key: string, task_type: BackupTaskType, source_public_id?: string): Promise<BackupTask> => (await api.post(`${base}/tasks`, {task_type, task_purpose: 'acceptance', ...(source_public_id ? {source_public_id} : {})}, {headers: {'Idempotency-Key': key}})).data.data,
  cancel: async (task: BackupTask, key: string): Promise<BackupTask> => (await api.post(`${base}/tasks/${encodeURIComponent(task.public_id)}/cancel`, {expected_version: task.row_version}, {headers: {'Idempotency-Key': key}})).data.data,
};

export function backupErrorCode(error: unknown): string {
  const e = error as {response?: {data?: {code?: string}}};
  return e.response?.data?.code ?? 'network_error';
}
export function terminalBackupTask(task: BackupTask): boolean {
  return ['succeeded', 'failed', 'canceled'].includes(task.status);
}
