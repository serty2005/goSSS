import enUS from 'antd/locale/en_US';
import ruRU from 'antd/locale/ru_RU';
import 'dayjs/locale/en';
import 'dayjs/locale/ru';
import type { GlobalTranslationLocaleDTO } from '@/types/api';
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
export const builtInSupportedLocaleList = supportedLocaleList;

const normalizeLocaleCandidate = (value: string | null | undefined): string => {
  return String(value || '')
    .trim()
    .toLowerCase()
    .replace('_', '-');
};

const resolveLocaleFromList = (
  candidate: string | null | undefined,
  availableLocaleCodes: string[],
): string => {
  const normalizedCandidate = normalizeLocaleCandidate(candidate);
  if (!normalizedCandidate) {
    return '';
  }

  const availableByCode = new Map(
    availableLocaleCodes.map((code) => [normalizeLocaleCandidate(code), code]),
  );

  if (availableByCode.has(normalizedCandidate)) {
    return String(availableByCode.get(normalizedCandidate) || '');
  }

  const baseCandidate = normalizedCandidate.split('-')[0];
  return String(availableByCode.get(baseCandidate) || '');
};

export const isSupportedLocale = (value: unknown): value is AppLocaleCode => {
  return Boolean(resolveLocaleFromList(typeof value === 'string' ? value : '', Object.keys(localeRegistry)));
};

export const resolveAppLocaleCode = (...candidates: Array<string | null | undefined>): AppLocaleCode => {
  return resolveAppLocaleCodeFromList(Object.keys(localeRegistry), ...candidates);
};

export const resolveAppLocaleCodeFromList = (
  availableLocaleCodes: string[],
  ...candidates: Array<string | null | undefined>
): AppLocaleCode => {
  for (const candidate of candidates) {
    const resolved = resolveLocaleFromList(candidate, availableLocaleCodes);
    if (resolved) {
      return resolved;
    }
  }
  return DEFAULT_APP_LOCALE;
};

const createCustomLocaleDefinition = (
  code: string,
  label?: string,
  nativeLabel?: string,
): SupportedLocaleDefinition => ({
  code,
  label: label || code.toUpperCase(),
  nativeLabel: nativeLabel || label || code.toUpperCase(),
  antdLocale: enUS,
  dayjsLocale: 'en',
  intlLocale: code,
  enabled: true,
  isDefault: false,
});

export const buildSupportedLocaleList = (
  customLocales: GlobalTranslationLocaleDTO[] = [],
): SupportedLocaleDefinition[] => {
  const result = [...supportedLocaleList];
  const existingCodes = new Set(result.map((item) => item.code));

  customLocales.forEach((locale) => {
    const code = normalizeLocaleCandidate(locale.code);
    if (!code || existingCodes.has(code)) {
      return;
    }

    result.push(
      createCustomLocaleDefinition(
        code,
        String(locale.label || '').trim(),
        String(locale.native_label || '').trim(),
      ),
    );
    existingCodes.add(code);
  });

  return result;
};

export const getSupportedLocale = (locale: string | null | undefined): SupportedLocaleDefinition => {
  const resolved = resolveLocaleFromList(locale, Object.keys(localeRegistry));
  if (resolved) {
    return localeRegistry[resolved];
  }

  const customCode = normalizeLocaleCandidate(locale);
  return customCode
    ? createCustomLocaleDefinition(customCode)
    : localeRegistry[DEFAULT_APP_LOCALE];
};

export const getStoredAppLocale = (): string | null => {
  if (typeof window === 'undefined') {
    return null;
  }
  try {
    return window.localStorage.getItem(APP_LOCALE_STORAGE_KEY);
  } catch {
    return null;
  }
};

export const getUrlAppLocale = (): string | null => {
  if (typeof window === 'undefined') {
    return null;
  }
  try {
    const params = new URLSearchParams(window.location.search);
    return params.get('locale');
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
