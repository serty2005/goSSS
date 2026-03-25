import { describe, expect, it } from 'vitest';
import { resolveAppLocaleCode } from '@/i18n/supportedLocales';

describe('resolveAppLocaleCode', () => {
  it('приоритетно берёт поддерживаемую locale из профиля', () => {
    expect(resolveAppLocaleCode('ru', 'en-US', 'en')).toBe('ru');
  });

  it('нормализует browser locale и fallback-значения', () => {
    expect(resolveAppLocaleCode(undefined, 'en-US')).toBe('en');
    expect(resolveAppLocaleCode(undefined, 'ru_RU')).toBe('ru');
  });

  it('возвращает en по умолчанию для неподдерживаемых значений', () => {
    expect(resolveAppLocaleCode('de', 'fr-FR')).toBe('en');
  });
});
