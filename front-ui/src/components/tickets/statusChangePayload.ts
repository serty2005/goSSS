import type { TicketStatus } from '@/types/api';
import type { ManagerTransferPayload } from './ManagerTransferModal';

// Запрос смены статуса тикета из UI: общий для страниц, где есть передача менеджеру.
export type StatusChangePayload = {
  id: string;
  status: TicketStatus;
  comment?: string;
  deferredUntil?: string;
  // Точка обслуживания Bitrix24, выбранная оператором, когда у компании нет сопоставления с точкой.
  bitrixServicePointId?: number;
} & Partial<ManagerTransferPayload>;
