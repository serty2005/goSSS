import dayjs from 'dayjs';

interface NormalizeServerAddressOptions {
  dropAnyPort?: boolean;
  dropPort443?: boolean;
}

const iikoWebAppHostRegex = /\b((?:[a-z0-9-]+\.)*[a-z0-9-]+\.(?:syrve\.app|iikoweb\.ru))\b/i;

export interface IikoWebAppLinkMeta {
  host: string;
  label: 'SyrveApp' | 'iikoWeb';
  url: string;
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

export const normalizeIikoWebAppUrl = (raw?: string): string => {
  if (!raw) return '';

  const match = String(raw).trim().toLowerCase().match(iikoWebAppHostRegex);
  const host = match?.[1]?.trim();
  if (!host) return '';

  return `https://${host}/`;
};

export const getIikoWebAppLinkMeta = (raw?: string): IikoWebAppLinkMeta | null => {
  const url = normalizeIikoWebAppUrl(raw);
  if (!url) return null;

  const host = url.replace(/^https:\/\//, '').replace(/\/$/, '');
  return {
    host,
    label: host.endsWith('.syrve.app') ? 'SyrveApp' : 'iikoWeb',
    url,
  };
};

export const isIikoWebAddress = (rawAddress?: string): boolean => {
  return Boolean(getIikoWebAppLinkMeta(rawAddress));
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
