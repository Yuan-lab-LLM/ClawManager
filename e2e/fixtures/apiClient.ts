import type { APIRequestContext, APIResponse } from "@playwright/test";
import { env } from "./env.js";
import { users } from "./users.js";

interface ApiEnvelope<T> {
  success: boolean;
  message?: string;
  data: T;
  error?: string;
}

export interface LoginTokens {
  access_token: string;
  refresh_token: string;
  expires_in: number;
}

export interface CurrentUser {
  id: number;
  username: string;
  email: string;
  role: string;
}

export interface RuntimePod {
  id: number;
  runtime_type: "openclaw" | "hermes";
  namespace: string;
  pod_name: string;
  pod_uid?: string;
  pod_ip?: string;
  node_name?: string;
  deployment_name: string;
  image_ref: string;
  state: string;
  capacity: number;
  used_slots: number;
  draining: boolean;
}

export interface InstanceRecord {
  id: number;
  user_id: number;
  name: string;
  type: string;
  runtime_type: "desktop" | "shell" | "gateway" | string;
  instance_mode?: "lite" | "pro" | string;
  status: "creating" | "running" | "stopped" | "error" | "deleting" | string;
  cpu_cores: number;
  memory_gb: number;
  disk_gb: number;
  gpu_enabled: boolean;
  gpu_count: number;
  os_type?: string;
  os_version?: string;
  pod_name?: string;
  pod_namespace?: string;
  pod_ip?: string;
  workspace_usage_bytes?: number;
  created_at?: string;
  updated_at?: string;
}

export interface InstanceListResponse {
  instances: InstanceRecord[];
  total: number;
  page: number;
  limit: number;
}

export interface TeamRecord {
  id: number;
  user_id: number;
  name: string;
  description?: string;
  status: "creating" | "running" | "failed" | "deleting" | "deleted" | string;
  communication_mode: string;
  shared_mount_path: string;
  created_at?: string;
  updated_at?: string;
}

export interface TeamMemberRecord {
  id: number;
  team_id: number;
  instance_id?: number;
  user_id: number;
  member_key: string;
  display_name: string;
  role: string;
  runtime_type?: "openclaw" | "hermes" | string;
  instance_mode?: "lite" | "pro" | string;
  status: "creating" | "idle" | "busy" | "failed" | "offline" | "deleting" | "deleted" | string;
}

export interface TeamDetailsRecord {
  team: TeamRecord;
  leader_member_id?: string;
  leader?: TeamMemberRecord;
  members: TeamMemberRecord[];
}

export interface CreateTeamMemberRequest {
  member_id?: string;
  name?: string;
  role: string;
  mode?: "lite" | "pro";
  instance_mode?: "lite" | "pro";
  runtime_type?: "openclaw" | "hermes";
  description?: string;
  cpu_cores?: number;
  memory_gb?: number;
  disk_gb?: number;
  gpu_enabled?: boolean;
  gpu_count?: number;
  is_leader?: boolean;
}

export interface CreateTeamRequest {
  name: string;
  description?: string;
  communication_mode?: string;
  shared_storage_gb?: number;
  members: CreateTeamMemberRequest[];
}

export interface InstanceExternalAccess {
  id: number;
  instance_id: number;
  enabled: boolean;
  auth_mode: "share_link" | "password" | string;
  password_hint?: string;
  expires_at?: string;
  created_by?: number;
  last_used_at?: string;
  created_at: string;
  updated_at: string;
}

export interface ExternalAccessRequest {
  expires_mode?: "preset" | "custom" | "permanent";
  expires_preset?: "1h" | "24h" | "7d" | "30d";
  expires_at?: string;
}

export interface PasswordExternalAccessResult {
  access: InstanceExternalAccess;
  password: string;
  share_url?: string;
}

export interface RuntimePodRegisterRequest {
  runtime_type: "openclaw" | "hermes" | string;
  namespace: string;
  pod_name: string;
  pod_uid?: string;
  pod_ip?: string;
  node_name?: string;
  deployment_name: string;
  image_ref: string;
  agent_endpoint?: string;
  state: string;
  capacity: number;
  used_slots: number;
  draining: boolean;
}

export interface RuntimePodIdentity {
  pod_id?: number;
  namespace?: string;
  pod_name?: string;
}

export interface RuntimeHeartbeatRequest extends RuntimePodIdentity {
  state: string;
  used_slots: number;
  draining: boolean;
}

export interface LoginUser {
  username: string;
  password: string;
}

export interface RegisterUser extends LoginUser {
  email: string;
}

