import apiClient from './axios';
import type {
  ApiResponse,
  TelephonyCallListParams,
  TelephonyCallListResponseDTO,
  TelephonyContactCompanyDTO,
  TelephonyLineDTO,
  TelephonyPendingContextDTO,
} from '@/types/api';

const normalizeTelephonyParams = (params: TelephonyCallListParams = {}) => ({
  ...params,
  status: Array.isArray(params.status) ? params.status.join(',') : params.status,
});

export const telephonyApi = {
  getLine: async () => {
    const response = await apiClient.get<TelephonyLineDTO>('/telephony/line');
    return response.data;
  },

  getMyPendingContext: async () => {
    const response = await apiClient.get<ApiResponse<TelephonyPendingContextDTO | null>>('/telephony/pending-context/me');
    return response.data.data;
  },

  bindPendingContext: async (id: string, ticketId: string) => {
    const response = await apiClient.post<ApiResponse<{ status: string }>>(`/telephony/pending-context/${id}/bind-ticket`, {
      ticket_id: ticketId,
    });
    return response.data;
  },

  getContactCompanies: async (contactId: number) => {
    const response = await apiClient.get<{ items: TelephonyContactCompanyDTO[] }>(`/telephony/contacts/${contactId}/companies`);
    return response.data.items;
  },

  getUserCalls: async (userId: number, params: TelephonyCallListParams = {}) => {
    const response = await apiClient.get<TelephonyCallListResponseDTO>(`/telephony/users/${userId}/calls`, {
      params: normalizeTelephonyParams(params),
    });
    return response.data;
  },

  getCalls: async (params: TelephonyCallListParams = {}) => {
    const response = await apiClient.get<TelephonyCallListResponseDTO>('/telephony/calls', {
      params: normalizeTelephonyParams(params),
    });
    return response.data;
  },
};
