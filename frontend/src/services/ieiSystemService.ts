import axios from "axios";
import type { WorkspaceFileOperations } from "../components/WorkspaceFileManager";
import type { WorkspaceEntry, WorkspacePreview, WorkspaceSearchResponse } from "../types/workspace";

export interface IEISystemSession {
  owner: string;
  expires_at: string;
}

export interface IEISystemInstance {
  id: number;
  owner: string;
  name: string;
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
  async list(instanceId: number, path = ""): Promise<WorkspaceEntry[]> {
    const response = await ieiAPI.get(`/instances/${instanceId}/workspace/files`, {
      params: { path },
    });
    return response.data.data.entries;
  },

  async search(instanceId: number, query: string): Promise<WorkspaceSearchResponse> {
    const response = await ieiAPI.get(`/instances/${instanceId}/workspace/search`, {
      params: { q: query },
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
