// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';

import { ticketsApi } from '@/api/tickets';
import { BITRIX_SERVICE_POINT_REQUIRED, getApiErrorCode } from '@/utils/apiError';
import BitrixServicePointModal from './BitrixServicePointModal';

vi.mock('@/api/tickets', () => ({
  ticketsApi: { getBitrixServicePoints: vi.fn() },
}));

const getPoints = vi.mocked(ticketsApi.getBitrixServicePoints);

const renderModal = (props: Partial<React.ComponentProps<typeof BitrixServicePointModal>> = {}) => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const onSubmit = vi.fn();
  const onCancel = vi.fn();
  render(
    <QueryClientProvider client={client}>
      <BitrixServicePointModal open onSubmit={onSubmit} onCancel={onCancel} {...props} />
    </QueryClientProvider>,
  );
  return { onSubmit, onCancel };
};

describe('BitrixServicePointModal', () => {
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

  it('не даёт продолжить без выбранной точки и передаёт выбранную точку', async () => {
    getPoints.mockResolvedValue([
      { b24_element_id: 16961, name: 'Балахнинская изба' },
      { b24_element_id: 20775, name: 'Другая точка' },
    ] as Awaited<ReturnType<typeof ticketsApi.getBitrixServicePoints>>);
    const { onSubmit } = renderModal();

    const confirm = await screen.findByRole('button', { name: 'Выбрать и продолжить' });
    expect((confirm as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText(/нет сопоставления с точкой обслуживания/)).toBeTruthy();

    await userEvent.click(screen.getByRole('combobox'));
    await userEvent.click(await screen.findByText('Балахнинская изба'));
    await waitFor(() => expect((confirm as HTMLButtonElement).disabled).toBe(false));
    await userEvent.click(confirm);

    expect(onSubmit).toHaveBeenCalledWith(16961);
    expect(getPoints).toHaveBeenCalledWith({ term: '', limit: 50 });
  });

  it('не запрашивает точки, пока модал закрыт', () => {
    renderModal({ open: false });
    expect(getPoints).not.toHaveBeenCalled();
  });
});

describe('getApiErrorCode', () => {
  it('читает машинный код из конверта ошибки API', () => {
    const error = { response: { status: 409, data: { status: 'error', error: { error: 'нужна точка', code: BITRIX_SERVICE_POINT_REQUIRED } } } };
    expect(getApiErrorCode(error)).toBe(BITRIX_SERVICE_POINT_REQUIRED);
    expect(getApiErrorCode({ response: { data: { error: { error: 'без кода' } } } })).toBe('');
    expect(getApiErrorCode(null)).toBe('');
  });
});
