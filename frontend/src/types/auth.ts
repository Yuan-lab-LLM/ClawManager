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

export interface TokenPair {
  access_token: string;
  refresh_token: string;
  expires_in: number;
}

export interface LoginRequest {
  username: string;
  password: string;
}

export interface RegisterRequest {
  username: string;
  email: string;
  password: string;
}

export interface RefreshTokenRequest {
  refresh_token: string;
}

export interface AuthResponse {
  success: boolean;
  message: string;
  data: TokenPair | User;
}
