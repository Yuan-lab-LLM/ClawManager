import api from './api';
import type { InstanceSkill, Skill, SkillScanResult, SkillVersion } from '../types/skill';

export const skillService = {
  listSkills: async (): Promise<Skill[]> => {
    const response = await api.get('/skills');
    return response.data.data;
  },

  importSkills: async (file: File): Promise<Skill[]> => {
    const formData = new FormData();
    formData.append('file', file);
    const response = await api.post('/skills/import', formData, {
      headers: {
        'Content-Type': 'multipart/form-data',
      },
    });
    return response.data.data;
  },

  updateSkill: async (id: number, payload: Pick<Skill, 'name' | 'description' | 'status'>): Promise<Skill> => {
    const response = await api.put(`/skills/${id}`, payload);
    return response.data.data;
  },

  deleteSkill: async (id: number): Promise<void> => {
    await api.delete(`/skills/${id}`);
  },

  downloadSkill: async (id: number): Promise<Blob> => {
    const response = await api.get(`/skills/${id}/download`, { responseType: 'blob' });
    return response.data;
  },

  listVersions: async (id: number): Promise<SkillVersion[]> => {
    const response = await api.get(`/skills/${id}/versions`);
    return response.data.data;
  },

  listScanResults: async (id: number): Promise<SkillScanResult[]> => {
    const response = await api.get(`/skills/${id}/scan-results`);
    return response.data.data;
  },

  listInstanceSkills: async (instanceId: number): Promise<InstanceSkill[]> => {
    const response = await api.get(`/instances/${instanceId}/skills`);
    return response.data.data;
  },

  listAvailableInstanceSkills: async (instanceId: number): Promise<Skill[]> => {
    const response = await api.get(`/instances/${instanceId}/skills/available`);
    return response.data.data;
  },
  attachSkillToInstance: async (instanceId: number, skillId: number): Promise<InstanceSkill> => {
    const response = await api.post(`/instances/${instanceId}/skills`, { skill_id: skillId });
    return response.data.data;
  },

  removeSkillFromInstance: async (instanceId: number, skillId: number): Promise<void> => {
    await api.delete(`/instances/${instanceId}/skills/${skillId}`);
  },
};
