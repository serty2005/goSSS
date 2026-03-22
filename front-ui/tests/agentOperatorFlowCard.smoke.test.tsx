// @vitest-environment jsdom

import React from 'react';
import { beforeAll, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
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

describe('AgentOperatorFlowCard smoke', () => {
  it('показывает список адаптеров и сохраняет selected_adapter_ids', async () => {
    const user = userEvent.setup();
    const onSaveSelection = vi.fn();
    const onRunAdapter = vi.fn();

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

    render(
      <AgentOperatorFlowCard
        operatorFlow={operatorFlow}
        inventoryCOMPorts={[]}
        saveSelectionPending={false}
        runAdapterPending={false}
        onSaveSelection={onSaveSelection}
        onRunAdapter={onRunAdapter}
      />,
    );

    expect(screen.getByText('Фискальный адаптер АТОЛ')).toBeTruthy();
    expect(screen.getByText('stable 1.2.3')).toBeTruthy();
    expect(screen.getByText('latest 1.3.0')).toBeTruthy();

    await user.click(screen.getByRole('checkbox', { name: 'Фискальный адаптер АТОЛ' }));
    await user.click(screen.getByRole('button', { name: 'Сохранить' }));

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
      <AgentOperatorFlowCard
        operatorFlow={operatorFlow}
        inventoryCOMPorts={[]}
        saveSelectionPending={false}
        runAdapterPending={false}
        onSaveSelection={onSaveSelection}
        onRunAdapter={onRunAdapter}
      />,
    );

    await user.click(screen.getByRole('button', { name: 'Запустить сейчас' }));

    expect(onRunAdapter).toHaveBeenCalledTimes(1);
    expect(onRunAdapter).toHaveBeenCalledWith({
      adapter_id: 'fiscal-atol',
    });
  });
});
