import apiClient from './axios';
import { ApiResponse, BitrixServicePointDTO, DashboardStatsDTO, TicketAttachmentDTO, TicketCreatePayload, TicketDTO, TicketDetailsDTO, TicketFiltersResponse, TicketListItemDTO, TicketListParams } from '@/types/api';

export const ticketsApi = {
  getTickets: async (params: TicketListParams = {}) => {
    const normalizedParams = {
      ...params,
      status: Array.isArray(params.status) ? params.status.join(',') : params.status,
    };
    const response = await apiClient.get<ApiResponse<TicketListItemDTO[]>>('/tickets', {
      params: normalizedParams,
    });
    return response.data;
  },

  getTicketFilters: async (params: TicketListParams = {}) => {
    const normalizedParams = {
      ...params,
      status: Array.isArray(params.status) ? params.status.join(',') : params.status,
    };
    const response = await apiClient.get<ApiResponse<TicketFiltersResponse>>('/tickets/filters', {
      params: normalizedParams,
    });
    return response.data;
  },

  getTicket: async (id: number | string) => {
    const response = await apiClient.get<ApiResponse<TicketDetailsDTO>>(`/tickets/${id}`);
    return response.data;
  },

  createTicket: async (payload: TicketCreatePayload) => {
    const response = await apiClient.post<ApiResponse<TicketDTO>>('/tickets', payload);
    return response.data;
  },

  changeStatus: async (id: number | string, status: string, comment?: string) => {
    const response = await apiClient.patch<ApiResponse<TicketDTO>>(`/tickets/${id}/status`, {
      status,
      comment,
    });
    return response.data;
  },

  updateDescription: async (id: number | string, description: string) => {
    const response = await apiClient.patch<ApiResponse<TicketDTO>>(`/tickets/${id}/description`, {
      description,
    });
    return response.data;
  },

  getDashboardStats: async () => {
    const response = await apiClient.get<ApiResponse<DashboardStatsDTO>>('/tickets/stats/dashboard');
    return response.data;
  },

  changeCompany: async (id: number | string, companyId: string) => {
    const response = await apiClient.patch<ApiResponse<TicketDTO>>(`/tickets/${id}/company`, {
      company_id: companyId,
    });
    return response.data;
  },

  assign: async (id: number | string, assignee_id?: number) => {
    const response = await apiClient.patch<ApiResponse<TicketDTO>>(`/tickets/${id}/assign`, {
      assignee_id: assignee_id ?? null,
    });
    return response.data;
  },

  updateBitrixFields: async (id: number | string, payload: { bitrix_service_point_id?: number; bitrix_deal_title: string }) => {
    const response = await apiClient.patch<ApiResponse<TicketDTO>>(`/tickets/${id}/bitrix`, {
      bitrix_service_point_id: payload.bitrix_service_point_id,
      bitrix_deal_title: payload.bitrix_deal_title,
    });
    return response.data;
  },

  addComment: async (id: number | string, comment: string, isPrivate = false) => {
    const response = await apiClient.post<ApiResponse<{ status: string }>>(`/tickets/${id}/comments`, {
      comment,
      is_private: isPrivate,
    });
    return response.data;
  },

  getBitrixServicePoints: async (params?: { term?: string; limit?: number; offset?: number; random_if_empty?: boolean }) => {
    const response = await apiClient.get<BitrixServicePointDTO[] | ApiResponse<BitrixServicePointDTO[]>>('/bitrix/service-points', {
      params,
    });
    const payload = response.data as unknown;
    if (Array.isArray(payload)) {
      return payload;
    }
    if (payload && typeof payload === 'object' && 'data' in (payload as Record<string, unknown>)) {
      const data = (payload as { data?: unknown }).data;
      return Array.isArray(data) ? (data as BitrixServicePointDTO[]) : [];
    }
    return [];
  },

  refreshCommentsFromServiceDesk: async (id: number | string) => {
    const response = await apiClient.post<ApiResponse<{ status: string; added: number }>>(`/tickets/${id}/refresh-comments`);
    return response.data;
  },

  recordConnectionCopy: async (id: number | string, label: string, value: string) => {
    const response = await apiClient.post<ApiResponse<{ status: string }>>(`/tickets/${id}/connection-copy`, {
      label,
      value,
    });
    return response.data;
  },

  uploadAttachments: async (id: number | string, files: File[]) => {
    const formData = new FormData();
    files.forEach((file) => formData.append('files', file));
    const response = await apiClient.post<ApiResponse<{ items: TicketAttachmentDTO[] }>>(
      `/tickets/${id}/attachments`,
      formData,
      {
        headers: {
          'Content-Type': 'multipart/form-data',
        },
      },
    );
    return response.data;
  },
};
