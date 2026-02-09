import apiClient from './axios';
import { ApiResponse, TicketCreatePayload, TicketDTO, TicketDetailsDTO, TicketFiltersResponse, TicketListItemDTO, TicketListParams } from '@/types/api';

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

  addComment: async (id: number | string, comment: string) => {
    const response = await apiClient.post<ApiResponse<{ status: string }>>(`/tickets/${id}/comments`, {
      comment,
    });
    return response.data;
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
};
