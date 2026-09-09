import api from './api';
import type { 
  LoginRequest, 
  RegisterRequest, 
  RefreshTokenRequest,
  AuthResponse,
  User 
} from '../types/auth';

export interface ChangePasswordRequest {
  current_password: string;
  new_password: string;
}

export const authService = {
  // Login
  login: async (data: LoginRequest): Promise<AuthResponse> => {
    const response = await api.post('/auth/login', data);
    if (response.data.data) {
      const { access_token, refresh_token } = response.data.data as { access_token: string; refresh_token: string; expires_in: number };
      localStorage.setItem('access_token', access_token);
      localStorage.setItem('refresh_token', refresh_token);
    }
    return response.data;
  },

  // Register
  register: async (data: RegisterRequest): Promise<AuthResponse> => {
    const response = await api.post('/auth/register', data);
    return response.data;
  },

  // Logout
  logout: async (): Promise<void> => {
    try {
      await api.post('/auth/logout');
    } finally {
      // A failed server revocation must not restore local login on reload.
      // Preserve the rejection so callers can report that revocation failed.
      localStorage.removeItem('access_token');
      localStorage.removeItem('refresh_token');
    }
  },

  // Refresh token
  refreshToken: async (data: RefreshTokenRequest): Promise<AuthResponse> => {
    const response = await api.post('/auth/refresh', data);
    if (response.data.data) {
      const { access_token, refresh_token } = response.data.data as { access_token: string; refresh_token: string; expires_in: number };
      localStorage.setItem('access_token', access_token);
      localStorage.setItem('refresh_token', refresh_token);
    }
    return response.data;
  },

  // Get current user
  getCurrentUser: async (): Promise<User> => {
    const response = await api.get('/auth/me');
    return response.data.data;
  },

  // Change password
  changePassword: async (data: ChangePasswordRequest): Promise<void> => {
    await api.post('/auth/change-password', data);
  },
};
