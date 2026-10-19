export type RuntimeType =
  | "openclaw"
  | "hermes"
  | "opencode"
  | "deepseek-harness";

export interface RuntimePod {
  id: number;
  runtime_type: RuntimeType;
  namespace: string;
  pod_name: string;
  pod_ip?: string;
  node_name?: string;
  deployment_name: string;
  image_ref: string;
  openclaw_version?: string;
  agent_protocol_version?: string;
  team_plugin_version?: string;
  session_store?: string;
  image_digest?: string;
  capabilities: string[];
  state: "pending" | "ready" | "draining" | "unhealthy" | "deleted" | string;
  used_slots: number;
  capacity: number;
  draining: boolean;
  cpu_millis_used: number;
  memory_bytes_used: number;
  disk_bytes_used: number;
  network_rx_bytes: number;
  network_tx_bytes: number;
  last_seen_at?: string;
  updated_at?: string;
  agent_reported?: boolean;
  pool_role?: "upgrade-target" | "upgrade-lab" | string;
  pool_purpose?: string;
  upgrade_id?: string;
  source_deployment?: string;
  scheduling_enabled?: boolean;
}

export interface RuntimeGateway {
  id: number;
  instance_id: number;
  runtime_pod_id: number;
  runtime_type: RuntimeType;
  gateway_id: string;
  gateway_port: number;
  gateway_pid?: number;
  workspace_path: string;
  state: string;
  generation: number;
  last_health_at?: string;
  error_message?: string;
}

export interface StartRuntimeRolloutRequest {
  runtime_type: RuntimeType;
  target_image_ref: string;
  batch_size: number;
  max_unavailable: number;
  preflight_id?: string;
  auto_rollback?: boolean;
  exclude_instance_ids?: number[];
}

export interface RuntimeRollout {
  id: number;
  runtime_type: RuntimeType;
  target_image_ref: string;
  target_image_digest?: string;
  status: string;
  phase: string;
  batch_size?: number;
  preflight_id?: string;
  plan_fingerprint?: string;
  auto_rollback: boolean;
  rollback_status?: string;
  rollback_error?: string;
  error_message?: string;
}

export interface RuntimeUpgradePreflightResult {
	isolated?: Array<{instance_id:number;team_id?:number;reason:string}>;
  unavailable?: Array<{instance_id:number;team_id?:number;reason:string}>;
  excluded_instance_ids?: number[];
  rollout?: RuntimeRollout;
  strategy: "legacy_rolling" | "openclaw_8plus_data_safe" | string;
  target_image_ref: string;
  target_runtime_version?: string;
  target_upgrade_protocol?: string;
  passed: boolean;
  blockers: string[];
  warnings: string[];
  instance_count: number;
  team_count: number;
  openclaw_team_member_count: number;
  hermes_team_member_count: number;
  required_capabilities: string[];
  empty_pool_reset?: boolean;
}

export interface RuntimeUpgradeItem {
	isolated_at?: string;
	isolation_phase?: string;
	phase_entered_at?: string;
  id: number;
  instance_id: number;
  team_id?: number;
  state: string;
  operation_phase?: string;
  operation_started_at?: string;
  operation_deadline_at?: string;
  operation_attempts?: number;
	operation_failures?: number;
	operation_next_retry_at?: string;
	operation_last_error?: string;
  error_message?: string;
  recovery_status?: "pending" | "starting" | "retrying" | "needs_attention" | "verified" | "source_restore_ready" | "source_restored";
  recovery_error_code?: string;
  restart_attempts?: number;
  restart_deadline_at?: string;
  restart_next_retry_at?: string;
}

export interface RuntimeUpgradeDetails {
  execution?: { version: number; check: number; migrate: number; start: number; restore: number };
  pool_progress?: {
    version: number;
    completed: number;
    applying: boolean;
    targets: Array<{ Namespace: string; Name: string; Image: string }>;
  } | null;
  rollout: RuntimeRollout;
  items: RuntimeUpgradeItem[];
  audits: Array<{ id: number; action: string; phase: string; outcome: string; created_at: string }>;
}
