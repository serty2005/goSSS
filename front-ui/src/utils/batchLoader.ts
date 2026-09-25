// Пакетный загрузчик: собирает ключи, запрошенные в течение короткого окна,
// и загружает их одним запросом вместо запроса на каждый ключ.
interface PendingItem<V> {
  resolve: (value: V | undefined) => void;
  reject: (reason: unknown) => void;
}

interface BatchLoaderOptions {
  // Максимум ключей в одном запросе.
  maxBatchSize: number;
  // Окно сбора ключей перед отправкой запроса.
  delayMs: number;
}

export const createBatchLoader = <V>(
  fetchBatch: (keys: string[]) => Promise<Map<string, V>>,
  { maxBatchSize, delayMs }: BatchLoaderOptions,
) => {
  let queue = new Map<string, PendingItem<V>[]>();
  let timer: ReturnType<typeof setTimeout> | null = null;

  const runBatch = async (batch: Map<string, PendingItem<V>[]>) => {
    try {
      const result = await fetchBatch(Array.from(batch.keys()));
      batch.forEach((items, key) => items.forEach((item) => item.resolve(result.get(key))));
    } catch (error) {
      batch.forEach((items) => items.forEach((item) => item.reject(error)));
    }
  };

  const flush = () => {
    timer = null;
    const entries = Array.from(queue.entries());
    queue = new Map();
    for (let offset = 0; offset < entries.length; offset += maxBatchSize) {
      void runBatch(new Map(entries.slice(offset, offset + maxBatchSize)));
    }
  };

  return {
    load: (key: string) => new Promise<V | undefined>((resolve, reject) => {
      const items = queue.get(key) || [];
      items.push({ resolve, reject });
      queue.set(key, items);
      if (timer === null) {
        timer = setTimeout(flush, delayMs);
      }
    }),
  };
};
