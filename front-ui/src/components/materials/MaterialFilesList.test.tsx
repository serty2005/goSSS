// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';

import type { MaterialFileDTO } from '@/types/api';
import MaterialFilesList from './MaterialFilesList';

const file: MaterialFileDTO = {
  id: 'f1',
  name: 'схема сети.pdf',
  content_type: 'application/pdf',
  size: 1536,
  url: 'https://sd.example.com/material-files/m1/f1/%D1%81%D1%85%D0%B5%D0%BC%D0%B0.pdf',
  created_at: '2026-10-09T09:00:00+03:00',
};

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
  vi.restoreAllMocks();
});

describe('MaterialFilesList', () => {
  it('показывает имя файла прямой ссылкой на скачивание и размер', () => {
    render(<MaterialFilesList files={[file]} />);

    const link = screen.getByRole('link', { name: 'схема сети.pdf' });
    expect(link.getAttribute('href')).toBe(file.url);
    expect(link.getAttribute('download')).toBe('схема сети.pdf');
    expect(screen.getByText('1,5 КБ')).toBeTruthy();
  });

  it('копирует абсолютную прямую ссылку', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
    render(<MaterialFilesList files={[file]} />);

    await userEvent.click(screen.getByRole('button', { name: 'Копировать ссылку на схема сети.pdf' }));

    await waitFor(() => expect(writeText).toHaveBeenCalledWith(file.url));
  });

  it('не показывает удаление без обработчика и вызывает его после подтверждения', async () => {
    const { rerender } = render(<MaterialFilesList files={[file]} />);
    expect(screen.queryByRole('button', { name: 'Удалить схема сети.pdf' })).toBeNull();

    const onDelete = vi.fn();
    rerender(<MaterialFilesList files={[file]} onDelete={onDelete} />);
    await userEvent.click(screen.getByRole('button', { name: 'Удалить схема сети.pdf' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Удалить' }));

    expect(onDelete).toHaveBeenCalledWith(file);
  });
});
