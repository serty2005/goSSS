type ApiErrorShape = {
  message?: string;
  response?: {
    status?: number;
    data?: unknown;
  };
};

const readString = (value: unknown): string => (typeof value === 'string' ? value.trim() : '');

const readServerMessage = (data: unknown): string => {
  if (!data) return '';
  if (typeof data === 'string') {
    return data.trim().startsWith('<') ? '' : data.trim();
  }
  if (typeof data !== 'object') return '';
  const payload = data as Record<string, unknown>;
  const nested = payload.error;
  if (nested && typeof nested === 'object') {
    const nestedPayload = nested as Record<string, unknown>;
    const nestedMessage = readString(nestedPayload.error) || readString(nestedPayload.message) || readString(nestedPayload.msg);
    if (nestedMessage) return nestedMessage;
  }
  const parts = [readString(payload.msg) || readString(payload.message), readString(nested)].filter(Boolean);
  return Array.from(new Set(parts)).join(': ');
};

/**
 * Извлекает текст ошибки из ответа API (конверт `{error: {error}}`, поля `msg`/`message`/`error`)
 * или из сетевой ошибки axios. Возвращает пустую строку, если подробностей нет.
 */
export const extractApiErrorMessage = (error: unknown): string => {
  if (!error) return '';
  if (typeof error === 'string') return error.trim();
  if (typeof error !== 'object') return '';
  const shape = error as ApiErrorShape;
  const serverMessage = readServerMessage(shape.response?.data);
  if (serverMessage) return serverMessage;
  const status = shape.response?.status;
  if (status) return `HTTP ${status}`;
  return readString(shape.message);
};

/**
 * Дополняет пользовательское сообщение подробностями ошибки из ответа сервера.
 * Если сервер уже вернул текст, начинающийся с того же сообщения, дубль не добавляется.
 */
export const withApiError = (fallback: string, error: unknown): string => {
  const detail = extractApiErrorMessage(error);
  if (!detail) return fallback;
  const normalizedFallback = fallback.trim().replace(/[.:]+$/, '');
  if (detail.toLowerCase().startsWith(normalizedFallback.toLowerCase())) {
    return detail;
  }
  return `${normalizedFallback}: ${detail}`;
};
