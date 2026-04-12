import type { GlobalTranslationsDTO } from '@/types/api';
import i18n, { resources } from './i18n';

export const TRANSLATION_NAMESPACES = [
  'admin',
  'auth',
  'common',
  'layout',
  'tickets',
] as const;

export type TranslationNamespace = (typeof TRANSLATION_NAMESPACES)[number];

export type TranslationOverrideMap = Partial<
  Record<string, Partial<Record<TranslationNamespace, Record<string, string>>>>
>;

type TranslationResourceMap = typeof resources;
type BuiltInLocaleCode = keyof TranslationResourceMap;
type FlattenedTranslationMap = Record<string, string>;

const isRecord = (value: unknown): value is Record<string, unknown> =>
  Boolean(value) && typeof value === 'object' && !Array.isArray(value);

const cloneResource = <T,>(value: T): T =>
  JSON.parse(JSON.stringify(value)) as T;

const builtInResources = cloneResource(resources) as TranslationResourceMap;

const setNestedValue = (
  target: Record<string, unknown>,
  key: string,
  value: string,
) => {
  const parts = key
    .split('.')
    .map((item) => item.trim())
    .filter(Boolean);
  if (parts.length === 0) {
    return;
  }

  let cursor: Record<string, unknown> = target;
  parts.forEach((part, index) => {
    if (index === parts.length - 1) {
      cursor[part] = value;
      return;
    }
    if (!isRecord(cursor[part])) {
      cursor[part] = {};
    }
    cursor = cursor[part] as Record<string, unknown>;
  });
};

const flattenObject = (
  source: Record<string, unknown>,
  prefix = '',
): Record<string, string> => {
  const result: Record<string, string> = {};

  Object.entries(source).forEach(([key, value]) => {
    const nextKey = prefix ? `${prefix}.${key}` : key;
    if (typeof value === 'string') {
      result[nextKey] = value;
      return;
    }
    if (isRecord(value)) {
      Object.assign(result, flattenObject(value, nextKey));
    }
  });

  return result;
};

const BUILT_IN_LOCALES = Object.keys(builtInResources) as BuiltInLocaleCode[];
let previousCustomLocaleCodes: string[] = [];

export const getTranslationOverrides = (
  catalog?: GlobalTranslationsDTO | null,
): TranslationOverrideMap => {
  if (!catalog || !isRecord(catalog.overrides)) {
    return {};
  }

  const result: TranslationOverrideMap = {};

  Object.entries(catalog.overrides).forEach(([localeCode, localeValue]) => {
    if (!isRecord(localeValue)) {
      return;
    }

    const localeMap: Partial<Record<TranslationNamespace, Record<string, string>>> = {};

    TRANSLATION_NAMESPACES.forEach((namespace) => {
      const namespaceValue = localeValue[namespace];
      if (!isRecord(namespaceValue)) {
        return;
      }

      const entries: Record<string, string> = {};
      Object.entries(namespaceValue).forEach(([key, value]) => {
        if (typeof value !== 'string') {
          return;
        }
        const normalizedKey = key.trim();
        if (!normalizedKey) {
          return;
        }
        entries[normalizedKey] = value;
      });

      if (Object.keys(entries).length > 0) {
        localeMap[namespace] = entries;
      }
    });

    if (Object.keys(localeMap).length > 0) {
      result[localeCode] = localeMap;
    }
  });

  return result;
};

export const buildTranslationOverrideBundle = (
  entries: Record<string, string>,
): Record<string, unknown> => {
  const bundle: Record<string, unknown> = {};
  Object.entries(entries).forEach(([key, value]) => {
    setNestedValue(bundle, key, value);
  });
  return bundle;
};

export const getBaseTranslationEntries = (
  locale: string,
  namespace: TranslationNamespace,
): Record<string, string> => {
  const bundle = builtInResources[locale as BuiltInLocaleCode]?.[
    namespace as keyof TranslationResourceMap[BuiltInLocaleCode]
  ];

  if (!isRecord(bundle)) {
    return {};
  }

  return flattenObject(bundle);
};

