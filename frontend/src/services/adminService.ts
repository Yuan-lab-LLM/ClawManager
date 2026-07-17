import api from './api';

export interface AdminSkillRecord {
  id: number;
  external_skill_id: string;
  user_id: number;
  skill_key: string;
  name: string;
  status: string;
  source_type: string;
  risk_level: string;
  scan_status: string;
  instance_count: number;
  risk_reason?: string;
  top_findings?: SkillFinding[];
  last_scanned_at?: string;
  updated_at: string;
}

export interface SkillFinding {
  analyzer: string;
  severity: string;
  category: string;
  rule_id: string;
  title: string;
  description: string;
  file_path?: string;
  line_number?: number;
  remediation: string;
  snippet?: string;
}

export interface SecurityScanConfig {
  active_mode: string;
  default_mode: string;
  quick_analyzers: string[];
  deep_analyzers: string[];
  quick_timeout_seconds: number;
  deep_timeout_seconds: number;
  allow_fallback: boolean;
  scanner_status: {
    connected: boolean;
    llm_enabled: boolean;
    status_label: string;
    available_capabilities: string[];
  };
  skill_scanner_config: {
    namespace: string;
    deployment_name: string;
    llm_api_key: string;
    llm_model: string;
    llm_base_url: string;
    meta_llm_api_key: string;
    meta_llm_model: string;
    meta_llm_base_url: string;
  };
}

export interface SecurityScanJobItem {
  id: number;
  asset_type: string;
  asset_id: number;
  asset_name: string;
  status: string;
  progress_pct: number;
  risk_level?: string;
  summary?: string;
  scan_result_id?: number;
  cached_result: boolean;
  triggered_analyzers?: string[];
  findings?: SkillFinding[];
  error_message?: string;
  started_at?: string;
  finished_at?: string;
}

export interface SecurityScanReport {
  job_id: number;
  asset_type: string;
  scan_mode: string;
  scan_scope: string;
  status: string;
  started_at?: string;
  finished_at?: string;
  total_items: number;
  completed_items: number;
  failed_items: number;
  risk_counts: Record<string, number>;
  findings_summary: Array<{ asset_name: string; summary: string }>;
  configured_analyzers: string[];
  available_analyzers: string[];
  triggered_analyzers: string[];
  items: SecurityScanJobItem[];
  config: SecurityScanConfig;
}

export interface SecurityScanJob {
  id: number;
  asset_type: string;
  scan_mode: string;
  scan_scope: string;
  status: string;
  requested_by?: number;
  total_items: number;
  completed_items: number;
  failed_items: number;
  current_item_name?: string;
  progress_pct: number;
  started_at?: string;
  finished_at?: string;
  created_at: string;
  updated_at: string;
  items?: SecurityScanJobItem[];
  report?: SecurityScanReport;
}

function normalizeSecurityScanConfig(config: any): SecurityScanConfig {
  return {
    active_mode: typeof config?.active_mode === 'string' ? config.active_mode : typeof config?.default_mode === 'string' ? config.default_mode : 'quick',
    default_mode: typeof config?.default_mode === 'string' ? config.default_mode : 'quick',
    quick_analyzers: Array.isArray(config?.quick_analyzers) ? config.quick_analyzers : [],
    deep_analyzers: Array.isArray(config?.deep_analyzers) ? config.deep_analyzers : [],
    quick_timeout_seconds: typeof config?.quick_timeout_seconds === 'number' ? config.quick_timeout_seconds : 30,
    deep_timeout_seconds: typeof config?.deep_timeout_seconds === 'number' ? config.deep_timeout_seconds : 120,
    allow_fallback: Boolean(config?.allow_fallback),
    scanner_status: {
      connected: Boolean(config?.scanner_status?.connected),
      llm_enabled: Boolean(config?.scanner_status?.llm_enabled),
      status_label: typeof config?.scanner_status?.status_label === 'string' ? config.scanner_status.status_label : '未启用',
      available_capabilities: Array.isArray(config?.scanner_status?.available_capabilities) ? config.scanner_status.available_capabilities : [],
    },
    skill_scanner_config: {
      namespace: typeof config?.skill_scanner_config?.namespace === 'string' ? config.skill_scanner_config.namespace : '',
      deployment_name: typeof config?.skill_scanner_config?.deployment_name === 'string' ? config.skill_scanner_config.deployment_name : '',
      llm_api_key: typeof config?.skill_scanner_config?.llm_api_key === 'string' ? config.skill_scanner_config.llm_api_key : '',
      llm_model: typeof config?.skill_scanner_config?.llm_model === 'string' ? config.skill_scanner_config.llm_model : '',
      llm_base_url: typeof config?.skill_scanner_config?.llm_base_url === 'string' ? config.skill_scanner_config.llm_base_url : '',
      meta_llm_api_key: typeof config?.skill_scanner_config?.meta_llm_api_key === 'string' ? config.skill_scanner_config.meta_llm_api_key : '',
      meta_llm_model: typeof config?.skill_scanner_config?.meta_llm_model === 'string' ? config.skill_scanner_config.meta_llm_model : '',
      meta_llm_base_url: typeof config?.skill_scanner_config?.meta_llm_base_url === 'string' ? config.skill_scanner_config.meta_llm_base_url : '',
    },
  };
}

