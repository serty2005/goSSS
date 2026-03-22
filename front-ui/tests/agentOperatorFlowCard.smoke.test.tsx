// @vitest-environment jsdom

import React from 'react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import AgentOperatorFlowCard from '@/components/agents/AgentOperatorFlowCard';
import { AgentOperatorFlowDTO } from '@/types/api';

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
});

afterEach(() => {
  vi.restoreAllMocks();
  cleanup();
});

describe('AgentOperatorFlowCard smoke', () => {
  it('сохраняет локальный выбор без saved_adapter_runtime_profiles и повторно гидратируется после ответа сервера', async () => {
    const user = userEvent.setup();
    const onSaveSelection = vi.fn();
    const onRunAdapter = vi.fn();
    const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => undefined);

    const operatorFlow: AgentOperatorFlowDTO = {
      available_adapters: [
        {
          adapter_id: 'fiscal-atol',
          title: 'Фискальный адаптер АТОЛ',
          description: 'Stable-релиз для продовой выдачи',
          published: true,
          selectable: true,
          status_text: 'Готов к выдаче',
          stable_version: '1.2.3',
          latest_version: '1.3.0',
          adapter_type: 'fiscal-atol',
          target_os: 'windows',
          target_arch: 'amd64',
        },
      ],
      selected_adapter_ids: [],
      effective_adapter_manifests: [],
    };

    const { rerender } = render(
      <React.StrictMode>
        <AgentOperatorFlowCard
          operatorFlow={operatorFlow}
          inventoryCOMPorts={[]}
          saveSelectionPending={false}
          runAdapterPending={false}
          onSaveSelection={onSaveSelection}
          onRunAdapter={onRunAdapter}
        />
      </React.StrictMode>,
    );

    expect(screen.getByText('Фискальный адаптер АТОЛ')).toBeTruthy();
    expect(screen.getByText('stable 1.2.3')).toBeTruthy();
    expect(screen.getByText('latest 1.3.0')).toBeTruthy();

    const checkbox = screen.getByRole('checkbox', { name: 'Фискальный адаптер АТОЛ' }) as HTMLInputElement;
    await user.click(checkbox);

    expect(checkbox.checked).toBe(true);
    expect(screen.queryByText('Сначала выберите хотя бы один адаптер')).toBeNull();
    expect(screen.queryByText('Команда')).toBeNull();
    expect(screen.queryByText('Operation / task_type')).toBeNull();
    expect(screen.queryByText('Timeout, сек')).toBeNull();
    expect(screen.queryByText('Метка')).toBeNull();
    expect(screen.queryByText('Модель')).toBeNull();
    expect(screen.queryByText('Baudrate')).toBeNull();
    expect(screen.queryByText('driver_hints JSON')).toBeNull();
    expect(screen.queryByText('extra_params JSON')).toBeNull();

    const saveButton = screen.getByRole('button', { name: 'Сохранить конфигурацию адаптеров' }) as HTMLButtonElement;
    const runButton = screen.getByRole('button', { name: 'Запустить сейчас' }) as HTMLButtonElement;

    expect(saveButton.disabled).toBe(false);
    expect(runButton.disabled).toBe(true);

    rerender(
      <React.StrictMode>
        <AgentOperatorFlowCard
          operatorFlow={{ ...operatorFlow }}
          inventoryCOMPorts={[]}
          saveSelectionPending={false}
          runAdapterPending={false}
          onSaveSelection={onSaveSelection}
          onRunAdapter={onRunAdapter}
        />
      </React.StrictMode>,
    );

    expect((screen.getByRole('checkbox', { name: 'Фискальный адаптер АТОЛ' }) as HTMLInputElement).checked).toBe(true);
    expect((screen.getByRole('button', { name: 'Сохранить конфигурацию адаптеров' }) as HTMLButtonElement).disabled).toBe(false);

    await user.click(screen.getByRole('button', { name: 'Сохранить конфигурацию адаптеров' }));

    expect(onSaveSelection).toHaveBeenCalledTimes(1);
    expect(onSaveSelection).toHaveBeenCalledWith({
      selected_adapter_ids: ['fiscal-atol'],
      runtime_profiles: [
        {
          adapter_id: 'fiscal-atol',
          command: 'run',
          operation: 'collect',
          timeout_seconds: 45,
          devices: [],
          schedule: {
            enabled: false,
            interval_seconds: undefined,
          },
        },
      ],
    });
    expect(onRunAdapter).not.toHaveBeenCalled();

    rerender(
      <React.StrictMode>
        <AgentOperatorFlowCard
          operatorFlow={{
            ...operatorFlow,
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
          }}
          inventoryCOMPorts={[]}
          saveSelectionPending={false}
          runAdapterPending={false}
          onSaveSelection={onSaveSelection}
          onRunAdapter={onRunAdapter}
        />
      </React.StrictMode>,
    );

    expect((screen.getByRole('button', { name: 'Сохранить конфигурацию адаптеров' }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('button', { name: 'Запустить сейчас' }) as HTMLButtonElement).disabled).toBe(false);

    const consoleErrors = consoleErrorSpy.mock.calls
      .flatMap((call) => call.map((value) => (value instanceof Error ? value.message : String(value))))
      .join('\n');

    expect(consoleErrors).not.toContain('Maximum update depth exceeded');
  }, 15000);

  it('сохраняет упрощенный payload подключения с полем address', async () => {
    const user = userEvent.setup();
    const onSaveSelection = vi.fn();
    const onRunAdapter = vi.fn();

    render(
      <React.StrictMode>
        <AgentOperatorFlowCard
          operatorFlow={{
            available_adapters: [
              {
                adapter_id: 'fiscal-atol',
                title: 'Фискальный адаптер АТОЛ',
                published: true,
                selectable: true,
                status_text: 'Готов к выдаче',
              },
            ],
            selected_adapter_ids: [],
            effective_adapter_manifests: [],
          }}
          inventoryCOMPorts={[]}
          saveSelectionPending={false}
          runAdapterPending={false}
          onSaveSelection={onSaveSelection}
          onRunAdapter={onRunAdapter}
        />
      </React.StrictMode>,
    );

    await user.click(screen.getByRole('checkbox', { name: 'Фискальный адаптер АТОЛ' }));
    await user.type(screen.getByPlaceholderText('Например, 10.25.1.22:5555'), '10.25.1.22:5555');
    await user.click(screen.getByRole('button', { name: 'Сохранить конфигурацию адаптеров' }));

    expect(onSaveSelection).toHaveBeenCalledTimes(1);
    expect(onSaveSelection).toHaveBeenCalledWith({
      selected_adapter_ids: ['fiscal-atol'],
      runtime_profiles: [
        {
          adapter_id: 'fiscal-atol',
          command: 'run',
          operation: 'collect',
          timeout_seconds: 45,
          devices: [
            {
              connection_type: 'tcp',
              address: '10.25.1.22:5555',
            },
          ],
          schedule: {
            enabled: false,
            interval_seconds: undefined,
          },
        },
      ],
    });
    expect(onRunAdapter).not.toHaveBeenCalled();
  });

  it('дает запустить уже сохраненный профиль адаптера', async () => {
    const user = userEvent.setup();
    const onSaveSelection = vi.fn();
    const onRunAdapter = vi.fn();

    const operatorFlow: AgentOperatorFlowDTO = {
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
          devices: [
            {
              connection_type: 'tcp',
              ip: '10.25.1.22',
              port: 5555,
            },
          ],
          schedule: {
            enabled: true,
            interval_seconds: 300,
          },
        },
      ],
      effective_adapter_manifests: [],
    };

    render(
      <React.StrictMode>
        <AgentOperatorFlowCard
          operatorFlow={operatorFlow}
          inventoryCOMPorts={[]}
          saveSelectionPending={false}
          runAdapterPending={false}
          onSaveSelection={onSaveSelection}
          onRunAdapter={onRunAdapter}
        />
      </React.StrictMode>,
    );

    const runButton = screen.getAllByRole('button', { name: 'Запустить сейчас' })
      .find((button) => !(button as HTMLButtonElement).disabled);

    expect(runButton).toBeTruthy();
    await user.click(runButton as HTMLButtonElement);

    expect(onRunAdapter).toHaveBeenCalledTimes(1);
    expect(onRunAdapter).toHaveBeenCalledWith({
      adapter_id: 'fiscal-atol',
    });
  });
});
