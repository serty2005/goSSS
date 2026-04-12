import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import enAdmin from '@/locales/en/admin.json';
import enAuth from '@/locales/en/auth.json';
import enCommon from '@/locales/en/common.json';
import enLayout from '@/locales/en/layout.json';
import enTickets from '@/locales/en/tickets.json';
import ruAdmin from '@/locales/ru/admin.json';
import ruAuth from '@/locales/ru/auth.json';
import ruCommon from '@/locales/ru/common.json';
import ruLayout from '@/locales/ru/layout.json';
import ruTickets from '@/locales/ru/tickets.json';
import {
  DEFAULT_APP_LOCALE,
  FALLBACK_APP_LOCALE,
  getBrowserLocale,
  getUrlAppLocale,
  getStoredAppLocale,
  resolveAppLocaleCode,
} from './supportedLocales';

export const resources = {
  en: {
    admin: enAdmin,
    auth: enAuth,
    common: enCommon,
    layout: enLayout,
    tickets: enTickets,
  },
  ru: {
    admin: ruAdmin,
    auth: ruAuth,
    common: ruCommon,
    layout: ruLayout,
    tickets: ruTickets,
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
      ns: ['admin', 'auth', 'common', 'layout', 'tickets'],
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
