import api from './api';
import type { 
  User, 
  UserQuota, 
  UpdateUserRequest, 
  UpdateRoleRequest, 
  UpdateQuotaRequest,
  ListUsersResponse,
} from '../types/user';

export interface CreateUserRequest {
  username: string;
  email: string;
  password: string;
  role: 'admin' | 'user';
}

export interface ImportUsersResponse {
  created_count: number;
  failed_count: number;
  created_users: Array<{
    username: string;
    login_alias?: string;
    email: string;
    role: 'admin' | 'user';
    auth_provider: 'local' | 'ldap';
    warning_codes?: string[];
    max_instances: number;
    max_cpu_cores: number;
    max_memory_gb: number;
    max_storage_gb: number;
    max_gpu_count: number;
    initial_password?: string;
  }>;
  updated_count?: number;
  updated_users?: Array<{
    username: string;
    login_alias?: string;
    email: string;
    role: 'admin' | 'user';
    auth_provider: 'ldap';
  }>;
  errors: Array<{
    line: number;
    username?: string;
    error: string;
  }>;
  skipped_count?: number;
  skipped?: Array<{ line: number; username?: string; error: string }>;
}

export interface LDAPImportUser {
  external_id: string;
  username: string;
  email: string;
  role?: 'admin' | 'user';
  error?: string;
  status: 'ready' | 'exists' | 'pending_alias' | 'invalid' | string;
}

export interface LDAPPreviewResponse {
  users: LDAPImportUser[];
  total: number;
}

export interface LDAPPreviewRequest {
  query?: string;
  limit?: number;
}

export interface LDAPImportRequest {
  role: 'admin' | 'user';
  max_instances: number;
  max_cpu_cores: number;
  max_memory_gb: number;
  max_storage_gb: number;
  max_gpu_count: number;
  query?: string;
  limit?: number;
  external_ids?: string[];
}

export const userService = {
  // Create new user (admin only)
  createUser: async (data: CreateUserRequest): Promise<User> => {
    const response = await api.post('/users', data);
    return response.data.data;
  },

  importUsers: async (file: File): Promise<ImportUsersResponse> => {
    const formData = new FormData();
    formData.append('file', file);
    const response = await api.post('/users/import', formData, {
      headers: {
        'Content-Type': 'multipart/form-data',
      },
    });
    return response.data.data;
  },

  previewLDAPUsers: async (params: LDAPPreviewRequest = {}): Promise<LDAPPreviewResponse> => {
    const response = await api.get('/users/import/ldap/preview', { params });
    return response.data.data;
  },

  importLDAPUsers: async (data: LDAPImportRequest): Promise<ImportUsersResponse> => {
    const response = await api.post('/users/import/ldap', data);
    return response.data.data;
  },

  // Get all users (admin only)
  getUsers: async (page = 1, limit = 20): Promise<ListUsersResponse> => {
    const response = await api.get('/users', {
      params: { page, limit }
    });
    return response.data.data || { users: [], total: 0, page, limit };
  },

  // Get user by ID
  getUser: async (id: number): Promise<User> => {
    const response = await api.get(`/users/${id}`);
    return response.data.data;
  },

  // Update user
  updateUser: async (id: number, data: UpdateUserRequest): Promise<User> => {
    const response = await api.put(`/users/${id}`, data);
    return response.data.data;
  },

  // Delete user (admin only)
  deleteUser: async (id: number): Promise<void> => {
    await api.delete(`/users/${id}`);
  },

  // Disable LDAP login for a provisioned user (admin only)
  disableUser: async (id: number): Promise<User> => {
    const response = await api.put(`/users/${id}`, { is_active: false });
    return response.data.data;
  },

  // Restore LDAP login for a disabled provisioned user (admin only)
  restoreUser: async (id: number): Promise<User> => {
    const response = await api.put(`/users/${id}`, { is_active: true });
    return response.data.data;
  },

  // Update user role (admin only)
  updateRole: async (id: number, data: UpdateRoleRequest): Promise<void> => {
    await api.put(`/users/${id}/role`, data);
  },

  // Get user quota
  getUserQuota: async (id: number): Promise<UserQuota> => {
    const response = await api.get(`/users/${id}/quota`);
    return response.data.data;
  },

  // Update user quota (admin only)
  updateQuota: async (id: number, data: UpdateQuotaRequest): Promise<UserQuota> => {
    const response = await api.put(`/users/${id}/quota`, data);
    return response.data.data;
  },
};
