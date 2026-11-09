import type { BrowserOriginMetadata } from "../lib/browserOrigin";
import axios, { type InternalAxiosRequestConfig } from "axios";
import { createWorkbenchService } from "./instanceWorkbenchService";
import type { WorkspaceFileOperations } from "../components/WorkspaceFileManager";
import type { WorkspaceEntry, WorkspacePreview, WorkspaceSearchResponse } from "../types/workspace";

export type SharedWorkspaceAccess = "none" | "read" | "write";

export interface SharedInstanceSession extends BrowserOriginMetadata {
  instance: {
    id: number;
    name: string;
    type: string;
    status: string;
    instance_mode?: string;
    runtime_type?: string;
    runtime_generation?: number;
  };
  access_url: string;
  session_expires_at: string;
  share_expires_at?: string;
  workspace_access: SharedWorkspaceAccess;
  workspace_available: boolean;
  workspace_root: string;
  csrf_token: string;
  workbench_scope?: string;
}

const API_BASE_URL = import.meta.env.VITE_API_URL || "/api/v1";

const sharedAPI = axios.create({
  baseURL: `${API_BASE_URL.replace(/\/+$/, "")}/shared-instances`,
  withCredentials: true,
});

function shareBase(code: string) {
  return `/${encodeURIComponent(code)}`;
}

// Cookies are shared by browser tabs. Refresh only an explicitly rejected CSRF
// check, which the server performs before executing any workspace operation.
const currentCSRF = new Map<string, string>();
const renewals = new Map<string, Promise<SharedInstanceSession>>();
type ShareRequest = InternalAxiosRequestConfig & { shareCSRFRetried?: boolean };
sharedAPI.interceptors.request.use((config) => {
  const scope = config.url?.split("/")[1];
  const token = scope && currentCSRF.get(scope);
  if (token && config.headers.has("X-ClawManager-Share-CSRF")) {
    config.headers.set("X-ClawManager-Share-CSRF", token);
  }
  return config;
});
sharedAPI.interceptors.response.use(undefined, async (error: unknown) => {
  if (!axios.isAxiosError(error) || error.response?.status !== 403 ||
      error.response.data?.error_code !== "share_csrf_invalid") throw error;
  const config = error.config as ShareRequest | undefined;
  const scope = config?.url?.split("/")[1];
  if (!config || !scope || config.shareCSRFRetried || config.signal?.aborted) throw error;
  config.shareCSRFRetried = true;
  let renewal = renewals.get(scope);
  if (!renewal) {
    renewal = getSharedInstanceSession(decodeURIComponent(scope));
    renewals.set(scope, renewal);
    void renewal.finally(() => { if (renewals.get(scope) === renewal) renewals.delete(scope); }).catch(() => {});
  }
  await renewal;
  return sharedAPI.request(config);
});

export async function getSharedInstanceSession(code: string): Promise<SharedInstanceSession> {
  const response = await sharedAPI.get(`${shareBase(code)}/session`);
  const session: SharedInstanceSession = response.data.data;
  currentCSRF.set(encodeURIComponent(code), session.csrf_token);
  return session;
}

export function createSharedWorkspaceService(
  code: string,
  csrfToken: string,
): WorkspaceFileOperations {
  currentCSRF.set(encodeURIComponent(code), csrfToken);
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

export function createSharedWorkbenchService(code: string, csrfToken: string) {
  currentCSRF.set(encodeURIComponent(code), csrfToken);
  return createWorkbenchService(sharedAPI, () => `${shareBase(code)}/workbench`, {
    "X-ClawManager-Share-CSRF": csrfToken,
  });
}
