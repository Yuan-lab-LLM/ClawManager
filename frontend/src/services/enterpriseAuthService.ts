import api from './api';

export interface EnterpriseAuthStatus {
  enabled: boolean;
  provider: 'ldap' | string;
  configured: boolean;
  checks: Record<string, 'ok' | 'failed' | 'skipped' | 'anonymous' | string>;
  warnings?: string[];
  error?: string;
}

export const enterpriseAuthService = {
  getStatus: async (): Promise<EnterpriseAuthStatus> => {
    const response = await api.get('/admin/auth/enterprise/status');
    return response.data.data;
  },
};
