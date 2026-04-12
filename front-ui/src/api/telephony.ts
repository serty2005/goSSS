import apiClient from "./axios";
import type {
  ApiResponse,
  MegafonUserSuggestionDTO,
  TelephonyCallListParams,
  TelephonyCallListResponseDTO,
  TelephonyContactCompanyDTO,
  TelephonyLineDTO,
  TelephonyPendingContextDTO,
} from "@/types/api";

const normalizeTelephonyParams = (params: TelephonyCallListParams = {}) => ({
  ...params,
  status: Array.isArray(params.status)
    ? params.status.join(",")
    : params.status,
  group_name: Array.isArray(params.group_name)
    ? params.group_name.join(",")
    : params.group_name,
});

export const telephonyApi = {
  getLine: async () => {
    const response =
      await apiClient.get<ApiResponse<TelephonyLineDTO>>("/telephony/line");
    return response.data.data;
  },

  getMyPendingContext: async () => {
    const response = await apiClient.get<
      ApiResponse<TelephonyPendingContextDTO | null>
    >("/telephony/pending-context/me");
    return response.data.data;
  },

  bindPendingContext: async (id: string, ticketId: string, contactName?: string) => {
    const response = await apiClient.post<ApiResponse<{ status: string }>>(
      `/telephony/pending-context/${id}/bind-ticket`,
      {
        ticket_id: ticketId,
        contact_name: contactName,
      },
    );
    return response.data;
  },

  bindCallToTicket: async (id: string, ticketId: string, contactName?: string) => {
    const response = await apiClient.post<ApiResponse<{ status: string }>>(
      `/telephony/calls/${id}/bind-ticket`,
      {
        ticket_id: ticketId,
        contact_name: contactName,
      },
    );
    return response.data;
  },

  getContactCompanies: async (contactId: number) => {
    const response = await apiClient.get<
      ApiResponse<{ items: TelephonyContactCompanyDTO[] }>
    >(`/telephony/contacts/${contactId}/companies`);
    return response.data.data.items;
  },

  getUserCalls: async (
    userId: number,
    params: TelephonyCallListParams = {},
  ) => {
    const response = await apiClient.get<
      ApiResponse<TelephonyCallListResponseDTO>
    >(`/telephony/users/${userId}/calls`, {
      params: normalizeTelephonyParams(params),
    });
    return response.data.data;
  },

  getCalls: async (params: TelephonyCallListParams = {}) => {
    const response = await apiClient.get<
      ApiResponse<TelephonyCallListResponseDTO>
    >("/telephony/calls", {
      params: normalizeTelephonyParams(params),
    });
    return response.data.data;
  },

  suggestMegafonUser: async (params: {
    first_name: string;
    last_name: string;
    full_name?: string;
  }) => {
    const response = await apiClient.get<
      ApiResponse<{ suggestion?: MegafonUserSuggestionDTO | null }>
    >("/megafon-vats/users/suggest", {
      params,
    });
    return response.data;
  },
};
