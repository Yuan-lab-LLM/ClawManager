import api from './api';
import type {
  InstanceSkill,
  Skill,
  SkillHubCatalogResponse,
  SkillHubTag,
  SkillImportDecision,
  SkillImportPreviewItem,
  SkillImportResultItem,
} from '../types/skill';

export const skillHubService = {
  listCatalog: async (params?: { tag_keys?: string[]; q?: string; page?: number; page_size?: number }): Promise<SkillHubCatalogResponse> => {
    const response = await api.get('/skill-hub/catalog', { params });
    return response.data.data;
  },

  listTags: async (): Promise<SkillHubTag[]> => {
    const response = await api.get('/skill-hub/tags');
    return response.data.data;
  },

  listMine: async (): Promise<Skill[]> => {
    const response = await api.get('/skill-hub/mine');
    return response.data.data;
  },

  listAttachable: async (): Promise<Skill[]> => {
    const response = await api.get('/skill-hub/attachable');
    return response.data.data;
  },

  listAdminSkills: async (): Promise<Skill[]> => {
    const response = await api.get('/admin/skill-hub/skills');
    return response.data.data;
  },

  previewImportSkills: async (file: File): Promise<SkillImportPreviewItem[]> => {
    const formData = new FormData();
    formData.append('file', file);
    const response = await api.post('/skill-hub/skills/import/preview', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    });
    return response.data.data;
  },

  importSkills: async (file: File, decisions?: SkillImportDecision[]): Promise<SkillImportResultItem[]> => {
    const formData = new FormData();
    formData.append('file', file);
    if (decisions && decisions.length > 0) {
      formData.append('decisions', JSON.stringify(decisions));
    }
    const response = await api.post('/skill-hub/skills/import', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    });
    return response.data.data;
  },

  publishSkill: async (skillId: number, tagIds: number[]): Promise<Skill> => {
    const response = await api.post(`/skill-hub/skills/${skillId}/publish`, { tag_ids: tagIds });
    return response.data.data;
  },

  unpublishSkill: async (skillId: number): Promise<Skill> => {
    const response = await api.post(`/skill-hub/skills/${skillId}/unpublish`);
    return response.data.data;
  },

  updateTags: async (skillId: number, tagIds: number[]): Promise<Skill> => {
    const response = await api.put(`/skill-hub/skills/${skillId}/tags`, { tag_ids: tagIds });
    return response.data.data;
  },

  deleteSkill: async (skillId: number): Promise<void> => {
    await api.delete(`/skill-hub/skills/${skillId}`);
  },

  downloadSkill: async (skillId: number): Promise<Blob> => {
    const response = await api.get(`/skill-hub/skills/${skillId}/download`, { responseType: 'blob' });
    return response.data;
  },

  installSkill: async (skillId: number, instanceId: number): Promise<InstanceSkill> => {
    const response = await api.post(`/skill-hub/skills/${skillId}/install`, { instance_id: instanceId });
    return response.data.data;
  },

  installSkillBatch: async (skillId: number, instanceIds: number[]): Promise<Array<{ instance_id: number; instance_skill?: InstanceSkill; error?: string }>> => {
    const response = await api.post(`/skill-hub/skills/${skillId}/install-batch`, { instance_ids: instanceIds });
    return response.data.data;
  },
  publishFromInstance: async (instanceId: number, skillId: number, tagIds: number[]): Promise<Skill> => {
    const response = await api.post(`/instances/${instanceId}/skills/${skillId}/publish-to-hub`, { tag_ids: tagIds });
    return response.data.data;
  },

  importInstanceSkill: async (instanceId: number, skillId: number): Promise<Skill> => {
    const response = await api.post(`/instances/${instanceId}/skills/${skillId}/import-to-library`);
    return response.data.data;
  },

  retryPackageCollect: async (instanceId: number, skillId: number): Promise<void> => {
    await api.post(`/instances/${instanceId}/skills/${skillId}/retry-package-collect`);
  },
};
