export interface User {
  id: number;
  username: string;
  email: string;
  role: 'admin' | 'user';
  auth_provider?: 'local' | 'ldap';
  login_alias?: string;
  external_id?: string;
  is_active: boolean;
  created_at: string;
  updated_at: string;
  last_login?: string;
}

export interface UserQuota {
  id: number;
  user_id: number;
  max_instances: number;
  max_cpu_cores: number;
  max_memory_gb: number;
  max_storage_gb: number;
  max_gpu_count: number;
  created_at: string;
  updated_at: string;
}

export interface UpdateUserRequest {
  email?: string;
  is_active?: boolean;
}

export interface UpdateRoleRequest {
  role: 'admin' | 'user';
}

export interface UpdateQuotaRequest {
  max_instances: number;
  max_cpu_cores: number;
  max_memory_gb: number;
  max_storage_gb: number;
  max_gpu_count: number;
}

export interface ListUsersResponse {
  users: User[];
  total: number;
  page: number;
  limit: number;
}
