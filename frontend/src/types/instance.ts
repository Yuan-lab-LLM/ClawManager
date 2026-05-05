import type { OpenClawConfigPlan } from "./openclawConfig";
import type { InstanceSkill } from "./skill";

export interface Instance {
  id: number;
  user_id: number;
  name: string;
  description?: string;
  type: "openclaw" | "ubuntu" | "debian" | "centos" | "custom" | "webtop";
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
  storage_class: string;
  mount_path: string;
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

export interface InstanceStatus {
  instance_id: number;
  status: string;
  pod_name?: string;
  pod_namespace?: string;
  pod_ip?: string;
  pod_status?: string;
  created_at: string;
  started_at?: string;
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
  type: "openclaw" | "ubuntu" | "debian" | "centos" | "custom" | "webtop";
  cpu_cores: number;
  memory_gb: number;
  disk_gb: number;
  gpu_enabled?: boolean;
  gpu_count?: number;
  os_type: string;
  os_version: string;
  image_registry?: string;
  image_tag?: string;
  storage_class?: string;
  openclaw_config_plan?: OpenClawConfigPlan;
  skill_ids?: number[];
}

export interface UpdateInstanceRequest {
  name?: string;
  description?: string;
}

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
  nameKey: string;
  descriptionKey: string;
  icon: string;
  defaultOs: string;
  defaultVersion: string;
}

export const INSTANCE_TYPES: InstanceType[] = [
  {
    id: "ubuntu",
    name: "Ubuntu Desktop",
    description: "Popular Linux distribution with GNOME desktop",
    nameKey: "instances.instanceTypes.ubuntu.name",
    descriptionKey: "instances.instanceTypes.ubuntu.description",
    icon: "ubuntu",
    defaultOs: "ubuntu",
    defaultVersion: "22.04",
  },
  {
    id: "debian",
    name: "Debian Desktop",
    description: "Stable and secure Linux distribution",
    nameKey: "instances.instanceTypes.debian.name",
    descriptionKey: "instances.instanceTypes.debian.description",
    icon: "debian",
    defaultOs: "debian",
    defaultVersion: "12",
  },
  {
    id: "centos",
    name: "CentOS Desktop",
    description: "Enterprise-class Linux distribution",
    nameKey: "instances.instanceTypes.centos.name",
    descriptionKey: "instances.instanceTypes.centos.description",
    icon: "centos",
    defaultOs: "centos",
    defaultVersion: "9",
  },
  {
    id: "openclaw",
    name: "GTClaw Desktop",
    description: "Optimized desktop environment",
    nameKey: "instances.instanceTypes.openclaw.name",
    descriptionKey: "instances.instanceTypes.openclaw.description",
    icon: "openclaw",
    defaultOs: "openclaw",
    defaultVersion: "latest",
  },
  {
    id: "webtop",
    name: "Webtop Desktop",
    description: "Browser-based Linux desktop proxied through GTManager",
    nameKey: "instances.instanceTypes.webtop.name",
    descriptionKey: "instances.instanceTypes.webtop.description",
    icon: "webtop",
    defaultOs: "ubuntu",
    defaultVersion: "xfce",
  },
  {
    id: "custom",
    name: "Custom Image",
    description: "Use your own custom image",
    nameKey: "instances.instanceTypes.custom.name",
    descriptionKey: "instances.instanceTypes.custom.description",
    icon: "custom",
    defaultOs: "custom",
    defaultVersion: "latest",
  },
];

export const PRESET_CONFIGS = {
  small: {
    name: "Small",
    nameKey: "instances.presets.small.name",
    cpu_cores: 2,
    memory_gb: 4,
    disk_gb: 20,
    description: "Suitable for light tasks",
    descriptionKey: "instances.presets.small.description",
  },
  medium: {
    name: "Medium",
    nameKey: "instances.presets.medium.name",
    cpu_cores: 4,
    memory_gb: 8,
    disk_gb: 50,
    description: "Good for development",
    descriptionKey: "instances.presets.medium.description",
  },
  large: {
    name: "Large",
    nameKey: "instances.presets.large.name",
    cpu_cores: 8,
    memory_gb: 16,
    disk_gb: 100,
    description: "For heavy workloads",
    descriptionKey: "instances.presets.large.description",
  },
};
