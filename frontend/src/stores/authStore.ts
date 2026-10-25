import { create } from 'zustand';
import type { User } from '../types/auth';
import { authService } from '../services/authService';

function authErrorMessage(error: unknown, fallback: string): string {
  const message = (error as { response?: { data?: { error?: unknown } } })
    ?.response?.data?.error;
  return typeof message === 'string' && message ? message : fallback;
}

interface AuthState {
  user: User | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  error: string | null;
  
  // Actions
  setUser: (user: User | null) => void;
  setAuthenticated: (value: boolean) => void;
  setLoading: (value: boolean) => void;
  setError: (error: string | null) => void;
  
  // Async actions
  login: (username: string, password: string) => Promise<void>;
  register: (username: string, email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  fetchCurrentUser: () => Promise<void>;
  clearError: () => void;
}

export const useAuthStore = create<AuthState>((set) => {
  // Check if there's a token on initialization
  const hasToken = !!localStorage.getItem('access_token');
  
  return {
    user: null,
    isAuthenticated: false,
    isLoading: hasToken, // If has token, start in loading state
    error: null,

    setUser: (user) => set({ user }),
    setAuthenticated: (value) => set({ isAuthenticated: value }),
    setLoading: (value) => set({ isLoading: value }),
    setError: (error) => set({ error }),
    clearError: () => set({ error: null }),

    login: async (username, password) => {
      set({ isLoading: true, error: null });
      try {
        await authService.login({ username, password });
        const user = await authService.getCurrentUser();
        set({ user, isAuthenticated: true, isLoading: false });
      } catch (err: unknown) {
        set({ 
          error: authErrorMessage(err, 'Login failed'),
          isLoading: false,
          isAuthenticated: false 
        });
        throw err;
      }
    },

    register: async (username, email, password) => {
      set({ isLoading: true, error: null });
      try {
        await authService.register({ username, email, password });
        // Auto login after registration
        await authService.login({ username, password });
        const user = await authService.getCurrentUser();
        set({ user, isAuthenticated: true, isLoading: false });
      } catch (err: unknown) {
        set({ 
          error: authErrorMessage(err, 'Registration failed'),
          isLoading: false 
        });
        throw err;
      }
    },

    logout: async () => {
      set({ isLoading: true, error: null });
      let logoutFailed = false;
      try {
        await authService.logout();
      } catch (error) {
        logoutFailed = true;
        throw error;
      } finally {
        set({ 
          user: null, 
          isAuthenticated: false, 
          isLoading: false,
          error: logoutFailed ? 'logout_incomplete' : null
        });
      }
    },

    fetchCurrentUser: async () => {
      set({ isLoading: true });
      const token = localStorage.getItem('access_token');
      if (!token) {
        set({ isAuthenticated: false, user: null, isLoading: false });
        return;
      }

      try {
        const user = await authService.getCurrentUser();
        set({ user, isAuthenticated: true, isLoading: false });
      } catch {
        set({ 
          user: null, 
          isAuthenticated: false, 
          isLoading: false 
        });
        localStorage.removeItem('access_token');
        localStorage.removeItem('refresh_token');
      }
    },
  };
});
