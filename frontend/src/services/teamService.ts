import api from "./api";
import type { WorkspaceText } from "../components/WorkspaceTextEditor";
import type {
  CreateTeamRequest,
  DispatchTeamTaskRequest,
  TeamDetails,
  TeamEventsHistoryResponse,
  TeamListResponse,
  TeamWorkspaceListResponse,
  TeamWorkspacePreviewResponse,
  TeamTask,
  TeamTasksHistoryResponse,
} from "../types/team";

export const teamService = {
  listTeams: async (
    page: number = 1,
    limit: number = 20,
  ): Promise<TeamListResponse> => {
    const response = await api.get("/teams", {
      params: { page, limit },
    });
    return response.data.data;
  },

  createTeam: async (data: CreateTeamRequest): Promise<TeamDetails> => {
    const response = await api.post("/teams", data);
    return response.data.data;
  },

  getTeam: async (id: number): Promise<TeamDetails> => {
    const response = await api.get(`/teams/${id}`);
    return response.data.data;
  },

  getTeamTasks: async (
    id: number,
    beforeId?: number,
    limit: number = 20,
  ): Promise<TeamTasksHistoryResponse> => {
    const response = await api.get(`/teams/${id}/tasks`, {
      params: { before_id: beforeId, limit },
    });
    return response.data.data;
  },

  getTeamEvents: async (
    id: number,
    beforeId?: number,
    limit: number = 50,
  ): Promise<TeamEventsHistoryResponse> => {
    const response = await api.get(`/teams/${id}/events`, {
      params: { before_id: beforeId, limit },
    });
    return response.data.data;
  },

  dispatchTask: async (
    id: number,
    data: DispatchTeamTaskRequest,
  ): Promise<TeamTask> => {
    const response = await api.post(`/teams/${id}/tasks`, data);
    return response.data.data;
  },

  deleteTeam: async (id: number): Promise<void> => {
    await api.delete(`/teams/${id}`);
  },

  deleteMember: async (teamId: number, memberId: number | string): Promise<void> => {
    await api.delete(`/teams/${teamId}/members/${memberId}`);
  },

  listWorkspaceFiles: async (
    teamId: number,
    path: string = "",
    signal?: AbortSignal,
  ): Promise<TeamWorkspaceListResponse> => {
    const response = await api.get(`/teams/${teamId}/workspace/files`, {
      params: { path },
      signal,
    });
    return response.data.data;
  },

  previewWorkspaceFile: async (
    teamId: number,
    path: string,
    signal?: AbortSignal,
  ): Promise<TeamWorkspacePreviewResponse> => {
    const response = await api.get(`/teams/${teamId}/workspace/preview`, {
      params: { path },
      signal,
    });
    return response.data.data;
  },

  downloadWorkspaceFile: async (teamId: number, path: string): Promise<Blob> => {
    const response = await api.get(`/teams/${teamId}/workspace/download`, {
      params: { path },
      responseType: "blob",
    });
    return response.data;
  },

  readText: async (teamId: number, path: string, signal?: AbortSignal): Promise<WorkspaceText> => {
    const response = await api.get(`/teams/${teamId}/workspace/text`, { params: { path }, signal });
    return response.data.data;
  },
  saveText: async (teamId: number, path: string, text: string, version: string): Promise<WorkspaceText> => {
    const response = await api.put(`/teams/${teamId}/workspace/text`, { path, text, version });
    return response.data.data;
  },

  createWorkspaceFolder: async (
    teamId: number,
    data: { path: string; name: string },
  ): Promise<void> => {
    await api.post(`/teams/${teamId}/workspace/folders`, data);
  },

  renameWorkspaceEntry: async (
    teamId: number,
    data: { path: string; new_name: string },
  ): Promise<void> => {
    await api.post(`/teams/${teamId}/workspace/rename`, data);
  },

  deleteWorkspaceEntry: async (teamId: number, path: string): Promise<void> => {
    await api.delete(`/teams/${teamId}/workspace/files`, {
      params: { path },
    });
  },

  uploadWorkspaceFiles: async (
    teamId: number,
    path: string,
    files: File[],
    relativePaths?: string[],
  ): Promise<void> => {
    const formData = new FormData();
    formData.append("path", path);
    files.forEach((file, index) => {
      formData.append("files", file);
      formData.append("relative_paths", relativePaths?.[index] || file.name);
    });
    await api.post(`/teams/${teamId}/workspace/upload`, formData, {
      headers: { "Content-Type": "multipart/form-data" },
    });
  },
};