export const defaultRuntimePod: RuntimePodRegisterRequest = {
  runtime_type: "openclaw",
  namespace: "clawmanager-e2e",
  pod_name: "e2e-openclaw-runtime-p0",
  pod_uid: "e2e-openclaw-runtime-p0-uid",
  pod_ip: "10.244.0.25",
  node_name: "e2e-node",
  deployment_name: "openclaw-runtime",
  image_ref: "e2e/openclaw:latest",
  agent_endpoint: "http://10.244.0.25:18080",
  state: "ready",
  capacity: 100,
  used_slots: 0,
  draining: false
};

function bearer(accessToken: string) {
  return { Authorization: `Bearer ${accessToken}` };
}

function runtimeAgentHeaders() {
  return { "X-ClawManager-Agent-Token": env.runtimeAgentToken };
}

async function parseJson<T>(response: APIResponse): Promise<T> {
  const text = await response.text();
  if (!text) {
    return undefined as T;
  }
  return JSON.parse(text) as T;
}

async function expectOkEnvelope<T>(response: APIResponse): Promise<T> {
  const payload = await parseJson<ApiEnvelope<T>>(response);
  if (!response.ok() || !payload.success) {
    throw new Error(payload?.error || `request failed with status ${response.status()}`);
  }
  return payload.data;
}

export async function login(
  request: APIRequestContext,
  user: LoginUser = users.admin
): Promise<LoginTokens> {
  const response = await request.post(`${env.backendUrl}/auth/login`, {
    data: {
      username: user.username,
      password: user.password
    }
  });
  return expectOkEnvelope<LoginTokens>(response);
}

export async function registerUser(
  request: APIRequestContext,
  user: RegisterUser = users.user
): Promise<void> {
  const response = await request.post(`${env.backendUrl}/auth/register`, {
    data: {
      username: user.username,
      email: user.email,
      password: user.password
    }
  });
  await expectOkEnvelope<unknown>(response);
}

export async function getMe(
  request: APIRequestContext,
  accessToken: string
): Promise<CurrentUser> {
  const response = await request.get(`${env.backendUrl}/auth/me`, {
    headers: bearer(accessToken)
  });
  return expectOkEnvelope<CurrentUser>(response);
}

export async function getMeRaw(
  request: APIRequestContext,
  accessToken?: string
): Promise<APIResponse> {
  return request.get(`${env.backendUrl}/auth/me`, {
    headers: accessToken ? bearer(accessToken) : undefined
  });
}

export async function postRuntimePodRegisterRaw(
  request: APIRequestContext,
  overrides: Partial<RuntimePodRegisterRequest> = {}
): Promise<APIResponse> {
  return request.post(`${env.backendUrl}/runtime-agent/register`, {
    headers: runtimeAgentHeaders(),
    data: {
      ...defaultRuntimePod,
      ...overrides
    }
  });
}

export async function registerRuntimePod(
  request: APIRequestContext,
  overrides: Partial<RuntimePodRegisterRequest> = {}
): Promise<RuntimePod> {
  const response = await postRuntimePodRegisterRaw(request, overrides);
  const data = await expectOkEnvelope<{ pod: RuntimePod }>(response);
  return data.pod;
}

export async function sendRuntimeHeartbeat(
  request: APIRequestContext,
  identity: RuntimePodIdentity,
  overrides: Partial<RuntimeHeartbeatRequest> = {}
): Promise<void> {
  const response = await request.post(`${env.backendUrl}/runtime-agent/heartbeat`, {
    headers: runtimeAgentHeaders(),
    data: {
      state: "ready",
      used_slots: 0,
      draining: false,
      ...identity,
      ...overrides
    }
  });
  await expectOkEnvelope<null>(response);
}

export async function listRuntimePods(
  request: APIRequestContext,
  accessToken: string
): Promise<RuntimePod[]> {
  const response = await request.get(`${env.backendUrl}/admin/runtime-pods`, {
    headers: bearer(accessToken)
  });
  const data = await expectOkEnvelope<{ pods: RuntimePod[] }>(response);
  return data.pods;
}

export async function listInstances(
  request: APIRequestContext,
  accessToken: string,
  options: { admin?: boolean; page?: number; limit?: number } = {}
): Promise<InstanceListResponse> {
  const endpoint = options.admin ? "/admin/instances" : "/instances";
  const response = await request.get(`${env.backendUrl}${endpoint}`, {
    headers: bearer(accessToken),
    params: {
      page: options.page ?? 1,
      limit: options.limit ?? 100
    }
  });
  return expectOkEnvelope<InstanceListResponse>(response);
}

export async function createTeam(
  request: APIRequestContext,
  accessToken: string,
  data: CreateTeamRequest
): Promise<TeamDetailsRecord> {
  const response = await request.post(`${env.backendUrl}/teams`, {
    headers: bearer(accessToken),
    data
  });
  return expectOkEnvelope<TeamDetailsRecord>(response);
}

export async function getTeam(
  request: APIRequestContext,
  accessToken: string,
  teamId: number
): Promise<TeamDetailsRecord> {
  const response = await request.get(`${env.backendUrl}/teams/${teamId}`, {
    headers: bearer(accessToken)
  });
  return expectOkEnvelope<TeamDetailsRecord>(response);
}

