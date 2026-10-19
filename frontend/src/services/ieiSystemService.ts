import axios from "axios";
import type { WorkspaceFileOperations } from "../components/WorkspaceFileManager";
import type { WorkspaceEntry, WorkspacePreview, WorkspaceSearchResponse } from "../types/workspace";

export interface IEISystemSession {
  owner: string;
  expires_at: string;
}

export interface OwnerDashboardData {
  growth?:OwnerGrowth;
  snapshot?:{version:number;as_of:string|null;next_refresh_at:string;state:string};
	 recent_questions?: Record<string,{text:string;at:string;checked_at:string}>;
	insights?: OwnerInsights;
  summary: { prompt_tokens:number; completion_tokens:number; total_tokens:number; sessions:number; questions:number|null; workday_streak:number|null };
  trend: {date:string;tokens:number}[];
  last_activity: Record<string,string>;
  coverage: {questions_supported:boolean;partial:boolean;description:string;ready_histories:number;unavailable_histories:number};
  timezone:string;
  range:string;
}

export interface UsageDay { date:string; tokens:number; prompt_tokens:number; completion_tokens:number; questions:number; sessions:number; observed:boolean; in_statistical_range?:boolean }
export interface MetricComparison {current:number;previous:number;delta:number;percent:number|null;status:'insufficient'|'new'|'flat'|'change';window?:OwnerInsights['window']}
export interface OwnerInsights {days:UsageDay[];window:{kind:'day'|'week'|'month';start:string;end:string;previous_start:string;previous_end:string};comparison:Record<string,MetricComparison>;first_observed:string|null;as_of:string;coverage_note:string}
export interface GrowthTask {code:string;name:string;period:'day'|'all';metric:string;target:number;progress:number;xp:number;awarded:boolean;reward?:string}
export interface OwnerGrowth {as_of?:string;xp:number;level:number;level_start:number;next_level:number;level_thresholds?:number[];started_at:string|null;tasks:GrowthTask[];ledger:{name:string;xp:number;at:string}[];days:{date:string;xp:number}[]|null;rule:string;note:string;open_level?:number;daily_limit?:number;today_xp?:number;effective_at?:string;upcoming_tasks?:GrowthTask[];ranking?:{rank:number|null;participants:number;tokens:number;start:string;end:string}}
export interface OwnerReport {comparison?:Record<string,{status:string;current:number;previous:number;percent:number|null}>;kind:'week'|'month';start:string;end:string;as_of:string;ongoing:boolean;counts:Record<string,number>;previous:Record<string,number>;days:UsageDay[];runtime:{runtime:string;tokens:number;questions:number}[];active_days:number;observed_days:number;instances:number;used_sessions:number;revision:number;saved_revisions:number[];note:string}

export interface IEISystemInstance {
	workspace_available?: boolean;
	workspace_root?: string;
  id: number;
  owner: string;
  name: string;
  alias?: string;
  description?: string;
  type: string;
  runtime_type: string;
  runtime_variant?: "linux" | "windows" | string;
  instance_mode: string;
  status: string;
  created_at: string;
  updated_at: string;
  started_at?: string;
}

export interface IEISystemInstanceList {
  instances: IEISystemInstance[];
  total: number;
  page: number;
  limit: number;
}

export interface IEISystemInstanceAccess {
  access_url: string;
  expires_at: string;
  desktop_proxy_mode?: "control-plane" | "fallback" | "direct";
  desktop_upstream_present?: boolean;
  workspace_available: boolean;
  workspace_root: string;
}

export interface IEISystemLifecycleOperation {
  operation_id: string;
  action: "restart" | "reset";
  status: "queued" | "processing" | "succeeded" | "failed" | string;
  instance_id?: number;
  error_code?: string;
  error_message?: string;
  created_at: string;
  started_at?: string;
  finished_at?: string;
  updated_at: string;
}

const API_BASE_URL = import.meta.env.VITE_API_URL || "/api/v1";

const ieiAPI = axios.create({
  baseURL: `${API_BASE_URL.replace(/\/+$/, "")}/ieisystem`,
  headers: { "Content-Type": "application/json" },
  withCredentials: true,
});

