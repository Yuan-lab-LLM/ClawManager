import api from "./api";
import type { WorkspaceEntry, WorkspacePreview, WorkspaceSearchResponse } from "../types/workspace";

export const workspaceService = {
  async list(instanceId: number, path = ""): Promise<WorkspaceEntry[]> {
    const response = await api.get(`/instances/${instanceId}/workspace/files`, {
      params: { path },
    });
    return response.data.data.entries;
  },

  async search(instanceId: number, query: string, limit?: number): Promise<WorkspaceSearchResponse> {
    const response = await api.get(`/instances/${instanceId}/workspace/search`, {
      params: { q: query, ...(limit ? { limit } : {}) },
    });
    return response.data.data;
  },

  async preview(instanceId: number, path: string): Promise<WorkspacePreview> {
    const response = await api.get(`/instances/${instanceId}/workspace/preview`, {
      params: { path },
    });
    return response.data.data.preview;
  },

  async previewBlob(instanceId: number, path: string): Promise<Blob> {
    const response = await api.get(`/instances/${instanceId}/workspace/preview`, {
      params: { path, raw: 1 },
      responseType: "blob",
    });
    return response.data;
  },

  async downloadBlob(instanceId: number, path: string): Promise<Blob> {
    const response = await api.get(`/instances/${instanceId}/workspace/download`, {
      params: { path },
      responseType: "blob",
    });
    return response.data;
  },

  async upload(instanceId: number, path: string, file: File): Promise<void> {
    const formData = new FormData();
    formData.append("file", file);
    await api.post(`/instances/${instanceId}/workspace/upload`, formData, {
      params: { path },
      headers: { "Content-Type": "multipart/form-data" },
    });
  },

  async mkdir(instanceId: number, path: string): Promise<void> {
    await api.post(`/instances/${instanceId}/workspace/folders`, { path });
  },

  async rename(instanceId: number, oldPath: string, newPath: string): Promise<void> {
    await api.patch(`/instances/${instanceId}/workspace/entries`, {
      old_path: oldPath,
      new_path: newPath,
    });
  },

  async remove(instanceId: number, path: string): Promise<void> {
    await api.delete(`/instances/${instanceId}/workspace/entries`, {
      params: { path },
    });
  },
};
