import api from './api';

export interface LLMModel {
	visibility_scope?: 'all' | 'selected_users';
	allowed_user_ids?: number[];
	access_policy_version?: number;
  id?: number;
  display_name: string;
  description?: string;
  provider_type: string;
  protocol_type?: string;
  model_kind?: 'chat' | 'image' | 'other';
  image_generation_enabled?: boolean;
  image_edit_enabled?: boolean;
  image_price_per_image?: number | null;
  reset_image_price?: boolean;
  model_group?: string;
  deployment_priority?: number;
  max_concurrent_requests?: number;
  context_window_tokens?: number;
  quality_tier?: number;
  base_url: string;
  provider_model_name: string;
  provider_models?: DiscoveredProviderModel[];
  reasoning_enabled: boolean;
  reasoning_control_mode?: string;
  supports_reasoning?: boolean;
  supports_tools?: boolean;
  tools_support_override?: boolean | null;
  supports_vision?: boolean;
  vision_support_override?: boolean | null;
  reset_vision_support_override?: boolean;
  capability_source?: string;
  reasoning_control?: string;
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
	visibility_scope?: 'all' | 'selected_users';
	allowed_user_ids?: number[];
  /** Local preview edit; never sent as provider metadata. */
  plan_edited?: boolean;
  /** Administrator choice for this import row, not provider metadata. */
  tools_support_override?: boolean | null;
  /** Local reasoning choice; distinct from provider supports_reasoning metadata. */
  reasoning_enabled?: boolean;
  reasoning_control_mode?: string;
  id: string;
  display_name: string;
  suggested_pool?: string;
  suggested_quality_tier?: number;
  suggested_priority?: number;
  confidence?: number;
  classification_source?: string;
  reasons?: string[];
  chat_capable?: boolean;
  context_window_tokens?: number;
  supports_reasoning?: boolean;
  supports_tools?: boolean;
  supports_vision?: boolean;
  vision_support_override?: boolean | null;
  reset_vision_support_override?: boolean;
  capability_source?: string;
  model_kind?: string;
  image_generation_enabled?: boolean;
  image_edit_enabled?: boolean;
  image_price_per_image?: number | null;
  reset_image_price?: boolean;
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

  saveModelsBatch: async (models: LLMModel[]): Promise<{ saved: LLMModel[]; failed: Array<{ display_name: string; provider_model_name: string; error: string }>; saved_count: number; failed_count: number }> => {
    const response = await api.post('/admin/models/batch', { models });
    return response.data.data;
  },

  discoverModels: async (request: {
    provider_type: string;
    protocol_type?: string;
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
