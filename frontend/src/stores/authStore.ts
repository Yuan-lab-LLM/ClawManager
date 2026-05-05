import { create } from 'zustand';
import type { User } from '../types/auth';
import { authService } from '../services/authService';
import { DEFAULT_LOCALE, FALLBACK_LOCALE, isSupportedLocale, translate } from '../lib/i18n';

const LOCALE_STORAGE_KEY = 'clawmanager_locale';

const authFallbackMessage = (key: string) => {
  const storedLocale = localStorage.getItem(LOCALE_STORAGE_KEY);
  const locale = isSupportedLocale(storedLocale) ? storedLocale : DEFAULT_LOCALE;
  return translate(locale, key) ?? translate(FALLBACK_LOCALE, key) ?? key;
};

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
      } catch (err: any) {
        set({ 
          error: err.response?.data?.error || authFallbackMessage('auth.loginFailed'),
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
      } catch (err: any) {
        set({ 
          error: err.response?.data?.error || authFallbackMessage('auth.registrationFailed'),
          isLoading: false 
        });
        throw err;
      }
    },

    logout: async () => {
      set({ isLoading: true });
      try {
        await authService.logout();
      } finally {
        set({ 
          user: null, 
          isAuthenticated: false, 
          isLoading: false,
          error: null 
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
      } catch (err) {
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
