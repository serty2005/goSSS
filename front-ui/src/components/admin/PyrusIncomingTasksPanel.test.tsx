// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { App as AntdApp } from 'antd';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';

import { pyrusAdminApi } from '@/api/pyrusAdmin';
import type { PyrusIncomingTaskDTO } from '@/types/api';
import PyrusIncomingTasksPanel from './PyrusIncomingTasksPanel';

vi.mock('@/api/pyrusAdmin', () => ({
  pyrusAdminApi: {
    listIncomingTasks: vi.fn(),
    listTaskEvents: vi.fn(),
    replayTask: vi.fn(),
    replayEvent: vi.fn(),
    replayProblemTasks: vi.fn(),
  },
}));

const api = vi.mocked(pyrusAdminApi);

const problemTask: PyrusIncomingTaskDTO = {
  task_id: 382645865,
  subject: 'Смена индивидуального предпринимателя',
  crm_id: '2298765',
  client_name: 'Олеся',
  events_total: 7,
  status_counts: { waiting: 5, failed: 2 },
  needs_attention: true,
  first_received_at: '2026-10-02T11:05:59Z',
  last_received_at: '2026-10-02T12:42:16Z',
  last_error: 'по CRMID=2298765 не найден сервер с владельцем (owner_id)',
};

const renderPanel = () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <AntdApp>
          <PyrusIncomingTasksPanel />
        </AntdApp>
      </MemoryRouter>
    </QueryClientProvider>,
  );
};

describe('PyrusIncomingTasksPanel', () => {
  beforeAll(() => {
    vi.stubGlobal('ResizeObserver', class {
      observe() {}
      unobserve() {}
      disconnect() {}
    });
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: (query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addListener: () => undefined,
        removeListener: () => undefined,
        addEventListener: () => undefined,
        removeEventListener: () => undefined,
        dispatchEvent: () => false,
      }),
    });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it('показывает задачу без тикета с CRMID и причиной', async () => {
    api.listIncomingTasks.mockResolvedValue({ status: 'success', data: [problemTask], meta: { total: 1, limit: 20, offset: 0, has_next: false, has_prev: false } });

    renderPanel();

    expect(await screen.findByText('CRMID 2298765')).toBeTruthy();
    expect(screen.getByText('Не создан')).toBeTruthy();
    expect(screen.getByText('Ждёт данных: 5')).toBeTruthy();
    expect(screen.getByText('Ошибка: 2')).toBeTruthy();
    expect(api.listIncomingTasks).toHaveBeenCalledWith({ scope: 'problem', limit: 20, offset: 0 });
  });

  it('повторяет задачу целиком одним запросом', async () => {
    api.listIncomingTasks.mockResolvedValue({ status: 'success', data: [problemTask], meta: { total: 1, limit: 20, offset: 0, has_next: false, has_prev: false } });
    api.replayTask.mockResolvedValue({ status: 'success', data: { status: 'accepted', events: 7 } });

    renderPanel();
    await userEvent.click(await screen.findByRole('button', { name: /Повторить задачу/ }));

    await waitFor(() => expect(api.replayTask).toHaveBeenCalledWith(382645865));
    expect(api.replayEvent).not.toHaveBeenCalled();
  });

  it('блокирует повтор у задачи без проблемных событий', async () => {
    api.listIncomingTasks.mockResolvedValue({
      status: 'success',
      data: [{ ...problemTask, needs_attention: false, status_counts: { done: 3 }, last_error: undefined, ticket_id: 'ticket-1' }],
      meta: { total: 1, limit: 20, offset: 0, has_next: false, has_prev: false },
    });

    renderPanel();

    const button = await screen.findByRole('button', { name: /Повторить задачу/ });
    expect((button as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText('Открыть').getAttribute('href')).toBe('/tickets/ticket-1');
  });
});
