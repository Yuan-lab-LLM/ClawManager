import api from './api';

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

export const adminService = {
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
};
