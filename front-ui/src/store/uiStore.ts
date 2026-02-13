import { create } from 'zustand';
import { persist } from 'zustand/middleware';

type ThemeMode = 'light' | 'dark';

interface UiState {
  themeMode: ThemeMode;
  toggleTheme: () => void;
  setTheme: (mode: ThemeMode) => void;
}

export const useUiStore = create<UiState>()(
  persist(
    (set) => ({
      themeMode: 'light',
      toggleTheme: () =>
        set((state) => ({ themeMode: state.themeMode === 'light' ? 'dark' : 'light' })),
      setTheme: (mode) => set({ themeMode: mode }),
    }),
    {
      name: 'etalon-ui-storage',
    }
  )
);
