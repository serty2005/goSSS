import { create } from 'zustand';

// Счётчик ожиданий, которые не проходят через TanStack Query
// (загрузка чанков страниц, загрузка файлов и т.п.).
// Вместе с активными запросами и мутациями он управляет единым индикатором в header.
interface GlobalLoadingState {
  pending: number;
  begin: () => void;
  end: () => void;
}

export const useGlobalLoadingStore = create<GlobalLoadingState>((set) => ({
  pending: 0,
  begin: () => set((state) => ({ pending: state.pending + 1 })),
  end: () => set((state) => ({ pending: Math.max(0, state.pending - 1) })),
}));
