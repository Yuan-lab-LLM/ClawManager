import axios from 'axios';
import type { teamService } from './teamService';
import type { Team, TeamMember, TeamDetails, DispatchTeamTaskRequest, TeamTask, TeamTasksHistoryResponse, TeamEventsHistoryResponse, TeamWorkspaceListResponse, TeamWorkspacePreviewResponse } from '../types/team';
import type { WorkspaceText } from '../components/WorkspaceTextEditor';
import type { CustomTeamTemplate } from '../types/customTeamTemplate';

// Intentionally independent of the CM JWT client/interceptors.
export const ownerTeamHTTP = axios.create({ baseURL: '/api/v1/ieisystem', withCredentials: true });
ownerTeamHTTP.interceptors.response.use(r => r, error => {
  if (axios.isAxiosError(error)) error.message = error.response?.data?.message || error.response?.data?.error || error.message;
  return Promise.reject(error);
});
export type TeamWorkspaceAPI = Omit<typeof teamService, 'createTeam' | 'listTeams'>;
export interface OwnerTeamSummary extends Team { members: TeamMember[]; member_count: number; available_count: number; confirming_count?: number; fault_count?: number }
export interface TeamOperation { operation_id: string; status: string; attempt_count?: number; team_id?: number; template_id?: number; error_code?: string; error_message?: string }
export interface OwnerTeamCapabilities { enabled: boolean; can_create: boolean; create_disabled_reason?: string; runtime_types?: string[]; instance_modes?: string[] }
export interface OwnerTeamCatalogEntry { key: string; revision: number; name: string; description: string; communication_mode: string; members: { member_id: string; name: string; role: string; is_leader: boolean; runtime_type: string; instance_mode: string }[] }

export function ownerTeamRequestKey(): string { return `owner-team-${crypto.randomUUID()}`; }
export class OwnerTeamOperationError extends Error {
  operation: TeamOperation;
  constructor(operation: TeamOperation) { super(operation.error_message || '操作未完成，现有资源已保留'); this.operation = operation; }
}

// Keep one intent through a lost HTTP response. A changed form must not silently
// create another operation while acceptance of the previous request is unknown.
export async function withOwnerTeamIntent<T extends object>(owner: string, action: string, data: T, send: (data: T, key: string) => Promise<TeamOperation>): Promise<TeamOperation> {
  const location = `owner-team-intent:${owner}:${action}`;
  const body = JSON.stringify(data);
  const previous = sessionStorage.getItem(location);
  const intent: { key: string; body: string } = previous ? JSON.parse(previous) : { key: ownerTeamRequestKey(), body };
  if (intent.body !== body) throw new Error('上次提交结果尚未确认，请先恢复上次提交并检查进度，避免重复创建。');
  sessionStorage.setItem(location, JSON.stringify(intent));
  try {
    const op = await send(data, intent.key);
    sessionStorage.setItem(`owner-team-operation:${owner}`, JSON.stringify(op));
    sessionStorage.removeItem(location);
    return op;
  } catch (error) {
    // A definitive validation/authorization response did not accept the intent.
    if (axios.isAxiosError(error) && error.response && [400,401,403,404,409,422,429].includes(error.response.status)) sessionStorage.removeItem(location);
    throw error;
  }
}

export function pendingOwnerTeamIntent(owner: string): string | undefined {
  const prefix = `owner-team-intent:${owner}:`;
  return Object.keys(sessionStorage).find(key => key.startsWith(prefix))?.slice(prefix.length);
}
export async function recoverOwnerTeamIntent(owner: string, action: string): Promise<TeamOperation> {
  const saved = JSON.parse(sessionStorage.getItem(`owner-team-intent:${owner}:${action}`) || 'null');
  if (!saved?.body) throw new Error('没有待检查的提交');
  const data = JSON.parse(saved.body);
  return withOwnerTeamIntent(owner, action, data, (payload, key) => {
    if (action === 'create') return ownerTeamService.create(payload, key);
    const match = /^template:(new|\d+):(.*)$/.exec(action);
    if (!match) throw new Error('无法识别提交类型');
    return match[1] === 'new' ? ownerTeamService.generateTemplate(payload, key) : ownerTeamService.templateAction(Number(match[1]), match[2], payload, key);
  });
}
async function get<T>(url: string, params?: object, signal?: AbortSignal): Promise<T> { return (await ownerTeamHTTP.get(url, { params, signal })).data.data; }
async function post<T>(url: string, data: unknown, key?: string): Promise<T> { return (await ownerTeamHTTP.post(url, data, key ? { headers: { 'Idempotency-Key': key } } : undefined)).data.data; }