function normalizeSecurityScanJobItem(item: any): SecurityScanJobItem {
  return {
    ...item,
    triggered_analyzers: Array.isArray(item?.triggered_analyzers) ? item.triggered_analyzers : [],
    findings: Array.isArray(item?.findings) ? item.findings : [],
  };
}

function normalizeSecurityScanReport(report: any): SecurityScanReport {
  return {
    ...report,
    scan_scope: typeof report?.scan_scope === 'string' ? report.scan_scope : 'incremental',
    risk_counts: report?.risk_counts ?? {},
    findings_summary: Array.isArray(report?.findings_summary) ? report.findings_summary : [],
    configured_analyzers: Array.isArray(report?.configured_analyzers) ? report.configured_analyzers : [],
    available_analyzers: Array.isArray(report?.available_analyzers) ? report.available_analyzers : [],
    triggered_analyzers: Array.isArray(report?.triggered_analyzers) ? report.triggered_analyzers : [],
    items: Array.isArray(report?.items) ? report.items.map(normalizeSecurityScanJobItem) : [],
    config: normalizeSecurityScanConfig(report?.config),
  };
}

function normalizeSecurityScanJob(job: any): SecurityScanJob {
  return {
    ...job,
    scan_scope: typeof job?.scan_scope === 'string' ? job.scan_scope : 'incremental',
    items: Array.isArray(job?.items) ? job.items.map(normalizeSecurityScanJobItem) : undefined,
    report: job?.report ? normalizeSecurityScanReport(job.report) : undefined,
  };
}

export interface ResourceSummary {
  capacity: number;
  allocatable: number;
  requested: number;
  unit: string;
}

export interface NodeResourceDetail {
  name: string;
  ready: boolean;
  roles: string[];
  kubelet_version: string;
  internal_ip: string;
  pod_count: number;
  cpu: ResourceSummary;
  memory: ResourceSummary;
  disk: ResourceSummary;
}

export interface ClusterResourceOverview {
  node_count: number;
  ready_nodes: number;
  cpu: ResourceSummary;
  memory: ResourceSummary;
  disk: ResourceSummary;
  nodes: NodeResourceDetail[];
}

export interface AIAuditItem {
  trace_id: string;
  request_id: string;
  session_id?: string;
  user_id?: number;
  username?: string;
  instance_id?: number;
  requested_model: string;
  actual_provider_model: string;
  provider_type: string;
  status: string;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  latency_ms?: number;
  error_message?: string;
  created_at: string;
  completed_at?: string;
}

export interface AIAuditListResponse {
  items: AIAuditItem[];
  total: number;
  page: number;
  limit: number;
}

export interface ModelInvocationRecord {
  id: number;
  trace_id: string;
  request_id: string;
  session_id?: string;
  requested_model: string;
  actual_provider_model: string;
  provider_type: string;
  status: string;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  request_payload?: string;
  response_payload?: string;
  error_message?: string;
  latency_ms?: number;
  created_at: string;
  completed_at?: string;
}

