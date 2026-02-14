import dayjs from 'dayjs';

interface NormalizeServerAddressOptions {
  dropAnyPort?: boolean;
  dropPort443?: boolean;
}

export const formatRnm = (rnm?: string): string => {
  if (!rnm) return '';
  return rnm.replace(/\D/g, '').replace(/(\d{4})(?=\d)/g, '$1 ').trim();
};

export const normalizeServerAddress = (
  rawAddress?: string,
  options: NormalizeServerAddressOptions = {},
): string => {
  if (!rawAddress) return '';

  const value = String(rawAddress).trim();
  if (!value) return '';

  try {
    const parseable = value.includes('://') ? value : `http://${value}`;
    const parsed = new URL(parseable);
    const host = parsed.hostname.trim().toLowerCase();
    if (!host) return '';

    const port = parsed.port.trim();
    if (options.dropAnyPort) {
      return host;
    }
    if (port && !(options.dropPort443 && port === '443')) {
      return `${host}:${port}`;
    }
    return host;
  } catch {
    const withoutProtocol = value.replace(/^https?:\/\//i, '');
    const withoutPath = withoutProtocol.split('/')[0].trim().toLowerCase();
    if (!withoutPath) return '';

    const [host, port] = withoutPath.split(':', 2);
    if (!host) return '';
    if (options.dropAnyPort) return host;
    if (port && !(options.dropPort443 && port === '443')) return `${host}:${port}`;
    return host;
  }
};

export const cleanWebUrl = (url?: string): string =>
  normalizeServerAddress(url, { dropAnyPort: true });

export const isIikoWebAddress = (rawAddress?: string): boolean => {
  const normalized = normalizeServerAddress(rawAddress, { dropAnyPort: true });
  if (!normalized) return false;
  return normalized.includes('iikoweb') || normalized.includes('syrve.app');
};

export const formatServerEdition = (edition?: string): string => {
  if (!edition) return '';
  const lower = edition.toLowerCase();
  if (lower === 'default') return 'RMS';
  if (lower === 'chain') return 'Chain';
  return edition;
};

export const formatDate = (date?: string): string => {
  if (!date) return '-';
  return dayjs(date).format('DD.MM.YYYY HH:mm');
};
