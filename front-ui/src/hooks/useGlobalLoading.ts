import { useEffect } from 'react';
import { useIsFetching, useIsMutating } from '@tanstack/react-query';
import { useGlobalLoadingStore } from '@/store/globalLoadingStore';

// Регистрирует ожидание в едином индикаторе загрузки, пока active = true.
export const useReportGlobalLoading = (active: boolean) => {
  const begin = useGlobalLoadingStore((state) => state.begin);
  const end = useGlobalLoadingStore((state) => state.end);

  useEffect(() => {
    if (!active) {
      return;
    }
    begin();
    return () => end();
  }, [active, begin, end]);
};

// Количество активных ожиданий приложения: запросы, мутации и ручные ожидания.
// Запросы с meta.globalLoading === false (фоновый опрос) не учитываются.
export const useGlobalLoadingCount = () => {
  const fetching = useIsFetching({
    predicate: (query) => query.meta?.globalLoading !== false,
  });
  const mutating = useIsMutating();
  const manual = useGlobalLoadingStore((state) => state.pending);
  return fetching + mutating + manual;
};
