import type { OpenClawConfigPlan } from "./openclawConfig";
import type { InstanceSkill } from "./skill";

export interface Instance {
  id: number;
  user_id: number;
  name: string;
  description?: string;
  type:
    | "openclaw"
    | "ubuntu"
    | "debian"
    | "centos"
    | "custom"
    | "webtop"
    | "hermes";
  runtime_type: "desktop" | "shell" | "gateway";
  instance_mode: "lite" | "pro";
  status: "creating" | "running" | "stopped" | "error" | "deleting";
  cpu_cores: number;
  memory_gb: number;
  disk_gb: number;
  gpu_enabled: boolean;
  gpu_count: number;
  os_type: string;
  os_version: string;
  image_registry?: string;
  image_tag?: string;
  desktop_stream_profile?: DesktopStreamProfile;
  storage_class: string;
  mount_path: string;
  workspace_path?: string;
  workspace_usage_bytes?: number;
  runtime_generation?: number;
  runtime_error_message?: string;
  pod_name?: string;
  pod_namespace?: string;
  pod_ip?: string;
  access_url?: string;
  openclaw_config_snapshot_id?: number;
  created_at: string;
  updated_at: string;
  started_at?: string;
  stopped_at?: string;
}

export type V2InstanceType = "openclaw" | "hermes";
export type InstanceMode = "lite" | "pro";
export type InstanceAvailability = "available" | "starting" | "unavailable";

export interface InstanceStatus {
  instance_id: number;
  status: string;
  availability?: InstanceAvailability;
  agent_type?: V2InstanceType;
  workspace_usage_bytes?: number;
  pod_name?: string;
  pod_namespace?: string;
  pod_ip?: string;
  pod_status?: string;
  created_at: string;
  started_at?: string;
}

export interface InstanceExternalAccess {
  id: number;
  instance_id: number;
  enabled: boolean;
  auth_mode: "share_link" | "password";
  password_hint?: string;
  expires_at?: string;
  created_by?: number;
  last_used_at?: string;
  created_at: string;
  updated_at: string;
}

export type ExternalAccessExpirationMode = "preset" | "custom" | "permanent";
export type ExternalAccessExpirationPreset = "1h" | "24h" | "7d" | "30d";

export interface ExternalAccessRequest {
  expires_mode?: ExternalAccessExpirationMode;
  expires_preset?: ExternalAccessExpirationPreset;
  expires_at?: string;
}

export interface ExternalAccessStatusResult {
  external_access?: InstanceExternalAccess | null;
  share_url?: string;
  password?: string;
}

export interface EnableShareLinkResult {
  access: InstanceExternalAccess;
  share_url?: string;
}

export interface PasswordExternalAccessResult {
  access: InstanceExternalAccess;
  password: string;
  share_url?: string;
}

export interface AgentInfo {
  agent_id: string;
  agent_version: string;
  protocol_version: string;
  status: string;
  capabilities: string[];
  host_info?: Record<string, unknown>;
  last_heartbeat_at?: string;
  last_reported_at?: string;
  last_seen_ip?: string;
  registered_at?: string;
}

export interface RuntimeStatus {
  instance_id: number;
  infra_status: string;
  agent_status: string;
  openclaw_status: string;
  openclaw_pid?: number;
  openclaw_version?: string;
  current_config_revision_id?: number;
  desired_config_revision_id?: number;
  system_info?: Record<string, unknown>;
  health?: Record<string, unknown>;
  summary?: Record<string, unknown>;
  last_reported_at?: string;
}

export interface InstanceRuntimeCommand {
  id: number;
  command_type: string;
  status: string;
  idempotency_key: string;
  issued_by?: number;
  issued_at: string;
  dispatched_at?: string;
  started_at?: string;
  finished_at?: string;
  timeout_seconds: number;
  payload?: Record<string, unknown>;
  result?: Record<string, unknown>;
  error_message?: string;
}

export interface InstanceRuntimeDetails {
  runtime?: RuntimeStatus;
  agent?: AgentInfo;
  commands: InstanceRuntimeCommand[];
  skills?: InstanceSkill[];
}

export interface InstanceConfigRevision {
  id: number;
  instance_id: number;
  source_snapshot_id?: number;
  source_bundle_id?: number;
  revision_no: number;
  checksum: string;
  status: string;
  published_by?: number;
  published_at?: string;
  activated_at?: string;
  content: unknown;
}

