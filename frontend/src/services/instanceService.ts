import api from "./api";
import type {
  Instance,
  InstanceListResponse,
  CreateInstanceRequest,
  UpdateInstanceRequest,
  InstanceStatus,
  InstanceRuntimeDetails,
  InstanceConfigRevision,
  ExternalAccessStatusResult,
  EnableShareLinkResult,
  PasswordExternalAccessResult,
  ExternalAccessRequest,
  BatchCreateLiteInstancesRequest,
  BatchCreateLiteInstancesResponse,
  BatchDeleteLiteInstancesResponse,
} from "../types/instance";
import type { InstanceSkill } from "../types/skill";

export const instanceService = {
  // Get instance list
  getInstances: async (
    page: number = 1,
    limit: number = 20,
  ): Promise<InstanceListResponse> => {
    const response = await api.get("/instances", {
      params: { page, limit },
    });
    return response.data.data;
  },

  // Create instance
  createInstance: async (data: CreateInstanceRequest): Promise<Instance> => {
    const response = await api.post("/instances", data);
    return response.data.data;
  },

  batchCreateLiteInstances: async (
    data: BatchCreateLiteInstancesRequest,
  ): Promise<BatchCreateLiteInstancesResponse> => {
    const response = await api.post("/instances/batch/lite", data);
    return response.data.data;
  },

  batchDeleteLiteInstances: async (
    instanceIds: number[],
  ): Promise<BatchDeleteLiteInstancesResponse> => {
    const response = await api.post("/instances/batch/delete", {
      instance_ids: instanceIds,
    });
    return response.data.data;
  },
  // Get instance by ID
  getInstance: async (id: number): Promise<Instance> => {
    const response = await api.get(`/instances/${id}`);
    return response.data.data.instance;
  },

  // Update instance
  updateInstance: async (
    id: number,
    data: UpdateInstanceRequest,
  ): Promise<void> => {
    await api.put(`/instances/${id}`, data);
  },

  // Delete instance
  deleteInstance: async (id: number): Promise<void> => {
    await api.delete(`/instances/${id}`);
  },

  // Start instance
  startInstance: async (id: number): Promise<void> => {
    await api.post(`/instances/${id}/start`);
  },

  // Stop instance
  stopInstance: async (id: number): Promise<void> => {
    await api.post(`/instances/${id}/stop`);
  },

  // Restart instance
  restartInstance: async (id: number): Promise<void> => {
    await api.post(`/instances/${id}/restart`);
  },

  // Force sync instance status
  forceSyncInstance: async (id: number): Promise<void> => {
    await api.post(`/instances/${id}/sync`);
  },

  // Get instance status
  getInstanceStatus: async (id: number): Promise<InstanceStatus> => {
    const response = await api.get(`/instances/${id}/status`);
    return response.data.data.instance_status;
  },

  getRuntimeDetails: async (id: number): Promise<InstanceRuntimeDetails> => {
    const response = await api.get(`/instances/${id}/runtime`);
    return response.data.data;
  },

  createRuntimeCommand: async (
    id: number,
    command:
      | "start"
      | "stop"
      | "restart"
      | "collect-system-info"
      | "health-check",
    idempotencyKey?: string,
  ): Promise<void> => {
    await api.post(
      `/instances/${id}/runtime/${command}`,
      idempotencyKey ? { idempotency_key: idempotencyKey } : {},
    );
  },

  listConfigRevisions: async (
    id: number,
    limit: number = 20,
  ): Promise<InstanceConfigRevision[]> => {
    const response = await api.get(`/instances/${id}/config/revisions`, {
      params: { limit },
    });
    return response.data.data;
  },

  publishConfigRevision: async (
    id: number,
    snapshotId: number,
  ): Promise<{
    revision: InstanceConfigRevision;
    command: unknown;
  }> => {
    const response = await api.post(
      `/instances/${id}/config/revisions/publish`,
      {
        snapshot_id: snapshotId,
      },
    );
    return response.data.data;
  },

  // Generate access token
  generateAccessToken: async (
    id: number,
  ): Promise<{
    token: string;
    access_url: string;
    proxy_url: string;
    expires_at: string;
    desktop_proxy_mode?: "control-plane" | "fallback" | "direct";
    desktop_upstream_present?: boolean;
  }> => {
    const response = await api.post(`/instances/${id}/access`);
    return response.data.data;
  },

  // Access instance with token
  getAccessUrl: (id: number, token: string): string => {
    return `/api/v1/instances/${id}/access?token=${token}`;
  },

  getExternalAccess: async (id: number): Promise<ExternalAccessStatusResult> => {
    const response = await api.get(`/instances/${id}/external-access`);
    return response.data.data;
  },

  enableExternalShareLink: async (
    id: number,
    data: ExternalAccessRequest,
  ): Promise<EnableShareLinkResult> => {
    const response = await api.post(
      `/instances/${id}/external-access/share-link`,
      data,
    );
    return response.data.data;
  },

  createExternalAccessPassword: async (
    id: number,
    data: ExternalAccessRequest,
  ): Promise<PasswordExternalAccessResult> => {
    const response = await api.post(
      `/instances/${id}/external-access/password`,
      data,
    );
    return response.data.data;
  },

  disableExternalAccess: async (id: number): Promise<void> => {
    await api.delete(`/instances/${id}/external-access`);
  },

  exportOpenClawWorkspace: async (id: number): Promise<Blob> => {
    const response = await api.get(`/instances/${id}/openclaw/export`, {
      responseType: "blob",
    });
    return response.data;
  },

  importOpenClawWorkspace: async (id: number, file: File): Promise<void> => {
    const formData = new FormData();
    formData.append("file", file);
    await api.post(`/instances/${id}/openclaw/import`, formData, {
      headers: {
        "Content-Type": "multipart/form-data",
      },
    });
  },

  exportHermesWorkspace: async (id: number): Promise<Blob> => {
    const response = await api.get(`/instances/${id}/hermes/export`, {
      responseType: "blob",
    });
    return response.data;
  },

  importHermesWorkspace: async (id: number, file: File): Promise<void> => {
    const formData = new FormData();
    formData.append("file", file);
    await api.post(`/instances/${id}/hermes/import`, formData, {
      headers: {
        "Content-Type": "multipart/form-data",
      },
    });
  },

  listSkills: async (id: number): Promise<InstanceSkill[]> => {
    const response = await api.get(`/instances/${id}/skills`);
    return response.data.data;
  },
};
