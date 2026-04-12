import { create } from 'zustand';
import type { GlobalTranslationsDTO } from '@/types/api';

type LocalizationState = {
  catalog: GlobalTranslationsDTO | null;
  setCatalog: (catalog: GlobalTranslationsDTO | null) => void;
  resetCatalog: () => void;
};

export const useLocalizationStore = create<LocalizationState>((set) => ({
  catalog: null,
  setCatalog: (catalog) => set({ catalog }),
  resetCatalog: () => set({ catalog: null }),
}));
