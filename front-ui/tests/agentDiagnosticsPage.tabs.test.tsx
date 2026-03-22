// @vitest-environment jsdom

import React from 'react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import AgentDiagnosticsPage from '@/pages/AgentDiagnosticsPage';
import { agentDiagnosticsApi } from '@/api/agentDiagnostics';
import { AgentDiagnosticsDetailsDTO } from '@/types/api';

vi.mock('@/api/agentDiagnostics', () => ({
  agentDiagnosticsApi: {
    getByUUID: vi.fn(),
    approveRegistration: vi.fn(),
    saveAdapterSelection: vi.fn(),
    runAdapter: vi.fn(),
  },
}));

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
  Object.defineProperty(window, 'scrollTo', { value: vi.fn(), writable: true });
  Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
    writable: true,
    value: vi.fn(),
  });
});

afterEach(() => {
  vi.clearAllMocks();
  cleanup();
});

describe('AgentDiagnosticsPage tabs', () => {
  it('рендерит страницу вкладками и открывает результат запуска адаптера', async () => {
    const user = userEvent.setup();
    const getByUUIDMock = vi.mocked(agentDiagnosticsApi.getByUUID);

    const details: AgentDiagnosticsDetailsDTO = {
      agent: {
        uuid: 'agent-1',
        hostname: 'cash-1',
        type: 'sssruner',
        status: 'active',
        last_registration_status: 'success',
        last_registration_error: '',
        last_registration_at: '2026-03-22T09:50:00Z',
        last_heartbeat: '2026-03-22T10:00:00Z',
        last_observed_at: '2026-03-22T10:00:05Z',
        machine_fingerprint: 'fp-1',
        registration_approved_by: '',
        has_latest_inventory: true,
        has_adapter_statuses: true,
      },
      registration_payload: {
        agent_uuid: 'agent-1',
      },
      registration_system_info: {
        os: 'windows',
      },
      latest_inventory: {
        hostname: 'cash-1',
        os: 'windows',
        arch: 'amd64',
        host_info: {
          cash_server_url: 'http://cash-1:8080',
        },
        network_interfaces: [],
        com_ports: [],
        installed_software: [],
        known_components: [],
      },
      latest_adapter_statuses: [
        {
          adapter_id: 'fiscal-atol',
          status: 'ready',
          run_status: 'completed',
          last_run_at: '2026-03-22T09:59:00Z',
        },
      ],
      recent_registrations: [
        {
          id: 1,
          status: 'success',
          machine_fingerprint: 'fp-1',
          created_at: '2026-03-22T09:50:00Z',
          payload: {
            hostname: 'cash-1',
          },
        },
      ],
      recent_adapter_runs: [
        {
          id: 101,
          agent_uuid: 'agent-1',
          adapter_id: 'fiscal-atol',
          type: 'run_adapter',
          status: 'failed',
          command: 'run',
          operation: 'collect',
          created_at: '2026-03-22T10:01:00Z',
          sent_at: '2026-03-22T10:01:03Z',
          completed_at: '2026-03-22T10:01:05Z',
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
          },
          result_payload: {
            status: 'failed',
            stderr: 'порт занят',
          },
        },
      ],
      operator_flow: {
        available_adapters: [
          {
            adapter_id: 'fiscal-atol',
            title: 'Фискальный адаптер АТОЛ',
            published: true,
            selectable: true,
            status_text: 'Готов к выдаче',
          },
        ],
        selected_adapter_ids: ['fiscal-atol'],
        saved_adapter_runtime_profiles: [
          {
            adapter_id: 'fiscal-atol',
            command: 'run',
            operation: 'collect',
            timeout_seconds: 45,
            devices: [],
            schedule: {
              enabled: false,
            },
          },
        ],
        effective_adapter_manifests: [
          {
            adapter_id: 'fiscal-atol',
            version: '1.0.0',
            target_os: 'windows',
            target_arch: 'amd64',
          },
        ],
      },
    };

    getByUUIDMock.mockResolvedValue({ data: details });

    const queryClient = new QueryClient({
      defaultOptions: {
        queries: {
          retry: false,
        },
      },
    });

    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/agent-diagnostics/agent-1']}>
          <Routes>
            <Route path="/agent-diagnostics/:uuid" element={<AgentDiagnosticsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );

    await screen.findByText('Сводка по агенту');

    expect(screen.getByRole('tab', { name: 'Обзор' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Heartbeat / Inventory' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Адаптеры' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Запуски адаптеров' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Регистрация' })).toBeTruthy();

    await user.click(screen.getByRole('tab', { name: 'Heartbeat / Inventory' }));
    await screen.findByText('Inventory snapshot');

    await user.click(screen.getByRole('tab', { name: 'Адаптеры' }));
    await screen.findByText('Управление адаптерами');

    await user.click(screen.getByRole('tab', { name: 'Запуски адаптеров' }));
    await screen.findByText('История запусков адаптеров');
    await user.click(screen.getByRole('button', { name: 'Открыть результат' }));
    await screen.findByText('Запуск адаптера #101');

    expect(screen.getByText('Payload команды')).toBeTruthy();
    expect(screen.getByText('Result payload')).toBeTruthy();
    expect(screen.getByText('порт занят')).toBeTruthy();

    await user.click(screen.getByRole('tab', { name: 'Регистрация' }));
    await waitFor(() => {
      expect(screen.getByText('История попыток регистрации')).toBeTruthy();
    });
  }, 20000);
});
