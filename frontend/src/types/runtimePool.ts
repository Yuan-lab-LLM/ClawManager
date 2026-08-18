export type RuntimeType = "openclaw" | "hermes" | "opencode";

export interface RuntimePod {
  id: number;
  runtime_type: RuntimeType;
  namespace: string;
  pod_name: string;
  pod_ip?: string;
  node_name?: string;
  deployment_name: string;
  image_ref: string;
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
}
