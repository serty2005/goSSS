import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import enAuth from '@/locales/en/auth.json';
import enCommon from '@/locales/en/common.json';
import enLayout from '@/locales/en/layout.json';
import ruAuth from '@/locales/ru/auth.json';
import ruCommon from '@/locales/ru/common.json';
import ruLayout from '@/locales/ru/layout.json';
import {
  DEFAULT_APP_LOCALE,
  FALLBACK_APP_LOCALE,
  getBrowserLocale,
  getUrlAppLocale,
  getStoredAppLocale,
  resolveAppLocaleCode,
} from './supportedLocales';

const resources = {
  en: {
    auth: enAuth,
    common: enCommon,
    layout: enLayout,
  },
  ru: {
    auth: ruAuth,
    common: ruCommon,
    layout: ruLayout,
  },
} as const;

const initialLocale = resolveAppLocaleCode(
  getUrlAppLocale(),
  getStoredAppLocale(),
  getBrowserLocale(),
  DEFAULT_APP_LOCALE,
  FALLBACK_APP_LOCALE,
);

if (!i18n.isInitialized) {
  void i18n
    .use(initReactI18next)
    .init({
      resources,
      lng: initialLocale,
      fallbackLng: DEFAULT_APP_LOCALE,
      supportedLngs: ['en', 'ru'],
      ns: ['auth', 'common', 'layout'],
      defaultNS: 'common',
      interpolation: {
        escapeValue: false,
      },
      returnEmptyString: false,
      react: {
        useSuspense: false,
      },
    });
}

export default i18n;
