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
};
