import { useReportGlobalLoading } from '@/hooks/useGlobalLoading';

interface LoadingPlaceholderProps {
  // Регистрировать ожидание в едином индикаторе header.
  // Нужно только для ожиданий, которые не отслеживаются TanStack Query
  // (запросы и мутации индикатор учитывает сам).
  report?: boolean;
}

// Заглушка на месте содержимого, которое ещё загружается.
// Ничего не рисует на странице: ожидание показывает единый индикатор в header.
const LoadingPlaceholder = ({ report = false }: LoadingPlaceholderProps) => {
  useReportGlobalLoading(report);
  return null;
};

export default LoadingPlaceholder;