export interface AuditEventRecord {
  id: number;
  trace_id: string;
  event_type: string;
  traffic_class: string;
  severity: string;
  message: string;
  details?: string;
  created_at: string;
}

export interface CostRecordView {
  id: number;
  trace_id: string;
  user_id?: number;
  username?: string;
  instance_id?: number;
  instance_name?: string;
  model_name: string;
  provider_type: string;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  estimated_cost: number;
  internal_cost: number;
  currency: string;
  recorded_at: string;
}

export interface RiskHitRecord {
  id: number;
  trace_id: string;
  rule_id: string;
  rule_name: string;
  severity: string;
  action: string;
  match_summary: string;
  created_at: string;
}

export interface AuditTraceDetail {
  trace_id: string;
  username?: string;
  invocations: ModelInvocationRecord[];
  audit_events: AuditEventRecord[];
  cost_records: CostRecordView[];
  risk_hits: RiskHitRecord[];
  flow_nodes: Array<{
    id: string;
    kind: string;
    title: string;
    request_id?: string;
    invocation_id?: number;
    model?: string;
    status?: string;
    summary?: string;
    input_payload?: string;
    output_payload?: string;
    created_at: string;
  }>;
  messages: Array<{
    id: number;
    trace_id: string;
    session_id: string;
    role: string;
    content: string;
    sequence_no: number;
    created_at: string;
  }>;
}

export interface CostBreakdownItem {
  label: string;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  estimated_cost: number;
  internal_cost: number;
}

export interface CostOverview {
  total_prompt_tokens: number;
  total_completion_tokens: number;
  total_tokens: number;
  total_estimated_cost: number;
  total_internal_cost: number;
  currency: string;
  user_summary: Array<{
    label: string;
    meta?: string;
    prompt_tokens: number;
    completion_tokens: number;
    total_tokens: number;
    estimated_cost: number;
    internal_cost: number;
  }>;
  instance_summary: Array<{
    label: string;
    meta?: string;
    prompt_tokens: number;
    completion_tokens: number;
    total_tokens: number;
    estimated_cost: number;
    internal_cost: number;
  }>;
  top_models: CostBreakdownItem[];
  top_users: CostBreakdownItem[];
  daily_trend: Array<{
    day: string;
    prompt_tokens: number;
    completion_tokens: number;
    total_tokens: number;
    estimated_cost: number;
    internal_cost: number;
  }>;
  model_trends: Array<{
    label: string;
    points: Array<{
      day: string;
      prompt_tokens: number;
      completion_tokens: number;
      total_tokens: number;
      estimated_cost: number;
      internal_cost: number;
    }>;
  }>;
  user_trends: Array<{
    label: string;
    points: Array<{
      day: string;
      prompt_tokens: number;
      completion_tokens: number;
      total_tokens: number;
      estimated_cost: number;
      internal_cost: number;
    }>;
  }>;
  recent_records: CostRecordView[];
  total_recent_records: number;
  page: number;
  limit: number;
}

export interface SessionUsageOverviewSummary {
  total_prompt_tokens: number;
  total_completion_tokens: number;
  total_tokens: number;
  total_estimated_cost: number;
  currency: string;
  session_count: number;
}

export interface SessionUsageOverviewCompliance {
  fallback_session_count: number;
  has_fallback_sessions: boolean;
  recent_fallback_audit_count: number;
}

export interface SessionUsageOverviewItem {
  instance_id: number;
  instance_name: string;
  instance_type: string;
  user_id: number;
  summary: SessionUsageOverviewSummary;
  compliance: SessionUsageOverviewCompliance;
}

export interface SessionUsageOverview {
  summary: SessionUsageOverviewSummary;
  items: SessionUsageOverviewItem[];
  total: number;
  page: number;
  limit: number;
}

