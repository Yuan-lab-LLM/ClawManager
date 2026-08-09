import api from './api';

export type EgressPrivateScopeType = 'instance' | 'user';

export interface EgressPrivateException {
  id: number;
  scope_type: EgressPrivateScopeType;
  scope_id: number;
  cidr: string;
  port: number;
  enabled: boolean;
  description?: string | null;
  created_by?: number | null;
  created_at: string;
  updated_at: string;
}

export interface UpsertEgressPrivateExceptionRequest {
  scope_type: EgressPrivateScopeType;
  scope_id: number;
  cidr: string;
  port: number;
  enabled?: boolean;
  description?: string;
}

export const egressPrivateExceptionService = {
  list: async (params?: {
    scope_type?: EgressPrivateScopeType;
    scope_id?: number;
  }): Promise<EgressPrivateException[]> => {
    const response = await api.get('/admin/egress-private-exceptions', { params });
    return response.data.data?.items ?? [];
  },

  create: async (payload: UpsertEgressPrivateExceptionRequest): Promise<EgressPrivateException> => {
    const response = await api.post('/admin/egress-private-exceptions', payload);
    return response.data.data;
  },

  update: async (
    id: number,
    payload: UpsertEgressPrivateExceptionRequest,
  ): Promise<EgressPrivateException> => {
    const response = await api.put(`/admin/egress-private-exceptions/${id}`, payload);
    return response.data.data;
  },

  remove: async (id: number): Promise<void> => {
    await api.delete(`/admin/egress-private-exceptions/${id}`);
  },
};
