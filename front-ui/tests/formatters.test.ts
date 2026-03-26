import { describe, expect, it } from 'vitest';
import {
  cleanWebUrl,
  getIikoWebAppLinkMeta,
  isIikoWebAddress,
  normalizeIikoWebAppUrl,
  normalizeServerAddress,
} from '@/utils/formatters';

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

  it('normalizeIikoWebAppUrl приводит ссылку к каноническому виду', () => {
    expect(normalizeIikoWebAppUrl('HTTP://Restaurant-Margaret.SYRVE.APP/app?foo=bar'))
      .toBe('https://restaurant-margaret.syrve.app/');
    expect(normalizeIikoWebAppUrl('ссылка: 809-613-203.iikoweb.ru/login'))
      .toBe('https://809-613-203.iikoweb.ru/');
  });

  it('getIikoWebAppLinkMeta определяет тип приложения', () => {
    expect(getIikoWebAppLinkMeta('https://restaurant-margaret.syrve.app/')).toEqual({
      host: 'restaurant-margaret.syrve.app',
      label: 'SyrveApp',
      url: 'https://restaurant-margaret.syrve.app/',
    });
    expect(getIikoWebAppLinkMeta('https://809-613-203.iikoweb.ru/')).toEqual({
      host: '809-613-203.iikoweb.ru',
      label: 'iikoWeb',
      url: 'https://809-613-203.iikoweb.ru/',
    });
    expect(getIikoWebAppLinkMeta('https://example.com')).toBeNull();
  });
});
