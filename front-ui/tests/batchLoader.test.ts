import { describe, expect, it, vi } from 'vitest';
import { createBatchLoader } from '@/utils/batchLoader';

describe('createBatchLoader', () => {
  it('объединяет ключи в пакеты с учётом лимита и раздаёт результаты по ключам', async () => {
    const fetchBatch = vi.fn(async (keys: string[]) => new Map(keys.map((key) => [key, `значение-${key}`])));
    const loader = createBatchLoader(fetchBatch, { maxBatchSize: 2, delayMs: 5 });

    const results = await Promise.all([loader.load('a'), loader.load('b'), loader.load('a'), loader.load('c')]);

    expect(results).toEqual(['значение-a', 'значение-b', 'значение-a', 'значение-c']);
    expect(fetchBatch).toHaveBeenCalledTimes(2);
    expect(fetchBatch.mock.calls.map(([keys]) => keys)).toEqual([['a', 'b'], ['c']]);
  });

  it('возвращает undefined для отсутствующих ключей и отклоняет пакет при ошибке', async () => {
    const loader = createBatchLoader(async () => new Map<string, string>(), { maxBatchSize: 10, delayMs: 1 });
    await expect(loader.load('нет')).resolves.toBeUndefined();

    const failing = createBatchLoader<string>(async () => { throw new Error('сбой'); }, { maxBatchSize: 10, delayMs: 1 });
    await expect(failing.load('x')).rejects.toThrow('сбой');
  });
});
