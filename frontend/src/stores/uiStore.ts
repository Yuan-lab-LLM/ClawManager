import { create } from 'zustand';

const SIDEBAR_COLLAPSED_KEY = 'sidebar_collapsed';

function readInitialSidebarCollapsed(): boolean {
  try {
    return localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === 'true';
  } catch {
    return false;
  }
}

interface UiState {
  sidebarCollapsed: boolean;
  toggleSidebarCollapsed: () => void;
}

export const useUiStore = create<UiState>((set, get) => ({
  sidebarCollapsed: readInitialSidebarCollapsed(),
  toggleSidebarCollapsed: () => {
    const sidebarCollapsed = !get().sidebarCollapsed;
    try {
      localStorage.setItem(SIDEBAR_COLLAPSED_KEY, String(sidebarCollapsed));
    } catch {
      // Persisting is best-effort; keep the in-memory state usable.
    }
    set({ sidebarCollapsed });
  },
}));
