import dayjs, { type Dayjs } from 'dayjs';
import { getSupportedLocale } from './supportedLocales';

type DateLike = string | number | Date | Dayjs | null | undefined;

const normalizeDate = (value: DateLike): Date | null => {
  if (!value) {
    return null;
  }
  if (value instanceof Date) {
    return Number.isNaN(value.getTime()) ? null : value;
  }
  if (dayjs.isDayjs(value)) {
    return value.isValid() ? value.toDate() : null;
  }
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? null : parsed;
};

const formatWithIntl = (
  value: DateLike,
  locale: string | null | undefined,
  options: Intl.DateTimeFormatOptions,
): string => {
  const normalizedDate = normalizeDate(value);
  if (!normalizedDate) {
    return '';
  }
  const localeDefinition = getSupportedLocale(locale);
  return new Intl.DateTimeFormat(localeDefinition.intlLocale, options).format(normalizedDate);
};

export const formatDate = (value: DateLike, locale?: string | null) => {
  return formatWithIntl(value, locale, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  });
};

export const formatDateTime = (value: DateLike, locale?: string | null) => {
  return formatWithIntl(value, locale, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
};

export const formatLocaleDateTime = (value: DateLike, locale?: string | null) => {
  return formatDateTime(value, locale);
};

export const formatDateForTicketStamp = (value: DateLike, locale?: string | null) => {
  return formatWithIntl(value, locale, {
    dateStyle: 'medium',
    timeStyle: 'short',
  });
};

export const formatRelativeTime = (
  value: DateLike,
  locale?: string | null,
  now: Date = new Date(),
): string => {
  const normalizedDate = normalizeDate(value);
  if (!normalizedDate) {
    return '';
  }

  const diffMilliseconds = normalizedDate.getTime() - now.getTime();
  const localeDefinition = getSupportedLocale(locale);
  const formatter = new Intl.RelativeTimeFormat(localeDefinition.intlLocale, { numeric: 'auto' });
  const units: Array<[Intl.RelativeTimeFormatUnit, number]> = [
    ['day', 24 * 60 * 60 * 1000],
    ['hour', 60 * 60 * 1000],
    ['minute', 60 * 1000],
  ];

  for (const [unit, unitSize] of units) {
    if (Math.abs(diffMilliseconds) >= unitSize || unit === 'minute') {
      return formatter.format(Math.round(diffMilliseconds / unitSize), unit);
    }
  }

  return formatter.format(0, 'minute');
};
