import api from './api';

// ---- Types ---------------------------------------------------------------

export type Severity = 'low' | 'medium' | 'high';
export type RuleAction = 'observe' | 'redact' | 'block';
export type RuleMode = 'enforce' | 'observe' | 'off';
export type RuleTarget =
  | 'user_input'
  | 'rag_result'
  | 'web_content'
  | 'email'
  | 'document'
  | 'tool_output';

export type AlertSource =
  | 'platform'
  | 'gateway'
  | 'aegis'
  | 'secureclaw'
  | 'ksecure'
  | 'kubearmor'
  | 'collab_governance';

export interface SecplaneRule {
  id?: number;
  rule_id: string;
  kind: string;
  display_name: string;
  description?: string;
  pattern: string;
  target: RuleTarget;
  severity: Severity;
  action: RuleAction;
  mode: RuleMode;
  is_enabled: boolean;
  sort_order: number;
  tags?: string;
  created_at?: string;
  updated_at?: string;
}

export interface SecplaneMatch {
  rule_id: string;
  rule_name: string;
  severity: Severity;
  action: RuleAction;
  mode: RuleMode;
  matched_text: string;
  match_summary: string;
}

export interface SecplaneAnalysis {
  is_sensitive: boolean;
  highest_severity: Severity;
  highest_action: RuleAction;
  hits: SecplaneMatch[];
}

export interface SecplaneAlert {
  id: number;
  trace_id?: string;
  source: AlertSource;
  rule_id?: string;
  rule_name?: string;
  severity: Severity;
  action: string;
  agent_id?: string;
  subject?: string;
  evidence?: string;
  raw_payload?: string;
  ts: string;
}

export interface TestRequest {
  text: string;
  target?: RuleTarget;
  record_alert?: boolean;
  subject?: string;
  trace_id?: string;
  agent_id?: string;
  source?: AlertSource;
  rule?: SecplaneRule;
}

// ---- Service -------------------------------------------------------------

export interface DispatchTarget {
  instance_id: number;
  command_id?: number;
  command_type: string;
  status: string;
  error?: string;
}

export interface DispatchResult {
  revision: string;
  sha256: string;
  user_config: Record<string, unknown>;
  skill_id?: number;
  skill_key?: string;
  version_no?: number;
  targets: DispatchTarget[];
}

// Collaboration governance policy — singleton row driving the ClawAegis
// collab_guard defense. Mirrors backend collabPolicyResponse. The 4 sub-mode
// fields (identityMode/schemaMode/quotaMode/approvalMode) each control one
// rule inside detectCollabGuardViolation; the master collabGuardMode is
// derived from identityMode on the backend (enforce if any sub-rule enforces).
export type CollabCommunicationMode = 'leader_mediated' | 'relay_only' | 'peer_limited';
export type CollabRedisAclMode = 'password_only' | 'per_team' | 'per_member';

export interface CollaborationPolicy {
  teamId: string;
  communicationMode: CollabCommunicationMode;
  redisAclMode: CollabRedisAclMode;
  relayRequired: boolean;
  identityMode: RuleMode;
  schemaMode: RuleMode;
  quotaMode: RuleMode;
  approvalMode: RuleMode;
  muteOnAnomaly: boolean;
  auditReplay: boolean;
  xaddRps: number;
  xaddWindowSeconds: number;
  streamMaxLen: number;
  approvalThreshold: number;
  redisAclPreview: string;
  updatedAt?: string;
}

