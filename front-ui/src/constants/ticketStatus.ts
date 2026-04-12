import type { TicketStatus } from '@/types/api';
import i18n from '@/i18n/i18n';

export type TicketStatusMeta = {
  label: string;
  color: string;
};

const UNKNOWN_STATUS_META: TicketStatusMeta = {
  label: '',
  color: 'default',
};

export const TICKET_STATUS_META: Record<TicketStatus, TicketStatusMeta> = {
  new: { label: 'Новая', color: 'blue' },
  in_progress: { label: 'В работе', color: 'processing' },
  pending: { label: 'Ожидание', color: 'orange' },
  deferred: { label: 'Отложено', color: 'orange' },
  onsite: { label: 'На выезд', color: 'cyan' },
  to_manager: { label: 'Передать менеджеру', color: 'purple' },
  resolved: { label: 'Решена', color: 'green' },
  spam: { label: 'Спам', color: 'red' },
  execution: { label: 'Реализация', color: 'magenta' },
  closed: { label: 'Закрыта', color: 'default' },
};

export const TICKET_STATUS_OPTIONS: Array<{ value: TicketStatus; label: string; color: string }> = [
  { value: 'new', ...TICKET_STATUS_META.new },
  { value: 'in_progress', ...TICKET_STATUS_META.in_progress },
  { value: 'pending', ...TICKET_STATUS_META.pending },
  { value: 'deferred', ...TICKET_STATUS_META.deferred },
  { value: 'onsite', ...TICKET_STATUS_META.onsite },
  { value: 'to_manager', ...TICKET_STATUS_META.to_manager },
  { value: 'resolved', ...TICKET_STATUS_META.resolved },
  { value: 'spam', ...TICKET_STATUS_META.spam },
  { value: 'execution', ...TICKET_STATUS_META.execution },
  { value: 'closed', ...TICKET_STATUS_META.closed },
];

export const TICKET_ACTIVE_STATUS_VALUES: TicketStatus[] = [
  'new',
  'in_progress',
  'pending',
  'onsite',
];

export const TICKET_CLOSED_LIKE_STATUS_VALUES: TicketStatus[] = [
  'resolved',
  'closed',
  'spam',
  'execution',
];

export const isClosedLikeTicketStatus = (status?: string) =>
  Boolean(status) && TICKET_CLOSED_LIKE_STATUS_VALUES.includes(status as TicketStatus);

export const getTicketStatusMeta = (status?: string): TicketStatusMeta => {
  if (!status) {
    return {
      ...UNKNOWN_STATUS_META,
      label: i18n.t('tickets:fallback.unknown'),
    };
  }
  const meta = TICKET_STATUS_META[status as TicketStatus];
  if (!meta) {
    return { label: status, color: UNKNOWN_STATUS_META.color };
  }
  return {
    ...meta,
    label: i18n.t(`layout:headerSearch.ticket.statusOptions.${status}`, {
      defaultValue: meta.label,
    }),
  };
};
