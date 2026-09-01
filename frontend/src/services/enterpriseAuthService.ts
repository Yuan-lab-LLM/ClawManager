import api from './api';

export interface EnterpriseAuthStatus {
  enabled: boolean;
  provider: 'ldap' | string;
  configured: boolean;
  checks: Record<string, 'ok' | 'failed' | 'skipped' | 'anonymous' | string>;
  details?: Record<string, string>;
  warnings?: string[];
  error?: string;
}

export interface LDAPConfigPublic {
  host: string;
  port: number;
  use_tls: boolean;
  start_tls: boolean;
  skip_tls_verify: boolean;
  tls_ca_file: string;
  tls_server_name: string;
  bind_dn: string;
  base_dn: string;
  user_filter: string;
  username_attribute: string;
  email_attribute: string;
  group_base_dn: string;
  group_filter: string;
  admin_group_dns: string[];
  default_role: 'user' | 'admin' | string;
}

export interface EnterpriseAuthConfig {
  provider: 'ldap' | string;
  enabled: boolean;
  allow_local_fallback: boolean;
  sync_role: boolean;
  ldap: LDAPConfigPublic;
  bind_password_configured: boolean;
  version: number;
  updated_at?: string;
  status: EnterpriseAuthStatus;
}

export interface EnterpriseAuthConfigUpdate {
  expected_version: number;
  enabled: boolean;
  allow_local_fallback: boolean;
  sync_role: boolean;
  ldap: LDAPConfigPublic;
  bind_password?: string;
  clear_bind_password?: boolean;
}

export const enterpriseAuthService = {
  getStatus: async (): Promise<EnterpriseAuthStatus> => {
    const response = await api.get('/admin/auth/enterprise/status');
    return response.data.data;
  },

  getConfig: async (): Promise<EnterpriseAuthConfig> => {
    const response = await api.get('/admin/auth/enterprise/config');
    return response.data.data;
  },

  testConfig: async (data: EnterpriseAuthConfigUpdate): Promise<EnterpriseAuthStatus> => {
    const response = await api.post('/admin/auth/enterprise/config/test', data);
    return response.data.data;
  },

  updateConfig: async (data: EnterpriseAuthConfigUpdate): Promise<EnterpriseAuthConfig> => {
    const response = await api.put('/admin/auth/enterprise/config', data);
    return response.data.data;
  },
};