export const splitTranslationKey = (
  fullKey: string,
): { namespace: TranslationNamespace; key: string } | null => {
  const normalized = String(fullKey || '').trim();
  if (!normalized) {
    return null;
  }

  for (const namespace of TRANSLATION_NAMESPACES) {
    const prefix = `${namespace}.`;
    if (normalized.startsWith(prefix)) {
      return {
        namespace,
        key: normalized.slice(prefix.length),
      };
    }
  }

  return null;
};

export const getAllBaseTranslationEntries = (locale: string): FlattenedTranslationMap => {
  const result: FlattenedTranslationMap = {};

  TRANSLATION_NAMESPACES.forEach((namespace) => {
    Object.entries(getBaseTranslationEntries(locale, namespace)).forEach(([key, value]) => {
      result[`${namespace}.${key}`] = value;
    });
  });

  return result;
};

export const getAllTranslationEntries = (
  locale: string,
  catalog?: GlobalTranslationsDTO | null,
): FlattenedTranslationMap => {
  const result = { ...getAllBaseTranslationEntries(locale) };
  const overrides = getTranslationOverrides(catalog);

  TRANSLATION_NAMESPACES.forEach((namespace) => {
    Object.entries(overrides[locale]?.[namespace] || {}).forEach(([key, value]) => {
      result[`${namespace}.${key}`] = value;
    });
  });

  return result;
};

export const syncCustomTranslationResources = (catalog?: GlobalTranslationsDTO | null) => {
  const overrides = getTranslationOverrides(catalog);

  BUILT_IN_LOCALES.forEach((code) => {
    TRANSLATION_NAMESPACES.forEach((namespace) => {
      const baseBundle = cloneResource(builtInResources[code][namespace]);
      i18n.removeResourceBundle(code, namespace);
      i18n.addResourceBundle(code, namespace, baseBundle, true, true);

      const namespaceOverrides = overrides[code]?.[namespace];
      if (!namespaceOverrides || Object.keys(namespaceOverrides).length === 0) {
        return;
      }

      i18n.addResourceBundle(
        code,
        namespace,
        buildTranslationOverrideBundle(namespaceOverrides),
        true,
        true,
      );
    });
  });

  const nextCustomLocaleCodes = (catalog?.locales || [])
    .filter((locale) => locale.is_builtin !== true)
    .map((locale) => String(locale.code || '').trim())
    .filter(Boolean);

  const customCodesToReset = new Set([
    ...previousCustomLocaleCodes,
    ...nextCustomLocaleCodes,
  ]);

  customCodesToReset.forEach((localeCode) => {
    TRANSLATION_NAMESPACES.forEach((namespace) => {
      i18n.removeResourceBundle(localeCode, namespace);
    });

    if (!nextCustomLocaleCodes.includes(localeCode)) {
      return;
    }

    TRANSLATION_NAMESPACES.forEach((namespace) => {
      const namespaceOverrides = overrides[localeCode]?.[namespace] || {};
      i18n.addResourceBundle(
        localeCode,
        namespace,
        buildTranslationOverrideBundle(namespaceOverrides),
        true,
        true,
      );
    });
  });

  previousCustomLocaleCodes = nextCustomLocaleCodes;
  const nextSupportedLngs = [
    ...BUILT_IN_LOCALES,
    ...nextCustomLocaleCodes,
  ];
  i18n.options.supportedLngs = nextSupportedLngs;
  if (i18n.services.languageUtils) {
    i18n.services.languageUtils.supportedLngs = [
      ...nextSupportedLngs,
      'cimode',
    ];
  }

  if (i18n.language && nextSupportedLngs.includes(i18n.language)) {
    void i18n.changeLanguage(i18n.language);
  }
};
