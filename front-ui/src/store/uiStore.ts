import { create } from 'zustand';
import { persist } from 'zustand/middleware';

type ThemeMode = 'light' | 'dark';

interface UiState {
  themeMode: ThemeMode;
  sidebarCollapsed: boolean;
  toggleTheme: () => void;
  setTheme: (mode: ThemeMode) => void;
  setSidebarCollapsed: (collapsed: boolean) => void;
}

export const useUiStore = create<UiState>()(
  persist(
    (set) => ({
      themeMode: 'light',
      sidebarCollapsed: false,
      toggleTheme: () =>
        set((state) => ({ themeMode: state.themeMode === 'light' ? 'dark' : 'light' })),
      setTheme: (mode) => set({ themeMode: mode }),
      setSidebarCollapsed: (collapsed) => set({ sidebarCollapsed: collapsed }),
    }),
    {
      name: 'etalon-ui-storage',
    }
  )
);
