import api from './api';

export interface SystemImageSetting {
  id?: number;
  instance_type: string;
  display_name: string;
  image: string;
  is_enabled?: boolean;
  created_at?: string;
  updated_at?: string;
}

export const systemSettingsService = {
  getImageSettings: async (): Promise<SystemImageSetting[]> => {
    const response = await api.get('/system-settings/images');
    return (response.data.data?.items ?? []).map((item: SystemImageSetting) => ({
      ...item,
      id: item.id && item.id > 0 ? item.id : undefined,
    }));
  },

  saveImageSetting: async (setting: SystemImageSetting): Promise<SystemImageSetting> => {
    const response = await api.put('/system-settings/images', setting);
    return response.data.data;
  },

  deleteImageSetting: async (target: number | string): Promise<void> => {
    await api.delete(`/system-settings/images/${target}`);
  },
};