export const adminService = {
  listSkills: async (): Promise<AdminSkillRecord[]> => {
    const response = await api.get('/admin/skills');
    return response.data.data ?? [];
  },

  getSecurityConfig: async (): Promise<SecurityScanConfig> => {
    const response = await api.get('/admin/security/config');
    return normalizeSecurityScanConfig(response.data.data);
  },

  saveSecurityConfig: async (payload: SecurityScanConfig): Promise<SecurityScanConfig> => {
    const response = await api.put('/admin/security/config', payload);
    return normalizeSecurityScanConfig(response.data.data);
  },

  startSecurityScan: async (payload: { asset_type: string; scan_scope?: string; scan_mode?: string; asset_id?: number }): Promise<SecurityScanJob> => {
    const response = await api.post('/admin/security/scan-jobs', payload);
    return normalizeSecurityScanJob(response.data.data);
  },

  listSecurityScanJobs: async (limit: number = 20): Promise<SecurityScanJob[]> => {
    const response = await api.get('/admin/security/scan-jobs', { params: { limit } });
    return Array.isArray(response.data.data) ? response.data.data.map(normalizeSecurityScanJob) : [];
  },

  getSecurityScanJob: async (jobId: number): Promise<SecurityScanJob> => {
    const response = await api.get(`/admin/security/scan-jobs/${jobId}`);
    return normalizeSecurityScanJob(response.data.data);
  },

  getClusterResources: async (): Promise<ClusterResourceOverview> => {
    const response = await api.get('/system-settings/cluster-resources');
    return response.data.data;
  },

  getAIAudit: async (params?: {
    page?: number;
    limit?: number;
    search?: string;
    status?: string;
    model?: string;
  }): Promise<AIAuditListResponse> => {
    const response = await api.get('/admin/ai-audit', { params });
    return response.data.data ?? { items: [], total: 0, page: 1, limit: params?.limit ?? 20 };
  },

  getAITraceDetail: async (traceId: string): Promise<AuditTraceDetail> => {
    const response = await api.get(`/admin/ai-audit/${traceId}`);
    const data = response.data.data ?? {};
    return {
      trace_id: data.trace_id ?? traceId,
      username: data.username,
      invocations: data.invocations ?? [],
      audit_events: data.audit_events ?? [],
      cost_records: data.cost_records ?? [],
      risk_hits: data.risk_hits ?? [],
      flow_nodes: data.flow_nodes ?? [],
      messages: data.messages ?? [],
    };
  },

  getCostOverview: async (params?: {
    page?: number;
    limit?: number;
    search?: string;
  }): Promise<CostOverview> => {
    const response = await api.get('/admin/costs', { params });
    const data = response.data.data ?? {};
    return {
      total_prompt_tokens: data.total_prompt_tokens ?? 0,
      total_completion_tokens: data.total_completion_tokens ?? 0,
      total_tokens: data.total_tokens ?? 0,
      total_estimated_cost: data.total_estimated_cost ?? 0,
      total_internal_cost: data.total_internal_cost ?? 0,
      currency: data.currency ?? 'USD',
      user_summary: data.user_summary ?? [],
      instance_summary: data.instance_summary ?? [],
      top_models: data.top_models ?? [],
      top_users: data.top_users ?? [],
      daily_trend: data.daily_trend ?? [],
      model_trends: data.model_trends ?? [],
      user_trends: data.user_trends ?? [],
      recent_records: data.recent_records ?? [],
      total_recent_records: data.total_recent_records ?? 0,
      page: data.page ?? params?.page ?? 1,
      limit: data.limit ?? params?.limit ?? 20,
    };
  },

  getSessionUsageOverview: async (params?: {
    page?: number;
    limit?: number;
    search?: string;
    since?: string;
    until?: string;
  }): Promise<SessionUsageOverview> => {
    const response = await api.get("/admin/session-usage/overview", { params });
    const data = response.data.data ?? {};
    return {
      summary: data.summary ?? {
        total_prompt_tokens: 0,
        total_completion_tokens: 0,
        total_tokens: 0,
        total_estimated_cost: 0,
        currency: "USD",
        session_count: 0,
      },
      items: data.items ?? [],
      total: data.total ?? 0,
      page: data.page ?? params?.page ?? 1,
      limit: data.limit ?? params?.limit ?? 20,
    };
  },
};
