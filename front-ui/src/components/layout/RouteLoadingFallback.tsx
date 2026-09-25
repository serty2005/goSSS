import { useReportGlobalLoading } from '@/hooks/useGlobalLoading';

// Fallback для ленивой загрузки страниц: ничего не рисует на странице,
// а показывает ожидание в едином индикаторе header.
const RouteLoadingFallback = () => {
  useReportGlobalLoading(true);
  return null;
};

export default RouteLoadingFallback;