export interface CreateInstanceRequest {
  name: string;
  description?: string;
  type:
    | "openclaw"
    | "ubuntu"
    | "debian"
    | "centos"
    | "custom"
    | "webtop"
    | "hermes";
  mode?: InstanceMode;
  instance_mode?: InstanceMode;
  runtime_type?: "desktop" | "shell" | "gateway";
  desktop_stream_profile?: DesktopStreamProfile;
  cpu_cores: number;
  memory_gb: number;
  disk_gb: number;
  gpu_enabled?: boolean;
  gpu_count?: number;
  os_type: string;
  os_version: string;
  image_registry?: string;
  image_tag?: string;
  environment_overrides?: Record<string, string>;
  storage_class?: string;
  openclaw_config_plan?: OpenClawConfigPlan;
  skill_ids?: number[];
}

export interface BatchCreateLiteInstancesRequest {
  name_prefix: string;
  count: number;
  start_index?: number;
  template?: Partial<CreateInstanceRequest>;
}

export interface BatchCreateLiteInstanceResult {
  name: string;
  status: "created" | "failed";
  instance?: Instance;
  error?: string;
}

export interface BatchCreateLiteInstancesResponse {
  requested: number;
  created: number;
  failed: number;
  results: BatchCreateLiteInstanceResult[];
}

export interface BatchDeleteLiteInstancesRequest {
  instance_ids: number[];
}

export interface BatchDeleteLiteInstanceResult {
  instance_id: number;
  name?: string;
  status: "deleting" | "failed";
  error?: string;
}

export interface BatchDeleteLiteInstancesResponse {
  requested: number;
  deleted: number;
  failed: number;
  results: BatchDeleteLiteInstanceResult[];
}
export interface UpdateInstanceRequest {
  name?: string;
  description?: string;
  desktop_stream_profile?: DesktopStreamProfile;
}

export type DesktopStreamProfile = "low" | "standard" | "high";

export interface InstanceListResponse {
  instances: Instance[];
  total: number;
  page: number;
  limit: number;
}

export interface InstanceType {
  id: string;
  name: string;
  description: string;
  icon: string;
  defaultOs: string;
  defaultVersion: string;
}

export const INSTANCE_TYPES: InstanceType[] = [
  {
    id: "ubuntu",
    name: "Ubuntu Desktop",
    description: "Popular Linux distribution with GNOME desktop",
    icon: "ubuntu",
    defaultOs: "ubuntu",
    defaultVersion: "22.04",
  },
  {
    id: "debian",
    name: "Debian Desktop",
    description: "Stable and secure Linux distribution",
    icon: "debian",
    defaultOs: "debian",
    defaultVersion: "12",
  },
  {
    id: "centos",
    name: "CentOS Desktop",
    description: "Enterprise-class Linux distribution",
    icon: "centos",
    defaultOs: "centos",
    defaultVersion: "9",
  },
  {
    id: "openclaw",
    name: "OpenClaw Desktop",
    description: "Optimized desktop environment",
    icon: "openclaw",
    defaultOs: "openclaw",
    defaultVersion: "latest",
  },
  {
    id: "webtop",
    name: "Webtop Desktop",
    description: "Browser-based Linux desktop proxied through ClawManager",
    icon: "webtop",
    defaultOs: "ubuntu",
    defaultVersion: "xfce",
  },
  {
    id: "hermes",
    name: "Hermes Runtime",
    description: "Hermes runtime built on the webtop desktop base",
    icon: "hermes",
    defaultOs: "hermes",
    defaultVersion: "latest",
  },
  {
    id: "custom",
    name: "Custom Image",
    description: "Use your own custom image",
    icon: "custom",
    defaultOs: "custom",
    defaultVersion: "latest",
  },
];

export const PRESET_CONFIGS = {
  small: {
    name: "Small",
    cpu_cores: 2,
    memory_gb: 4,
    disk_gb: 20,
    description: "Suitable for light tasks",
  },
  medium: {
    name: "Medium",
    cpu_cores: 4,
    memory_gb: 8,
    disk_gb: 50,
    description: "Good for development",
  },
  large: {
    name: "Large",
    cpu_cores: 8,
    memory_gb: 16,
    disk_gb: 100,
    description: "For heavy workloads",
  },
};
