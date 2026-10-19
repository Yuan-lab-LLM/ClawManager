import axios from "axios";
import type { WorkspaceFileOperations } from "../components/WorkspaceFileManager";
import type { WorkspaceEntry, WorkspacePreview, WorkspaceSearchResponse } from "../types/workspace";

export type SharedWorkspaceAccess = "none" | "read" | "write";

export interface SharedInstanceSession {
  instance: {
    id: number;
    name: string;
    type: string;
    status: string;
    instance_mode?: string;
    runtime_type?: string;
  };
  access_url: string;
  session_expires_at: string;
  share_expires_at?: string;
  workspace_access: SharedWorkspaceAccess;
  workspace_available: boolean;
  workspace_root: string;
  csrf_token: string;
}

const API_BASE_URL = import.meta.env.VITE_API_URL || "/api/v1";

const sharedAPI = axios.create({
  baseURL: `${API_BASE_URL.replace(/\/+$/, "")}/shared-instances`,
  withCredentials: true,
});

function shareBase(code: string) {
  return `/${encodeURIComponent(code)}`;
}

export async function getSharedInstanceSession(code: string): Promise<SharedInstanceSession> {
  const response = await sharedAPI.get(`${shareBase(code)}/session`);
  return response.data.data;
}

export function createSharedWorkspaceService(
  code: string,
  csrfToken: string,
): WorkspaceFileOperations {
  const base = `${shareBase(code)}/workspace`;
  const writeHeaders = {
    "X-ClawManager-Share-CSRF": csrfToken,
  };

  return {
    async list(_instanceId: number, path = ""): Promise<WorkspaceEntry[]> {
      const response = await sharedAPI.get(`${base}/files`, { params: { path } });
      return response.data.data.entries;
    },

    async search(_instanceId: number, query: string, limit?: number): Promise<WorkspaceSearchResponse> {
      const response = await sharedAPI.get(`${base}/search`, { params: { q: query, ...(limit ? { limit } : {}) } });
      return response.data.data;
    },

    async preview(_instanceId: number, path: string): Promise<WorkspacePreview> {
      const response = await sharedAPI.get(`${base}/preview`, { params: { path } });
      return response.data.data.preview;
    },

    async previewBlob(_instanceId: number, path: string): Promise<Blob> {
      const response = await sharedAPI.get(`${base}/preview`, {
        params: { path, raw: 1 },
        responseType: "blob",
      });
      return response.data;
    },

    async downloadBlob(_instanceId: number, path: string): Promise<Blob> {
      const response = await sharedAPI.get(`${base}/download`, {
        params: { path },
        responseType: "blob",
      });
      return response.data;
    },

    async upload(_instanceId: number, path: string, file: File): Promise<void> {
      const formData = new FormData();
      formData.append("file", file);
      await sharedAPI.post(`${base}/upload`, formData, {
        params: { path },
        headers: {
          ...writeHeaders,
          "Content-Type": "multipart/form-data",
        },
      });
    },

    async mkdir(_instanceId: number, path: string): Promise<void> {
      await sharedAPI.post(`${base}/folders`, { path }, { headers: writeHeaders });
    },

    async rename(
      _instanceId: number,
      oldPath: string,
      newPath: string,
    ): Promise<void> {
      await sharedAPI.patch(
        `${base}/entries`,
        { old_path: oldPath, new_path: newPath },
        { headers: writeHeaders },
      );
    },

    async remove(_instanceId: number, path: string): Promise<void> {
      await sharedAPI.delete(`${base}/entries`, {
        params: { path },
        headers: writeHeaders,
      });
    },
  };
}
