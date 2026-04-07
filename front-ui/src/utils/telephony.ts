import type { TelephonyContactDTO } from '@/types/api';

const trimPhone = (value?: string | null) => String(value || '').trim();

export const getTelephonyContactPhoneDisplay = (
  contact?: TelephonyContactDTO | null,
  fallbackPhone?: string | null,
) => {
  return trimPhone(contact?.phone_display || contact?.phone_normalized || fallbackPhone);
};

export const getTelephonyContactLabel = (
  contact?: TelephonyContactDTO | null,
  fallbackPhone?: string | null,
) => {
  const phone = getTelephonyContactPhoneDisplay(contact, fallbackPhone);
  const name = trimPhone(contact?.name);
  if (name && phone) {
    return `${name} (${phone})`;
  }
  return name || phone;
};

export const formatPhoneForCopy = (value?: string | null) => {
  const raw = trimPhone(value)
    .replace(/(?:доб|ext)\.?\s*\d+.*$/i, '')
    .replace(/[^\d+]/g, '');
  if (!raw) {
    return '';
  }

  if (raw.startsWith('+')) {
    return `+${raw.slice(1).replace(/\+/g, '')}`;
  }

  const digits = raw.replace(/\D/g, '');
  if (!digits) {
    return '';
  }
  if (digits.length === 11 && digits.startsWith('8')) {
    return `+7${digits.slice(1)}`;
  }
  return `+${digits}`;
};

export const getTelephonyContactPhoneForCopy = (
  contact?: TelephonyContactDTO | null,
  fallbackPhone?: string | null,
) => {
  return formatPhoneForCopy(contact?.phone_normalized || contact?.phone_display || fallbackPhone);
};
