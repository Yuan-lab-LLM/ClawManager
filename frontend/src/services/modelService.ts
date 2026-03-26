import api from './api';

export interface LLMModel {
  id?: number;
  display_name: string;
  description?: string;
  provider_type: string;
  base_url: string;
  provider_model_name: string;
  api_key?: string;
  api_key_secret_ref?: string;
  is_secure: boolean;
  is_active: boolean;
  input_price: number;
  output_price: number;
  currency: string;
  created_at?: string;
  updated_at?: string;
}

export interface DiscoveredProviderModel {
  id: string;
  display_name: string;
}

export const modelService = {
  getModels: async (): Promise<LLMModel[]> => {
    const response = await api.get('/admin/models');
    return response.data.data?.items ?? [];
  },

  saveModel: async (model: LLMModel): Promise<LLMModel> => {
    const response = await api.put('/admin/models', model);
    return response.data.data;
  },

  discoverModels: async (request: {
    provider_type: string;
    base_url: string;
    api_key?: string;
    api_key_secret_ref?: string;
  }): Promise<DiscoveredProviderModel[]> => {
    const response = await api.post('/admin/models/discover', request);
    return response.data.data?.items ?? [];
  },

  deleteModel: async (id: number): Promise<void> => {
    await api.delete(`/admin/models/${id}`);
  },
};
