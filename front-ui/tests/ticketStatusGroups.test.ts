import { describe, expect, it } from 'vitest';
import type { TicketListItemDTO } from '@/types/api';
import { buildStatusGroupRows, isStatusGroupRow, mergeStatusCounts, type StatusGroupOptions } from '@/components/tickets/ticketStatusGroups';

const ticket = (id: string, status: string) => ({ id, status }) as unknown as TicketListItemDTO;

const layout = (rows: TicketListItemDTO[], options: Partial<StatusGroupOptions> = {}) =>
  buildStatusGroupRows(rows, {
    collapsed: new Set(),
    order: 'asc',
    hasNextPage: false,
    ...options,
  }).map((row) => (isStatusGroupRow(row) ? `#${row.statusGroup?.status}${row.statusGroup?.collapsed ? '(свернута)' : ''}` : row.id));

describe('ticketStatusGroups', () => {
  it('сводит устаревшие статусы к актуальным', () => {
    expect(mergeStatusCounts({ new: 2, registered: 1, inprogress: 3, closed: 4, waitClientAnswer: 1 })).toEqual({
      new: 3,
      in_progress: 3,
      closed: 4,
      waitClientAnswer: 1,
    });
  });

  it('вставляет заголовок перед каждой группой и объединяет устаревшие статусы', () => {
    const rows = [ticket('1', 'new'), ticket('2', 'registered'), ticket('3', 'in_progress'), ticket('4', 'closed')];
    const result = buildStatusGroupRows(rows, { counts: { new: 10, in_progress: 1, closed: 7 }, collapsed: new Set(), order: 'asc', hasNextPage: false });

    expect(result.map((row) => (isStatusGroupRow(row) ? `#${row.statusGroup?.status}` : row.id))).toEqual([
      '#new', '1', '2', '#in_progress', '3', '#closed', '4',
    ]);
    expect(result[0].statusGroup).toMatchObject({ count: 10, loaded: 2, collapsed: false });
  });

  it('не занижает счетчик группы относительно загруженных заявок', () => {
    const result = buildStatusGroupRows([ticket('1', 'new'), ticket('2', 'new'), ticket('3', 'new')], {
      counts: { new: 1 }, collapsed: new Set(), order: 'asc', hasNextPage: false,
    });
    expect(result[0].statusGroup?.count).toBe(3);
  });

  it('показывает свернутую группу по счетчику без ее заявок', () => {
    // Сервер исключил закрытые: их заявок нет в rows, но заголовок со счетчиком остается на своем месте.
    const rows = [ticket('1', 'new'), ticket('2', 'in_progress')];
    const result = buildStatusGroupRows(rows, {
      counts: { new: 1, in_progress: 1, closed: 65000 },
      collapsed: new Set(['closed']),
      order: 'asc',
      hasNextPage: false,
    });

    expect(result.map((row) => (isStatusGroupRow(row) ? `#${row.statusGroup?.status}` : row.id))).toEqual(['#new', '1', '#in_progress', '2', '#closed']);
    expect(result.at(-1)?.statusGroup).toMatchObject({ collapsed: true, count: 65000, loaded: 0 });
  });

  it('скрывает заявки свернутой группы, даже если они еще не убраны из загруженных', () => {
    expect(layout([ticket('1', 'new'), ticket('2', 'closed')], { counts: { new: 1, closed: 1 }, collapsed: new Set(['closed']) }))
      .toEqual(['#new', '1', '#closed(свернута)']);
  });

  it('не показывает группы после неполной, пока есть следующие страницы', () => {
    const rows = [ticket('1', 'new'), ticket('2', 'new')];
    const options = { counts: { new: 50, closed: 9 }, collapsed: new Set(['closed']), hasNextPage: true };

    expect(layout(rows, options)).toEqual(['#new', '1', '2']);
    expect(layout(rows, { ...options, counts: { new: 2, closed: 9 } })).toEqual(['#new', '1', '2', '#closed(свернута)']);
  });

  it('учитывает обратное направление и фильтр статусов', () => {
    const rows = [ticket('1', 'closed'), ticket('2', 'new')];
    expect(layout(rows, { order: 'desc', counts: { closed: 1, new: 1 } })).toEqual(['#closed', '1', '#new', '2']);
    expect(layout(rows, { allowedStatuses: new Set(['new']), counts: { closed: 1, new: 1 } })).toEqual(['#new', '2']);
  });
});