export const ownerTeamService = {
  capabilities: (signal?: AbortSignal) => get<OwnerTeamCapabilities>('/team-capabilities', undefined, signal),
  list: (page = 1, limit = 100, q = '', signal?: AbortSignal) => get<{ teams: OwnerTeamSummary[]; total: number; page: number; limit: number }>('/teams', { page, limit, q }, signal),
  catalog: (signal?: AbortSignal) => get<{ templates: OwnerTeamCatalogEntry[] }>('/team-templates', undefined, signal),
  create: (data: { name: string; description?: string; template_key?: string; template_id?: number; revision: number; members?: { member_id: string; runtime_type: string; instance_mode: string }[] }, key: string) => post<TeamOperation>('/teams', data, key),
  operation: (id: string, signal?: AbortSignal) => get<TeamOperation>(`/team-operations/${encodeURIComponent(id)}`, undefined, signal),
  retryOperation: (id: string, expected_attempt_count: number) => post<TeamOperation>(`/team-operations/${encodeURIComponent(id)}/retry`, { expected_attempt_count }),
  removeTeam: async (id: number) => (await ownerTeamHTTP.delete(`/teams/${id}`, { data: { confirm_data_loss: true }, headers: { 'Idempotency-Key': ownerTeamRequestKey() } })).data.data as TeamOperation,
  removeMember: async (id: number, member: string) => (await ownerTeamHTTP.delete(`/teams/${id}/members/${encodeURIComponent(member)}`, { data: { confirm_data_loss: true }, headers: { 'Idempotency-Key': ownerTeamRequestKey() } })).data.data as TeamOperation,
  update: async (id: number, data: { name: string; description?: string }) => { await ownerTeamHTTP.patch(`/teams/${id}`, data); },
  templates: (signal?: AbortSignal) => get<{ templates: CustomTeamTemplate[] }>('/custom-team-templates', undefined, signal),
  template: (id: number, signal?: AbortSignal) => get<CustomTeamTemplate>(`/custom-team-templates/${id}`, undefined, signal),
  generateTemplate: (data: { intent: string; member_count?: number }, key: string) => post<TeamOperation>('/custom-team-templates', data, key),
  updateTemplate: async (id: number, name: string, expected_revision: number) => (await ownerTeamHTTP.patch(`/custom-team-templates/${id}`, { name, expected_revision })).data.data as CustomTeamTemplate,
  deleteTemplate: async (id: number) => { await ownerTeamHTTP.delete(`/custom-team-templates/${id}`); },
  templateAction: (id: number, action: string, data: object, key: string) => post<TeamOperation>(`/custom-team-templates/${id}/${action}`, data, key),
  templateIcon: async (id: number, member: string, data: { icon_override: string; expected_revision: number }) => (await ownerTeamHTTP.put(`/custom-team-templates/${id}/members/${encodeURIComponent(member)}/icon`, data)).data.data as CustomTeamTemplate,
};

// Shared CM workspace receives this adapter; no iframe or CM login is involved.
export const ownerTeamWorkspaceAPI: TeamWorkspaceAPI = {
  getTeam: (id: number) => get<TeamDetails>(`/teams/${id}`),
  getTeamTasks: (id: number, before_id?: number, limit = 20) => get<TeamTasksHistoryResponse>(`/teams/${id}/tasks`, { before_id, limit }),
  getTeamEvents: (id: number, before_id?: number, limit = 50) => get<TeamEventsHistoryResponse>(`/teams/${id}/events`, { before_id, limit }),
  dispatchTask: (id: number, data: DispatchTeamTaskRequest) => post<TeamTask>(`/teams/${id}/tasks`, data),
  deleteTeam: async (id: number) => {
    const op = (await ownerTeamHTTP.delete(`/teams/${id}`, { data: { confirm_data_loss: true }, headers: { 'Idempotency-Key': ownerTeamRequestKey() } })).data.data as TeamOperation;
    await waitForOwnerTeamOperation(op.operation_id);
  },
  deleteMember: async (id: number, member: number | string) => {
    const op = (await ownerTeamHTTP.delete(`/teams/${id}/members/${encodeURIComponent(member)}`, { data: { confirm_data_loss: true }, headers: { 'Idempotency-Key': ownerTeamRequestKey() } })).data.data as TeamOperation;
    await waitForOwnerTeamOperation(op.operation_id);
  },
  listWorkspaceFiles: (id: number, path = '', signal?: AbortSignal) => get<TeamWorkspaceListResponse>(`/teams/${id}/workspace/files`, { path }, signal),
  previewWorkspaceFile: (id: number, path: string, signal?: AbortSignal) => get<TeamWorkspacePreviewResponse>(`/teams/${id}/workspace/preview`, { path }, signal),
  downloadWorkspaceFile: async (id: number, path: string) => (await ownerTeamHTTP.get<Blob>(`/teams/${id}/workspace/download`, { params: { path }, responseType: 'blob' })).data,
  readText: (id: number, path: string, signal?: AbortSignal) => get<WorkspaceText>(`/teams/${id}/workspace/text`, { path }, signal),
  saveText: async (id, path, text, version) => (await ownerTeamHTTP.put(`/teams/${id}/workspace/text`, { path, text, version })).data.data as WorkspaceText,
  createWorkspaceFolder: async (id, data) => { await ownerTeamHTTP.post(`/teams/${id}/workspace/folders`, data); },
  renameWorkspaceEntry: async (id, data) => { await ownerTeamHTTP.patch(`/teams/${id}/workspace/entries`, data); },
  deleteWorkspaceEntry: async (id, path) => { await ownerTeamHTTP.delete(`/teams/${id}/workspace/entries`, { params: { path } }); },
  uploadWorkspaceFiles: async (id, path, files, relativePaths) => {
    const data = new FormData(); data.append('path', path);
    files.forEach((file, i) => { data.append('files', file); data.append('relative_paths', relativePaths?.[i] || file.name); });
    await ownerTeamHTTP.post(`/teams/${id}/workspace/upload`, data);
  },
};

export async function waitForOwnerTeamOperation(id: string, signal?: AbortSignal): Promise<TeamOperation> {
  const deadline = Date.now() + 5 * 60_000;
  while (!signal?.aborted && Date.now() < deadline) {
    const operation = await ownerTeamService.operation(id, signal);
    if (operation.status === 'succeeded') return operation;
    if (operation.status === 'failed') throw new OwnerTeamOperationError(operation);
    await new Promise<void>((resolve, reject) => { const timer = window.setTimeout(() => { signal?.removeEventListener('abort', abort); resolve(); }, 1500); const abort = () => { clearTimeout(timer); reject(new DOMException('Aborted', 'AbortError')); }; signal?.addEventListener('abort', abort, { once: true }); });
  }
  throw new Error(`操作仍在后台处理，请稍后查看记录：${id}；不要重复创建。`);
}
