import enUS from 'antd/locale/en_US';
import ruRU from 'antd/locale/ru_RU';
import 'dayjs/locale/en';
import 'dayjs/locale/ru';
import type { AppLocaleCode, SupportedLocaleDefinition } from './localeTypes';

export const DEFAULT_APP_LOCALE: AppLocaleCode = 'en';
export const FALLBACK_APP_LOCALE: AppLocaleCode = 'ru';
export const APP_LOCALE_STORAGE_KEY = 'etalon-ui-locale';

const localeRegistry: Record<AppLocaleCode, SupportedLocaleDefinition> = {
  en: {
    code: 'en',
    label: 'English',
    nativeLabel: 'English',
    antdLocale: enUS,
    dayjsLocale: 'en',
    intlLocale: 'en-US',
    enabled: true,
    isDefault: true,
  },
  ru: {
    code: 'ru',
    label: 'Russian',
    nativeLabel: 'Русский',
    antdLocale: ruRU,
    dayjsLocale: 'ru',
    intlLocale: 'ru-RU',
    enabled: true,
    isDefault: false,
  },
};

export const supportedLocales = localeRegistry;
export const supportedLocaleList = Object.values(localeRegistry);

const normalizeLocaleCandidate = (value: string | null | undefined): string => {
  return String(value || '')
    .trim()
    .toLowerCase()
    .replace('_', '-')
    .split('-')[0];
};

export const isSupportedLocale = (value: unknown): value is AppLocaleCode => {
  const normalized = normalizeLocaleCandidate(typeof value === 'string' ? value : '');
  return normalized === 'en' || normalized === 'ru';
};

export const resolveAppLocaleCode = (...candidates: Array<string | null | undefined>): AppLocaleCode => {
  for (const candidate of candidates) {
    const normalized = normalizeLocaleCandidate(candidate);
    if (isSupportedLocale(normalized)) {
      return normalized;
    }
  }
  return DEFAULT_APP_LOCALE;
};

export const getSupportedLocale = (locale: string | null | undefined): SupportedLocaleDefinition => {
  return localeRegistry[resolveAppLocaleCode(locale)];
};

export const getStoredAppLocale = (): AppLocaleCode | null => {
  if (typeof window === 'undefined') {
    return null;
  }
  try {
    const stored = window.localStorage.getItem(APP_LOCALE_STORAGE_KEY);
    return stored && isSupportedLocale(stored) ? stored : null;
  } catch {
    return null;
  }
};

export const getUrlAppLocale = (): AppLocaleCode | null => {
  if (typeof window === 'undefined') {
    return null;
  }
  try {
    const params = new URLSearchParams(window.location.search);
    const locale = params.get('locale');
    return locale && isSupportedLocale(locale) ? resolveAppLocaleCode(locale) : null;
  } catch {
    return null;
  }
};

export const persistAppLocale = (locale: AppLocaleCode) => {
  if (typeof window === 'undefined') {
    return;
  }
  try {
    window.localStorage.setItem(APP_LOCALE_STORAGE_KEY, locale);
  } catch {
    // Игнорируем ошибки локального хранилища и продолжаем работу с активной locale.
  }
};

export const getBrowserLocale = (): string | null => {
  if (typeof navigator === 'undefined') {
    return null;
  }
  const [firstPreferredLocale] = navigator.languages || [];
  return firstPreferredLocale || navigator.language || null;
};
