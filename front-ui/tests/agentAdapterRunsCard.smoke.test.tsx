// @vitest-environment jsdom

import React from 'react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import AgentAdapterRunsCard from '@/components/agents/AgentAdapterRunsCard';
import { AgentAdapterRunDTO } from '@/types/api';

beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }),
  });

  class ResizeObserverMock {
    observe() {}
    unobserve() {}
    disconnect() {}
  }

  vi.stubGlobal('ResizeObserver', ResizeObserverMock);
  Object.defineProperty(window, 'getComputedStyle', {
    writable: true,
    value: () => ({
      getPropertyValue: () => '',
    }),
  });
  Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
    writable: true,
    value: vi.fn(),
  });
});

afterEach(() => {
  vi.restoreAllMocks();
  cleanup();
});

describe('AgentAdapterRunsCard smoke', () => {
  const runs: AgentAdapterRunDTO[] = [
    {
      id: 103,
      agent_uuid: 'agent-1',
      adapter_id: 'fiscal-atol',
      type: 'run_adapter',
      status: 'failed',
      command: 'run',
      operation: 'collect',
      created_at: '2026-03-22T10:03:00Z',
      sent_at: '2026-03-22T10:03:03Z',
      completed_at: '2026-03-22T10:03:05Z',
      duration_ms: 1900,
      exit_code: 2,
      error_text: 'Не удалось открыть порт',
      stdout: '{"status":"error"}',
      stderr: 'порт занят',
      structured_result: {
        status: 'error',
        reason: 'port_busy',
      },
      payload: {
        adapter_id: 'fiscal-atol',
        command: 'run',
        operation: 'collect',
      },
      result_payload: {
        status: 'failed',
        stderr: 'порт занят',
      },
    },
    {
      id: 102,
      agent_uuid: 'agent-1',
      adapter_id: 'iiko-syrve-rms',
      type: 'run_adapter',
      status: 'failed',
      command: 'run',
      operation: 'sync',
      created_at: '2026-03-22T10:02:00Z',
      duration_ms: 1200,
      exit_code: 1,
      error_text: 'Не удалось подключиться к RMS',
      stdout: '{"status":"error"}',
      stderr: 'rms offline',
      structured_result: {
        status: 'error',
      },
      payload: {
        adapter_id: 'iiko-syrve-rms',
      },
      result_payload: {
        status: 'failed',
      },
    },
    {
      id: 101,
      agent_uuid: 'agent-1',
      adapter_id: 'iiko-syrve-rms',
      type: 'run_adapter',
      status: 'sent',
      command: 'run',
      operation: 'sync',
      created_at: '2026-03-22T10:01:00Z',
      sent_at: '2026-03-22T10:01:05Z',
      payload: {
        adapter_id: 'iiko-syrve-rms',
      },
    },
  ];

  it('фильтрует историю запусков и открывает детальный просмотр результата', async () => {
    const user = userEvent.setup();

    render(
      <AgentAdapterRunsCard
        runs={runs}
        knownAdapterIDs={['fiscal-atol', 'fiscal-mitsu', 'iiko-syrve-rms']}
      />,
    );

    expect(screen.getByText('История запусков адаптеров')).toBeTruthy();

    await user.click(screen.getByText('С ошибкой'));

    expect(screen.getByText('Не удалось открыть порт')).toBeTruthy();
    expect(screen.getByText('Не удалось подключиться к RMS')).toBeTruthy();

    const [, adapterFilterGroup] = screen.getAllByRole('radiogroup');
    await user.click(within(adapterFilterGroup).getByText('fiscal-atol'));

    const table = screen.getByRole('table');
    await waitFor(() => {
      expect(within(table).getByText('Не удалось открыть порт')).toBeTruthy();
      expect(within(table).queryByText('Не удалось подключиться к RMS')).toBeNull();
      expect(within(table).getAllByRole('button', { name: 'Открыть результат' })).toHaveLength(1);
    });

    await user.click(screen.getByRole('button', { name: 'Открыть результат' }));

    expect(screen.getByText('Запуск адаптера #103')).toBeTruthy();
    expect(screen.getByText('Payload команды')).toBeTruthy();
    expect(screen.getByText('Result payload')).toBeTruthy();
    expect(screen.getByText('stdout')).toBeTruthy();
    expect(screen.getByText('stderr')).toBeTruthy();
    expect(screen.getByText('порт занят')).toBeTruthy();
    expect(screen.getByText(/port_busy/)).toBeTruthy();
  }, 15000);

  it('показывает корректное пустое состояние для legacy-агента без запусков', () => {
    render(<AgentAdapterRunsCard runs={[]} knownAdapterIDs={[]} />);

    expect(screen.getByText(/legacy submit_json-агента это штатно/i)).toBeTruthy();
  });
});
