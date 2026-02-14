import { describe, expect, it } from 'vitest';
import { cleanWebUrl, isIikoWebAddress, normalizeServerAddress } from '@/utils/formatters';

describe('formatters: адреса серверов', () => {
  it('убирает порт 443 в отображении адреса', () => {
    expect(normalizeServerAddress('my-cloud.iikoweb.ru:443', { dropPort443: true })).toBe('my-cloud.iikoweb.ru');
  });

  it('оставляет нестандартный порт', () => {
    expect(normalizeServerAddress('srv.local:8080', { dropPort443: true })).toBe('srv.local:8080');
  });

  it('cleanWebUrl оставляет только хост', () => {
    expect(cleanWebUrl('https://my-cloud.syrve.app:443/path')).toBe('my-cloud.syrve.app');
  });

  it('isIikoWebAddress определяет только iikoweb и syrve.app', () => {
    expect(isIikoWebAddress('https://foo.iikoweb.ru:443')).toBe(true);
    expect(isIikoWebAddress('bar.syrve.app:443')).toBe(true);
    expect(isIikoWebAddress('bar.syrve.online:443')).toBe(false);
    expect(isIikoWebAddress('10.10.10.10:8080')).toBe(false);
  });
});