export async function deleteTeam(
  request: APIRequestContext,
  accessToken: string,
  teamId: number
): Promise<void> {
  const response = await request.delete(`${env.backendUrl}/teams/${teamId}`, {
    headers: bearer(accessToken)
  });
  await expectOkEnvelope<{ id: number }>(response);
}

export async function createExternalAccessPassword(
  request: APIRequestContext,
  accessToken: string,
  instanceId: number,
  data: ExternalAccessRequest = { expires_mode: "preset", expires_preset: "1h" }
): Promise<PasswordExternalAccessResult> {
  const response = await request.post(`${env.backendUrl}/instances/${instanceId}/external-access/password`, {
    headers: bearer(accessToken),
    data
  });
  return expectOkEnvelope<PasswordExternalAccessResult>(response);
}

export async function disableExternalAccess(
  request: APIRequestContext,
  accessToken: string,
  instanceId: number
): Promise<void> {
  const response = await request.delete(`${env.backendUrl}/instances/${instanceId}/external-access`, {
    headers: bearer(accessToken)
  });
  await expectOkEnvelope<null>(response);
}

export interface GatewayModelSummary {
  id: number | string;
  display_name?: string;
}

export async function listGatewayModels(
  request: APIRequestContext,
  accessToken: string,
): Promise<GatewayModelSummary[]> {
  const response = await request.get(`${env.backendUrl}/gateway/llm/models`, {
    headers: bearer(accessToken),
  });
  const body = await expectOkEnvelope<{ items: GatewayModelSummary[] }>(response);
  return body.items ?? [];
}

export async function gatewayChatCompletion(
  request: APIRequestContext,
  accessToken: string,
  payload: {
    model: string;
    instance_id?: number;
    messages: Array<{ role: string; content: string }>;
    stream?: boolean;
  },
  extraHeaders: Record<string, string> = {},
) {
  return request.post(`${env.backendUrl}/gateway/llm/chat/completions`, {
    headers: {
      ...bearer(accessToken),
      ...extraHeaders,
    },
    data: payload,
  });
}

export interface LLMGovernanceOverview {
  total_managed_instances: number;
  non_compliant_count: number;
  external_config_count: number;
  high_fallback_count: number;
  items: Array<{
    instance_id: number;
    instance_name: string;
    is_compliant: boolean;
  }>;
}

export async function getLLMGovernanceOverview(
  request: APIRequestContext,
  accessToken: string,
): Promise<LLMGovernanceOverview> {
  const response = await request.get(`${env.backendUrl}/admin/llm-governance/overview`, {
    headers: bearer(accessToken),
  });
  return expectOkEnvelope<LLMGovernanceOverview>(response);
}

export interface InstanceSessionUsageResult {
  summary: {
    total_prompt_tokens: number;
    total_completion_tokens: number;
    total_tokens: number;
    total_estimated_cost: number;
    currency: string;
    session_count: number;
  };
  compliance: {
    fallback_session_count: number;
    has_fallback_sessions: boolean;
    recent_fallback_audit_count: number;
  };
  items: Array<{
    session_id: string;
    session_key: string;
    total_tokens: number;
    invocation_count: number;
  }>;
  total: number;
  page: number;
  limit: number;
}

export interface SessionUsageOverviewResult {
  summary: {
    total_tokens: number;
    total_estimated_cost: number;
    currency: string;
    session_count: number;
  };
  items: Array<{
    instance_id: number;
    instance_name: string;
    instance_type: string;
    user_id: number;
    summary: {
      total_tokens: number;
      session_count: number;
      total_estimated_cost: number;
      currency: string;
    };
    compliance: {
      fallback_session_count: number;
      has_fallback_sessions: boolean;
    };
  }>;
  total: number;
  page: number;
  limit: number;
}

export async function getInstanceSessionUsage(
  request: APIRequestContext,
  accessToken: string,
  instanceId: number,
  params?: { page?: number; limit?: number; search?: string; since?: string; until?: string },
): Promise<InstanceSessionUsageResult> {
  const response = await request.get(`${env.backendUrl}/instances/${instanceId}/session-usage`, {
    headers: bearer(accessToken),
    params,
  });
  return expectOkEnvelope<InstanceSessionUsageResult>(response);
}

export async function getAdminSessionUsageOverview(
  request: APIRequestContext,
  accessToken: string,
  params?: { page?: number; limit?: number; search?: string; since?: string; until?: string },
): Promise<SessionUsageOverviewResult> {
  const response = await request.get(`${env.backendUrl}/admin/session-usage/overview`, {
    headers: bearer(accessToken),
    params,
  });
  return expectOkEnvelope<SessionUsageOverviewResult>(response);
}
