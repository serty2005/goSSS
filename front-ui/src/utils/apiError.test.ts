import { describe, expect, it } from 'vitest';
import { extractApiErrorMessage, withApiError } from './apiError';

describe('apiError', () => {
  it('берет текст из конверта ошибки backend', () => {
    const error = { message: 'Request failed', response: { status: 400, data: { status: 'error', error: { error: 'компания уже существует' } } } };
    expect(extractApiErrorMessage(error)).toBe('компания уже существует');
    expect(withApiError('Не удалось подтвердить кандидата', error)).toBe('Не удалось подтвердить кандидата: компания уже существует');
  });

  it('склеивает msg и error из плоского ответа', () => {
    const error = { response: { status: 500, data: { msg: 'не удалось подтвердить кандидата', error: 'duplicate key' } } };
    expect(extractApiErrorMessage(error)).toBe('не удалось подтвердить кандидата: duplicate key');
    expect(withApiError('Не удалось подтвердить кандидата', error)).toBe('не удалось подтвердить кандидата: duplicate key');
  });

  it('возвращает статус или сообщение сети, если подробностей нет', () => {
    expect(withApiError('Ошибка', { response: { status: 502, data: '<html>' } })).toBe('Ошибка: HTTP 502');
    expect(withApiError('Ошибка', { message: 'Network Error' })).toBe('Ошибка: Network Error');
    expect(withApiError('Ошибка', null)).toBe('Ошибка');
  });
});
