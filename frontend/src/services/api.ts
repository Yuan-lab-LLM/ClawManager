import axios from 'axios';
import type { AxiosInstance, AxiosError, InternalAxiosRequestConfig } from 'axios';

const API_BASE_URL = import.meta.env.VITE_API_URL || '/api/v1';

// Create axios instance
const api: AxiosInstance = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
});

const AUTH_FLOW_ENDPOINTS = ['/auth/login', '/auth/register', '/auth/refresh'];

function isAuthFlowRequest(url?: string): boolean {
  if (!url) {
    return false;
  }

  const path = url.split('?')[0];
  return AUTH_FLOW_ENDPOINTS.some((endpoint) => path.endsWith(endpoint));
}

// Token refresh state
let isRefreshing = false;
let refreshSubscribers: ((token: string) => void)[] = [];

// Subscribe to token refresh
function subscribeTokenRefresh(callback: (token: string) => void) {
  refreshSubscribers.push(callback);
}

// Notify all subscribers with new token
function onTokenRefreshed(newToken: string) {
  refreshSubscribers.forEach((callback) => callback(newToken));
  refreshSubscribers = [];
}

// Request interceptor - add auth token
api.interceptors.request.use(
  (config) => {
    const token = localStorage.getItem('access_token');
    if (token) {
      config.headers.Authorization = `Bearer ${token}`;
    }
    return config;
  },
  (error) => {
    return Promise.reject(error);
  }
);

// Response interceptor - handle errors
api.interceptors.response.use(
  (response) => response,
  async (error: AxiosError) => {
    const originalRequest = error.config as InternalAxiosRequestConfig & { _retry?: boolean };

    // A backup strong-auth challenge requires a separate recent MFA proof.
    // Refreshing the ordinary session cannot satisfy it and may log out a
    // correctly authenticated administrator instead of showing the challenge.
    const responseCode = (error.response?.data as { code?: string } | undefined)?.code;
    if (
      error.response?.status === 401 &&
      originalRequest?.url?.startsWith('/admin/system-backup/') &&
      typeof responseCode === 'string' && responseCode.startsWith('strong_auth_')
    ) {
      return Promise.reject(error);
    }
    
    // If error is not 401 or request is already retried, reject
    if (
      error.response?.status !== 401 ||
      !originalRequest ||
      originalRequest._retry ||
      isAuthFlowRequest(originalRequest.url)
    ) {
      return Promise.reject(error);
    }
    
    const refreshToken = localStorage.getItem('refresh_token');
    if (!refreshToken) {
      // No refresh token, redirect to login
      localStorage.removeItem('access_token');
      window.location.href = '/login';
      return Promise.reject(error);
    }
    
    // If already refreshing, queue this request
    if (isRefreshing) {
      return new Promise((resolve) => {
        subscribeTokenRefresh((newToken: string) => {
          originalRequest._retry = true;
          originalRequest.headers.Authorization = `Bearer ${newToken}`;
          resolve(api(originalRequest));
        });
      });
    }
    
    // Mark as retried to prevent infinite loop
    originalRequest._retry = true;
    isRefreshing = true;
    
    try {
      const response = await axios.post(`${API_BASE_URL}/auth/refresh`, {
        refresh_token: refreshToken,
      });
      
      const { access_token, refresh_token } = response.data.data;
      localStorage.setItem('access_token', access_token);
      localStorage.setItem('refresh_token', refresh_token);
      
      // Update request header and retry
      originalRequest.headers.Authorization = `Bearer ${access_token}`;
      
      // Notify all queued requests
      onTokenRefreshed(access_token);
      
      isRefreshing = false;
      return api(originalRequest);
    } catch (refreshError) {
      // Refresh failed, clear tokens and redirect
      isRefreshing = false;
      refreshSubscribers = [];
      localStorage.removeItem('access_token');
      localStorage.removeItem('refresh_token');
      window.location.href = '/login';
      return Promise.reject(refreshError);
    }
  }
);

export default api;
