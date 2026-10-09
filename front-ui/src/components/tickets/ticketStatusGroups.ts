import type { TicketListItemDTO } from '@/types/api';
import { canonicalTicketStatus, TICKET_STATUS_OPTIONS } from '@/constants/ticketStatus';

export type TicketStatusGroupInfo = {
  status: string;
  /** Число заявок группы на сервере с учетом текущих фильтров. */
  count: number;
  loaded: number;
  collapsed: boolean;
};

export type TicketTableRow = TicketListItemDTO & {
  /** Заполнено только у строк-заголовков групп статусов. */
  statusGroup?: TicketStatusGroupInfo;
};

export const isStatusGroupRow = (row: TicketTableRow) => Boolean(row.statusGroup);

// mergeStatusCounts сводит счетчики устаревших статусов backend к актуальным статусам.
export const mergeStatusCounts = (counts?: Record<string, number>): Record<string, number> => {
  const merged: Record<string, number> = {};
  Object.entries(counts || {}).forEach(([status, count]) => {
    const key = canonicalTicketStatus(status);
    merged[key] = (merged[key] || 0) + count;
  });
  return merged;
};

export type StatusGroupOptions = {
  /** Счетчики по сырым статусам backend. */
  counts?: Record<string, number>;
  collapsed: ReadonlySet<string>;
  /** Направление сортировки по статусу. */
  order: 'asc' | 'desc';
  /** Статусы, выбранные в фильтре; пусто - без ограничения. */
  allowedStatuses?: ReadonlySet<string>;
  /** Есть ли еще страницы: пока они есть, группы после неполной группы не показываются. */
  hasNextPage: boolean;
};

const KNOWN_STATUS_ORDER = TICKET_STATUS_OPTIONS.map((item) => item.value as string);

// buildStatusGroupRows вставляет строку-заголовок перед каждой группой статуса.
// Заявки свернутых групп сервер не отдает (exclude_status), поэтому их заголовки строятся по счетчикам.
// Порядок групп совпадает с серверной сортировкой по статусу.
export const buildStatusGroupRows = (
  rows: TicketListItemDTO[],
  options: StatusGroupOptions,
): TicketTableRow[] => {
  const counts = mergeStatusCounts(options.counts);
  const rowsByStatus = new Map<string, TicketListItemDTO[]>();
  rows.forEach((row) => {
    const key = canonicalTicketStatus(row.status);
    const group = rowsByStatus.get(key);
    if (group) {
      group.push(row);
    } else {
      rowsByStatus.set(key, [row]);
    }
  });

  const unknownStatuses = Array.from(new Set([...Object.keys(counts), ...rowsByStatus.keys()]))
    .filter((status) => !KNOWN_STATUS_ORDER.includes(status))
    .sort();
  const ordered = [...KNOWN_STATUS_ORDER, ...unknownStatuses];
  if (options.order === 'desc') {
    ordered.reverse();
  }

  const allowed = options.allowedStatuses;
  const result: TicketTableRow[] = [];
  for (const status of ordered) {
    if (allowed && allowed.size > 0 && !allowed.has(status)) {
      continue;
    }
    const groupRows = rowsByStatus.get(status) || [];
    const loaded = groupRows.length;
    const collapsed = options.collapsed.has(status);
    const count = Math.max(counts[status] ?? loaded, collapsed ? 0 : loaded);
    if (count === 0) {
      continue;
    }

    if (!collapsed && loaded === 0) {
      // Группа еще не достигнута подгрузкой: следующие группы тоже пока скрыты.
      if (options.hasNextPage) {
        break;
      }
      continue;
    }

    result.push({
      id: `status-group:${status}`,
      status,
      statusGroup: { status, count, loaded, collapsed },
    } as unknown as TicketTableRow);
    if (collapsed) {
      continue;
    }
    result.push(...groupRows);
    if (options.hasNextPage && loaded < count) {
      break;
    }
  }
  return result;
};
