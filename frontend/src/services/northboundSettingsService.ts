import api from './api';

export interface NorthboundSettings {
	owner_teams_enabled: boolean;
	owner_team_creation_user_id: number;
  id: number;
  api_enabled: boolean;
  external_node_port: number;
  require_explicit_callers: boolean;
  challenge_ttl_seconds: number;
  access_token_ttl_seconds: number;
  refresh_token_ttl_seconds: number;
  core_request_timeout_seconds: number;
  challenge_rate_per_minute: number;
  login_rate_per_minute: number;
  account_login_rate_per_minute: number;
  create_rate_per_minute: number;
  query_rate_per_minute: number;
  share_rate_per_minute: number;
  max_pending_operations: number;
  operation_tick_milliseconds: number;
  operation_lease_seconds: number;
  operation_max_attempts: number;
  allowed_lite_types: string[];
  allowed_pro_types: string[];
  lite_cpu_cores: number;
  lite_memory_gb: number;
  lite_disk_gb: number;
  pro_cpu_cores: number;
  pro_memory_gb: number;
  pro_disk_gb: number;
  workbuddy_pro_cpu_cores: number;
  workbuddy_pro_memory_gb: number;
  workbuddy_pro_disk_gb: number;
  version: number;
  updated_at: string;
}

export interface NorthboundCallerPolicy {
  user_id: number;
  username?: string;
  email?: string;
  enabled: boolean;
  scopes: string[];
  updated_at?: string;
}

export interface NorthboundCertificateStatus {
  available: boolean;
  managed: boolean;
  subject?: string;
  issuer?: string;
  dns_names: string[];
  ip_addresses: string[];
  not_after?: string;
  days_remaining?: number;
  sha256?: string;
  error?: string;
  management_note: string;
  prepared: boolean;
  prepared_not_after?: string;
}

export interface NorthboundOverview {
  settings: NorthboundSettings;
  callers: NorthboundCallerPolicy[];
  cluster: {
    available: boolean;
    namespace?: string;
    service_name?: string;
    external_node_port?: number;
    gateway_port: number;
    core_port: number;
    error?: string;
    certificate: NorthboundCertificateStatus;
  };
  stats: { active_sessions: number; queued_operations: number; processing_operations: number; failed_operations: number };
  audit: Array<{ id: number; actor?: string; action: string; created_at: string }>;
}

export const northboundSettingsService = {
  saveSection: async (section: string, version: number, fields: Partial<NorthboundSettings>): Promise<NorthboundSettings> => {
    const response = await api.patch(`/admin/northbound/settings/${section}`, { version, fields });
    return response.data.data;
  },
  getAPIKeys: async (userID: number): Promise<NorthboundAPIKeyList> => {
    const response = await api.get('/admin/northbound/api-keys', { params: { user_id: userID } });
    return response.data.data;
  },
  mutateAPIKey: async (request: NorthboundAPIKeyMutation): Promise<NorthboundAPIKeyList & {secret?: string}> => {
    const response = await api.post('/admin/northbound/api-keys', request);
    return response.data.data;
  },
  getOverview: async (): Promise<NorthboundOverview> => {
    const response = await api.get('/admin/northbound');
    return response.data.data;
  },
  saveSettings: async (settings: NorthboundSettings): Promise<NorthboundSettings> => {
    const response = await api.put('/admin/northbound/settings', settings);
    return response.data.data;
  },
  saveCaller: async (policy: NorthboundCallerPolicy): Promise<NorthboundCallerPolicy> => {
    const response = await api.put('/admin/northbound/callers', policy);
    return response.data.data;
  },
  setExternalNodePort: async (nodePort: number): Promise<NorthboundOverview> => {
    const response = await api.put('/admin/northbound/external-node-port', { node_port: nodePort });
    return response.data.data;
  },
  downloadPublicCA: async (): Promise<void> => {
    const response = await api.get('/admin/northbound/certificate/ca', { responseType: 'blob' });
    const url = URL.createObjectURL(response.data);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = 'clawmanager-northbound-ca.crt';
    anchor.click();
    URL.revokeObjectURL(url);
  },
  downloadPreparedCA: async (): Promise<void> => {
    const response = await api.get('/admin/northbound/certificate/prepared-ca', { responseType: 'blob' });
    const url = URL.createObjectURL(response.data);
    const anchor = document.createElement('a'); anchor.href = url; anchor.download = 'clawmanager-northbound-managed-ca.crt'; anchor.click(); URL.revokeObjectURL(url);
  },
  prepareCertificate: async (request: { dns_names: string[]; ip_addresses: string[]; valid_days: number }): Promise<NorthboundOverview> => {
    const response = await api.post('/admin/northbound/certificate/prepare', request); return response.data.data;
  },
  activateCertificate: async (): Promise<NorthboundOverview> => {
    const response = await api.post('/admin/northbound/certificate/activate', { confirmation: 'ACTIVATE' }); return response.data.data;
  },
};

export interface NorthboundAPIKey {
 id: string; name: string; scopes: string[]; effective_scopes: string[]; enabled: boolean;
 expires_at: string; created_at: string; created_by: number; revoked_at?: string;
}
export interface NorthboundAPIKeyList {user_id: number; version: number; keys: NorthboundAPIKey[]}
export interface NorthboundAPIKeyMutation {
 user_id: number; version: number; action: 'create'|'update'|'disable'|'revoke'|'rotate'|'remove';
 id?: string; name?: string; scopes?: string[]; enabled?: boolean; expires_at?: string;
}