export const secplaneService = {
  listRules: async (kind = 'prompt_filter'): Promise<SecplaneRule[]> => {
    const response = await api.get('/secplane/policy/rules', {
      params: kind ? { kind } : undefined,
    });
    return response.data.data?.items ?? [];
  },

  saveRule: async (rule: SecplaneRule): Promise<SecplaneRule> => {
    const response = await api.put('/secplane/policy/rules', rule);
    return response.data.data;
  },

  disableRule: async (ruleId: string): Promise<void> => {
    await api.delete(`/secplane/policy/rules/${ruleId}`);
  },

  bulkSetEnabled: async (
    ruleIds: string[],
    isEnabled: boolean,
  ): Promise<void> => {
    await api.post('/secplane/policy/rules/bulk-status', {
      rule_ids: ruleIds,
      is_enabled: isEnabled,
    });
  },

  testRules: async (req: TestRequest): Promise<SecplaneAnalysis> => {
    const response = await api.post('/secplane/policy/rules/test', req);
    return response.data.data;
  },

  listAlerts: async (params?: {
    source?: AlertSource;
    severity?: Severity;
    rule_id?: string;
    limit?: number;
  }): Promise<SecplaneAlert[]> => {
    const response = await api.get('/secplane/alerts', { params });
    return response.data.data?.items ?? [];
  },

  // Compile current rules → ClawAegisEx user_config → upload as new skill
  // version → enqueue install_skill on each target. Empty instance_ids =
  // dispatch to ALL OpenClaw instances.
  dispatchAegis: async (instanceIds?: number[]): Promise<DispatchResult> => {
    const body = instanceIds && instanceIds.length > 0 ? { instance_ids: instanceIds } : {};
    const response = await api.post('/secplane/dispatch/aegis', body);
    return response.data.data;
  },

  // "Apply policy" endpoint. Currently aliased on the backend to the same
  // install_skill pipeline that dispatchAegis uses (zip + blob + skill
  // version), because standard OpenClaw pod agents reject the more direct
  // secplane.apply_aegis_config / update_skill channels. Plugin still
  // hot-reloads from the rewritten user_config.json via mtime watch (~1s).
  dispatchAegisApply: async (instanceIds?: number[]): Promise<DispatchResult> => {
    const body = instanceIds && instanceIds.length > 0 ? { instance_ids: instanceIds } : {};
    const response = await api.post('/secplane/dispatch/aegis-apply', body);
    return response.data.data;
  },

  // Same shape as dispatchAegis but pipes secureclaw_config rules into a
  // SecureClawConfig user_config.json packaged in the secureclaw skill.
  dispatchSecureClaw: async (instanceIds?: number[]): Promise<DispatchResult> => {
    const body = instanceIds && instanceIds.length > 0 ? { instance_ids: instanceIds } : {};
    const response = await api.post('/secplane/dispatch/secureclaw', body);
    return response.data.data;
  },

  // Collaboration governance policy — singleton row in secplane_policy_rule
  // (Kind="collab_policy", RuleID="collab.policy"). GET returns the default
  // policy if no row exists yet; PUT upserts. The policy is compiled into
  // ClawAegis UserConfig.CollabGuard* fields by aegis.Compile and flows
  // through the normal install_skill dispatch pipeline.
  getCollabPolicy: async (): Promise<CollaborationPolicy> => {
    const response = await api.get('/secplane/collab/policy');
    return response.data.data;
  },

  saveCollabPolicy: async (policy: CollaborationPolicy): Promise<CollaborationPolicy> => {
    const response = await api.put('/secplane/collab/policy', policy);
    return response.data.data;
  },

  // dispatchCollabPolicy is a thin wrapper over dispatchAegisApply — the
  // collab_policy row flows through aegis.Compile automatically alongside
  // other rule kinds. Kept as a named method so the frontend doesn't need
  // to know the dispatch endpoint shape.
  dispatchCollabPolicy: async (instanceIds?: number[]): Promise<DispatchResult> => {
    const body = instanceIds && instanceIds.length > 0 ? { instance_ids: instanceIds } : {};
    const response = await api.post('/secplane/dispatch/aegis-apply', body);
    return response.data.data;
  },

  listCollabAlerts: async (limit = 50): Promise<SecplaneAlert[]> => {
    const response = await api.get('/secplane/alerts', {
      params: { source: 'collab_governance', limit },
    });
    return response.data.data?.items ?? [];
  },

  // 拉取 pod 实时 user_config.json（从最近一次 agent 上传的 skill_blob 解出）
  // 区别于 effective-config — 后者只看"最后一次 DISPATCH"，前者看"agent 上报的最新内容"。
  getLiveAegisConfig: async (instanceId: number): Promise<LiveAegisConfig> => {
    const response = await api.get(`/secplane/instances/${instanceId}/aegis/live-config`);
    return response.data.data;
  },

  // 出站可信端点白名单 CRUD（secplane_outbound_trusted 表）
  listOutboundTrusted: async (): Promise<OutboundTrustedEndpoint[]> => {
    const response = await api.get('/secplane/outbound/trusted');
    return response.data.data?.items ?? [];
  },
  createOutboundTrusted: async (
    req: Partial<OutboundTrustedEndpoint> & { domain_pattern: string },
  ): Promise<OutboundTrustedEndpoint> => {
    const response = await api.post('/secplane/outbound/trusted', req);
    return response.data.data;
  },
  deleteOutboundTrusted: async (id: number): Promise<void> => {
    await api.delete(`/secplane/outbound/trusted/${id}`);
  },
  probeOutboundTrusted: async (host: string): Promise<OutboundProbeResult> => {
    const response = await api.post('/secplane/outbound/trusted/probe', { host });
    return response.data.data;
  },
  reprobeOutboundTrusted: async (id: number): Promise<OutboundReprobeResponse> => {
    const response = await api.post(`/secplane/outbound/trusted/${id}/reprobe`);
    return response.data.data;
  },

  // 应急熔断 (kill switch) — enable/disable 会自动触发对所有 running 实例的 dispatchAegisApply
  getKillSwitch: async (): Promise<KillSwitchState> => {
    const response = await api.get('/secplane/kill-switch');
    return response.data.data;
  },
  enableKillSwitch: async (reason: string): Promise<KillSwitchToggleResult> => {
    const response = await api.post('/secplane/kill-switch/enable', { reason });
    return response.data.data;
  },
  disableKillSwitch: async (): Promise<KillSwitchToggleResult> => {
    const response = await api.post('/secplane/kill-switch/disable');
    return response.data.data;
  },
};

export interface KillSwitchState {
  id: number;
  enabled: number; // 0/1
  reason?: string | null;
  set_by?: string | null;
  set_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface KillSwitchToggleResult {
  state: KillSwitchState;
  dispatch: { revision?: string; target_count?: number; error?: string; skipped?: boolean };
}

export interface OutboundProbeResult {
  host: string;
  fingerprint_sha256: string;
  subject_cn: string;
  issuer: string;
  not_after: string;
}

export interface OutboundReprobeResponse {
  endpoint: OutboundTrustedEndpoint;
  probe: OutboundProbeResult;
  previous_fingerprint: string;
  drift: boolean;
}

export interface OutboundTrustedEndpoint {
  id: number;
  domain_pattern: string;
  fingerprint_sha256?: string | null;
  label?: string | null;
  channel?: string | null;
  scope?: string | null;
  status: string;
  expires_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface LiveAegisConfig {
  instance_id: number;
  // Primary path (runtime_config table).
  skill_name?: string;
  revision?: string;
  sha256?: string;
  config_sha256?: string;
  source?: string;
  command_id?: number;
  status?: string;
  dispatched_at?: string;
  // Legacy skill_blob fallback fields.
  skill_id?: number;
  blob_content_hash?: string;
  source_file?: string;
  // "runtime_config" | "skill_blob".
  provenance: string;
  user_config: Record<string, unknown>;
  fetched_at: string;
}
