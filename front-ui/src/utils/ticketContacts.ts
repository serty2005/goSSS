import type { TicketContactDTO } from '@/types/api';
import { getTelephonyContactPhoneForCopy } from '@/utils/telephony';

export const getPrimaryTicketContact = (contacts?: TicketContactDTO[]) =>
  (contacts || []).find((item) => item.is_primary) || (contacts || [])[0];

export const getPrimaryTicketPhone = (contacts?: TicketContactDTO[], fallbackPhone?: string) => {
  const primary = getPrimaryTicketContact(contacts);
  if (primary?.contact_type === 'phone') {
    return getTelephonyContactPhoneForCopy(primary.telephony_contact) || primary.value || primary.display_value || '';
  }
  return fallbackPhone || '';
};

export const getPrimaryTicketTelegram = (contacts?: TicketContactDTO[]) => {
  const primary = getPrimaryTicketContact(contacts);
  if (primary?.contact_type === 'telegram') {
    return primary.display_value || primary.value || '';
  }
  const telegram = (contacts || []).find((item) => item.contact_type === 'telegram');
  return telegram?.display_value || telegram?.value || '';
};