function lifecycleIdempotencyKey() {
  if (typeof crypto.randomUUID === "function") return `iei-${crypto.randomUUID()}`;
  return `iei-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

export const ieiSystemService = {
  async recordVisit():Promise<void>{await ieiAPI.post('/visit',{}, {headers:{'X-Owner-Action':'1'}})},
  async getGrowth(sync=false,visit=false):Promise<OwnerGrowth>{
    const response=sync?await ieiAPI.post('/growth/sync',{}, {params:{visit},headers:{'X-Owner-Action':'1'}}):await ieiAPI.get('/growth');return response.data.data;
  },
  async getReport(kind:string,date:string,save=false,revision?:number):Promise<OwnerReport>{
    const response=save?await ieiAPI.post('/reports',{}, {params:{kind,date},headers:{'X-Owner-Action':'1'}}):await ieiAPI.get('/reports',{params:{kind,date,revision}});return response.data.data;
  },
  async activity(id:number,stream:string,sequence:number,active:boolean):Promise<void>{await ieiAPI.post(`/instances/${id}/activity`,{stream,sequence,active},{headers:{'X-Owner-Action':'1'}})},
  async getDashboard(range='7d'): Promise<OwnerDashboardData> {
    const response=await ieiAPI.get('/dashboard',{params:{range}});
    return response.data.data;
  },
  async exchangeSession(token: string): Promise<IEISystemSession> {
    const response = await ieiAPI.post("/session", { token });
    return response.data.data;
  },

  async getSession(): Promise<IEISystemSession> {
    const response = await ieiAPI.get("/session");
    return response.data.data;
  },

  async refreshSession(): Promise<IEISystemSession> {
    const response = await ieiAPI.post("/session/refresh");
    return response.data.data;
  },

  async clearSession(): Promise<void> {
    await ieiAPI.delete("/session");
  },

  async listInstances(page = 1, limit = 100): Promise<IEISystemInstanceList> {
    const response = await ieiAPI.get("/instances", { params: { page, limit } });
    return response.data.data;
  },

  async getInstance(id: number): Promise<IEISystemInstance> {
    const response = await ieiAPI.get(`/instances/${id}`);
    return response.data.data.instance;
  },

  async restartInstance(id: number, idempotencyKey = lifecycleIdempotencyKey()): Promise<IEISystemLifecycleOperation> {
    const response = await ieiAPI.post(
      `/instances/${id}/restart`,
      undefined,
      { headers: { "Idempotency-Key": idempotencyKey } },
    );
    return response.data.data.operation;
  },

  async resetInstance(id: number, idempotencyKey = lifecycleIdempotencyKey()): Promise<IEISystemLifecycleOperation> {
    const response = await ieiAPI.post(
      `/instances/${id}/reset`,
      { confirm_data_loss: true },
      { headers: { "Idempotency-Key": idempotencyKey } },
    );
    return response.data.data.operation;
  },

  async getLatestLifecycleOperation(id: number): Promise<IEISystemLifecycleOperation | null> {
    const response = await ieiAPI.get(`/instances/${id}/lifecycle-operation`);
    return response.data.data.operation ?? null;
  },

  async getLifecycleOperation(operationID: string): Promise<IEISystemLifecycleOperation> {
    const response = await ieiAPI.get(`/lifecycle-operations/${encodeURIComponent(operationID)}`);
    return response.data.data.operation;
  },

  async generateAccess(id: number): Promise<IEISystemInstanceAccess> {
    const response = await ieiAPI.post(`/instances/${id}/access`);
    return response.data.data;
  },
};

export const ieiSystemWorkspaceService: WorkspaceFileOperations = {
  async readText(instanceId, path) {
    const response = await ieiAPI.get(`/instances/${instanceId}/workspace/text`, { params: { path } });
    return response.data.data;
  },
  async saveText(instanceId, path, text, version) {
    const response = await ieiAPI.put(`/instances/${instanceId}/workspace/text`, { path, text, version }, {headers:{"X-Owner-Action":"1"}});
    return response.data.data;
  },
  async list(instanceId: number, path = ""): Promise<WorkspaceEntry[]> {
    const response = await ieiAPI.get(`/instances/${instanceId}/workspace/files`, {
      params: { path },
    });
    return response.data.data.entries;
  },

  async search(instanceId: number, query: string, limit?: number): Promise<WorkspaceSearchResponse> {
    const response = await ieiAPI.get(`/instances/${instanceId}/workspace/search`, {
      params: { q: query, ...(limit ? { limit } : {}) },
    });
    return response.data.data;
  },

  async preview(instanceId: number, path: string): Promise<WorkspacePreview> {
    const response = await ieiAPI.get(`/instances/${instanceId}/workspace/preview`, {
      params: { path },
    });
    return response.data.data.preview;
  },

  async previewBlob(instanceId: number, path: string): Promise<Blob> {
    const response = await ieiAPI.get(`/instances/${instanceId}/workspace/preview`, {
      params: { path, raw: 1 },
      responseType: "blob",
    });
    return response.data;
  },

  async downloadBlob(instanceId: number, path: string): Promise<Blob> {
    const response = await ieiAPI.get(`/instances/${instanceId}/workspace/download`, {
      params: { path },
      responseType: "blob",
    });
    return response.data;
  },

  async upload(instanceId: number, path: string, file: File): Promise<void> {
    const formData = new FormData();
    formData.append("file", file);
    await ieiAPI.post(`/instances/${instanceId}/workspace/upload`, formData, {
      params: { path },
      headers: { "Content-Type": "multipart/form-data" },
    });
  },

  async mkdir(instanceId: number, path: string): Promise<void> {
    await ieiAPI.post(`/instances/${instanceId}/workspace/folders`, { path });
  },

  async rename(instanceId: number, oldPath: string, newPath: string): Promise<void> {
    await ieiAPI.patch(`/instances/${instanceId}/workspace/entries`, {
      old_path: oldPath,
      new_path: newPath,
    });
  },

  async remove(instanceId: number, path: string): Promise<void> {
    await ieiAPI.delete(`/instances/${instanceId}/workspace/entries`, {
      params: { path },
    });
  },
};
