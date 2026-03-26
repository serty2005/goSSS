import dayjs from 'dayjs';
import { useEffect, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { useAuthStore } from '@/store/authStore';
import type { AppLocaleCode } from './localeTypes';
import {
  DEFAULT_APP_LOCALE,
  getBrowserLocale,
  getStoredAppLocale,
  getSupportedLocale,
  getUrlAppLocale,
  persistAppLocale,
  resolveAppLocaleCode,
  supportedLocaleList,
} from './supportedLocales';

export const useAppLocale = () => {
  const profileLocale = useAuthStore((state) => state.user?.profile_config?.interface?.locale);
  const { i18n } = useTranslation();
  const urlLocale = useMemo(() => getUrlAppLocale(), []);
  const storedLocale = useMemo(() => getStoredAppLocale(), []);
  const browserLocale = useMemo(() => getBrowserLocale(), []);

  const locale = useMemo(
    () => resolveAppLocaleCode(
      urlLocale,
      profileLocale,
      storedLocale,
      i18n.resolvedLanguage,
      browserLocale,
      DEFAULT_APP_LOCALE,
    ),
    [browserLocale, i18n.resolvedLanguage, profileLocale, storedLocale, urlLocale],
  );

  const localeDefinition = useMemo(() => getSupportedLocale(locale), [locale]);

  useEffect(() => {
    persistAppLocale(locale);
    if (typeof document !== 'undefined') {
      document.documentElement.lang = locale;
    }
    if (dayjs.locale() !== localeDefinition.dayjsLocale) {
      dayjs.locale(localeDefinition.dayjsLocale);
    }
    if (i18n.resolvedLanguage !== locale) {
      void i18n.changeLanguage(locale);
    }
  }, [i18n, locale, localeDefinition.dayjsLocale]);

  const setLocale = async (nextLocale: AppLocaleCode) => {
    const nextLocaleDefinition = getSupportedLocale(nextLocale);
    persistAppLocale(nextLocale);
    if (typeof document !== 'undefined') {
      document.documentElement.lang = nextLocale;
    }
    dayjs.locale(nextLocaleDefinition.dayjsLocale);
    await i18n.changeLanguage(nextLocale);
  };

  return {
    locale,
    antdLocale: localeDefinition.antdLocale,
    dayjsLocale: localeDefinition.dayjsLocale,
    supportedLocales: supportedLocaleList,
    setLocale,
  };
};
