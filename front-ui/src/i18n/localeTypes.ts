import type enUS from 'antd/locale/en_US';

export type AppLocaleCode = string;

export type AntdLocaleDefinition = typeof enUS;

export interface SupportedLocaleDefinition {
  code: AppLocaleCode;
  label: string;
  nativeLabel: string;
  antdLocale: AntdLocaleDefinition;
  dayjsLocale: string;
  intlLocale: string;
  enabled: boolean;
  isDefault: boolean;
}
